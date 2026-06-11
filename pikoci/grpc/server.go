package grpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WorkDispatcher is the interface the gRPC server uses for finding work.
// It is satisfied by *pikoci.PikoCI but not the Service interface (since
// NextWork is an internal method not exposed to HTTP clients).
type WorkDispatcher interface {
	NextWork(ctx context.Context) (*workitem.Item, error)
}

// Server implements the WorkerService gRPC server.
type Server struct {
	workerv1.UnimplementedWorkerServiceServer

	svc       WorkDispatcher
	notifier  *notifier.WorkNotifier
	streams   *WorkerStreamManager
	jwtSecret []byte
	logger    *slog.Logger

	// registeredWorkers stores registration info from Register calls.
	// Entries expire after 30 seconds if Execute is not called.
	registeredMu      sync.Mutex
	registeredWorkers map[string]*registeredWorker
}

type registeredWorker struct {
	maxJobs    int32
	registedAt time.Time
}

// NewServer creates a new gRPC WorkerService server.
func NewServer(svc WorkDispatcher, n *notifier.WorkNotifier, sm *WorkerStreamManager, jwtSecret []byte, l *slog.Logger) *Server {
	return &Server{
		svc:               svc,
		notifier:          n,
		streams:           sm,
		jwtSecret:         jwtSecret,
		logger:            l,
		registeredWorkers: make(map[string]*registeredWorker),
	}
}

// Streams returns the stream manager used by this server.
func (s *Server) Streams() *WorkerStreamManager {
	return s.streams
}

// Register validates a worker's JWT token and stores the worker's max_jobs
// capacity for use when the Execute stream opens.
func (s *Server) Register(ctx context.Context, req *workerv1.RegisterRequest) (*workerv1.RegisterResponse, error) {
	if err := s.validateWorkerToken(req.WorkerToken); err != nil {
		return &workerv1.RegisterResponse{
			Accepted: false,
			Message:  "invalid worker token",
		}, nil
	}

	s.logger.Info("worker registered via gRPC", "worker_id", req.WorkerId, "max_jobs", req.MaxJobs)

	s.registeredMu.Lock()
	s.registeredWorkers[req.WorkerId] = &registeredWorker{
		maxJobs:    req.MaxJobs,
		registedAt: time.Now(),
	}
	s.registeredMu.Unlock()

	return &workerv1.RegisterResponse{
		Accepted: true,
		Message:  "registered",
	}, nil
}

// Execute handles the bidirectional streaming RPC between a worker and the server.
func (s *Server) Execute(stream workerv1.WorkerService_ExecuteServer) error {
	// Wait for the first message which should be a heartbeat identifying the worker.
	firstMsg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive initial message: %v", err)
	}

	hb := firstMsg.GetHeartbeat()
	if hb == nil {
		return status.Errorf(codes.InvalidArgument, "first message must be a heartbeat")
	}

	workerID := hb.WorkerId
	if workerID == "" {
		return status.Errorf(codes.InvalidArgument, "worker_id is required in initial heartbeat")
	}

	// Look up registration from the Register call — rejects workers that
	// haven't called Register (which validates the JWT token).
	// Registrations expire after 30 seconds to prevent stale entries.
	s.registeredMu.Lock()
	rw, ok := s.registeredWorkers[workerID]
	if ok {
		delete(s.registeredWorkers, workerID)
	}
	// Clean up any stale registrations while holding the lock
	for id, w := range s.registeredWorkers {
		if time.Since(w.registedAt) > 30*time.Second {
			delete(s.registeredWorkers, id)
		}
	}
	s.registeredMu.Unlock()
	if !ok {
		return status.Errorf(codes.Unauthenticated, "worker %q has not called Register", workerID)
	}
	if time.Since(rw.registedAt) > 30*time.Second {
		return status.Errorf(codes.Unauthenticated, "registration for worker %q has expired", workerID)
	}
	maxJobs := rw.maxJobs
	if maxJobs <= 0 {
		maxJobs = 1
	}
	ws := NewWorkerStream(workerID, maxJobs)
	s.streams.Register(ws)
	defer s.streams.Unregister(workerID)

	s.logger.Info("worker connected to Execute stream", "worker_id", workerID)

	ctx := stream.Context()

	// Goroutine: send messages from the channel to the stream
	sendErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ws.Done:
				return
			case msg := <-ws.Send:
				if err := stream.Send(msg); err != nil {
					sendErr <- err
					return
				}
			}
		}
	}()

	// Goroutine: listen for notifier signals and dispatch work
	go s.dispatchLoop(ctx, ws)

	// Main loop: receive messages from the worker
	for {
		select {
		case err := <-sendErr:
			s.logger.Error("send error on worker stream", "worker_id", workerID, "error", err)
			return err
		default:
		}

		msg, err := stream.Recv()
		if err == io.EOF {
			s.logger.Info("worker disconnected", "worker_id", workerID)
			return nil
		}
		if err != nil {
			s.logger.Error("recv error on worker stream", "worker_id", workerID, "error", err)
			return err
		}

		s.handleWorkerMessage(ctx, ws, msg)
	}
}

// dispatchLoop listens for work notifications and dispatches jobs to the worker.
func (s *Server) dispatchLoop(ctx context.Context, ws *WorkerStream) {
	s.logger.Debug("dispatch loop started", "worker_id", ws.WorkerID,
		"capacity", ws.HasCapacity(), "running", ws.RunningCount(), "max_jobs", ws.MaxJobs)

	// Try to dispatch immediately on connect
	s.tryDispatch(ctx, ws)

	for {
		ch, cleanup := s.notifier.Wait()
		select {
		case <-ctx.Done():
			cleanup()
			return
		case <-ws.Done:
			cleanup()
			return
		case <-ch:
			cleanup()
			s.logger.Debug("dispatch loop woken by notifier", "worker_id", ws.WorkerID,
				"capacity", ws.HasCapacity(), "running", ws.RunningCount())
			s.tryDispatch(ctx, ws)
		case <-time.After(30 * time.Second):
			cleanup()
			s.logger.Debug("dispatch loop periodic check", "worker_id", ws.WorkerID,
				"capacity", ws.HasCapacity(), "running", ws.RunningCount())
			s.tryDispatch(ctx, ws)
		}
	}
}

// tryDispatch attempts to find work and send it to the worker, filling capacity.
func (s *Server) tryDispatch(ctx context.Context, ws *WorkerStream) {
	for ws.HasCapacity() {
		item, err := s.svc.NextWork(ctx)
		if err != nil {
			s.logger.Error("NextWork failed in dispatch", "worker_id", ws.WorkerID, "error", err)
			return
		}
		if item == nil {
			return
		}
		s.sendWorkItem(ws, item)
	}
}

// sendWorkItem converts a workitem.Item to a protobuf Job and sends it.
func (s *Server) sendWorkItem(ws *WorkerStream, item *workitem.Item) {
	job := &workerv1.Job{
		Team:              item.Body.TeamCanonical,
		PipelineName:      item.Body.PipelineCanonical,
		JobName:           item.Body.JobName,
		BuildId:           strconv.FormatUint(uint64(item.Body.BuildID), 10),
		BuildNumber:       item.Body.BuildNumber,
		VersionId:         item.Body.VersionID,
		ResourceCanonical: item.Body.ResourceCanonical,
		RetryBuildNumber:  item.Body.RetryBuildNumber,
		RetryBuildId:      item.Body.RetryBuildID,
		Type:              item.Type,
	}

	buildID := job.BuildId
	if item.Type == "check" {
		// For resource checks, use a synthetic ID for tracking.
		// Set it on the proto Job so the worker sends it back in JobResult.
		buildID = fmt.Sprintf("check-%s-%s", item.Body.PipelineCanonical, item.Body.ResourceCanonical)
		job.BuildId = buildID
	}
	ws.AddBuild(buildID)

	msg := &workerv1.ServerMessage{
		Payload: &workerv1.ServerMessage_Job{Job: job},
	}

	select {
	case ws.Send <- msg:
		s.logger.Info("dispatched job to worker",
			"worker_id", ws.WorkerID,
			"type", item.Type,
			"build_id", buildID,
			"pipeline", item.Body.PipelineCanonical)
	default:
		s.logger.Warn("worker send buffer full, dropping job",
			"worker_id", ws.WorkerID, "build_id", buildID)
		ws.RemoveBuild(buildID)
	}
}

// handleWorkerMessage processes an incoming message from a worker.
func (s *Server) handleWorkerMessage(ctx context.Context, ws *WorkerStream, msg *workerv1.WorkerMessage) {
	switch p := msg.Payload.(type) {
	case *workerv1.WorkerMessage_Heartbeat:
		s.handleHeartbeat(ctx, ws, p.Heartbeat)
	case *workerv1.WorkerMessage_JobAccepted:
		s.logger.Debug("job accepted by worker",
			"worker_id", ws.WorkerID, "build_id", p.JobAccepted.BuildId)
	case *workerv1.WorkerMessage_LogChunk:
		// Log chunks are currently written via UpdateJobBuild from the worker.
		// Future: could stream to UI in real-time here.
		s.logger.Debug("log chunk received",
			"worker_id", ws.WorkerID, "build_id", p.LogChunk.BuildId,
			"step", p.LogChunk.StepName, "bytes", len(p.LogChunk.Data))
	case *workerv1.WorkerMessage_StepResult:
		s.logger.Debug("step result received",
			"worker_id", ws.WorkerID, "build_id", p.StepResult.BuildId,
			"step", p.StepResult.StepName, "status", p.StepResult.Status)
	case *workerv1.WorkerMessage_JobResult:
		s.handleJobResult(ctx, ws, p.JobResult)
	}
}

// handleHeartbeat processes a heartbeat from a worker.
// The worker sends full heartbeat data (hostname, OS, etc.) via HTTP every 30s.
// The gRPC heartbeat is just for keepalive tracking on the stream level.
func (s *Server) handleHeartbeat(ctx context.Context, ws *WorkerStream, hb *workerv1.Heartbeat) {
	s.logger.Debug("heartbeat received via gRPC",
		"worker_id", ws.WorkerID, "running_jobs", hb.RunningJobs)
}

// handleJobResult processes a completed job result from a worker.
func (s *Server) handleJobResult(ctx context.Context, ws *WorkerStream, jr *workerv1.JobResult) {
	ws.RemoveBuild(jr.BuildId)

	s.logger.Info("job result received",
		"worker_id", ws.WorkerID, "build_id", jr.BuildId,
		"status", jr.Status)

	// Notify so other workers can pick up next work
	s.notifier.Notify()
}

// CancelBuild sends a cancel message to the worker running the given build.
func (s *Server) CancelBuild(buildID string, reason string) error {
	ws := s.streams.WorkerForBuild(buildID)
	if ws == nil {
		return fmt.Errorf("no worker found for build %q", buildID)
	}

	msg := &workerv1.ServerMessage{
		Payload: &workerv1.ServerMessage_CancelJob{
			CancelJob: &workerv1.CancelJob{
				BuildId: buildID,
				Reason:  reason,
			},
		},
	}

	return s.streams.SendToWorker(ws.WorkerID, msg)
}

// validateWorkerToken validates a JWT worker token.
func (s *Server) validateWorkerToken(tokenStr string) error {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("invalid claims")
	}

	isFromWorker, _ := claims["is_from_worker"].(bool)
	if !isFromWorker {
		return fmt.Errorf("token is not a worker token")
	}

	return nil
}

