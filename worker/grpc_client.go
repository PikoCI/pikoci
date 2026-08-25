package worker

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strconv"
	"time"

	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/pikoci/pikoci/pikoci/workitem"
)

// grpcLoop connects to the gRPC server and processes work via bidirectional streaming.
// It reconnects with exponential backoff on disconnect.
func (w *Worker) grpcLoop(ctx context.Context) error {
	for {
		if w.draining.Load() {
			return nil
		}

		err := w.grpcSession(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if w.draining.Load() {
			return nil
		}

		w.logger.Error("gRPC session ended, reconnecting", "error", err)
		jitter := time.Duration(rand.Intn(5000)) * time.Millisecond
		select {
		case <-time.After(2*time.Second + jitter):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// grpcSession runs a single gRPC streaming session.
func (w *Worker) grpcSession(ctx context.Context) error {
	// Register with the server
	regResp, err := w.grpcClient.Register(ctx, &workerv1.RegisterRequest{
		WorkerId:      w.Name,
		WorkerToken:   w.WorkerToken,
		MaxJobs:       int32(w.Concurrency),
		Tags:          w.Tags,
		ExclusiveTags: w.ExclusiveTags,
	})
	if err != nil {
		return fmt.Errorf("register failed: %w", err)
	}
	if !regResp.Accepted {
		return fmt.Errorf("registration rejected: %s", regResp.Message)
	}
	w.logger.Info("registered with gRPC server")

	// Open Execute stream
	stream, err := w.grpcClient.Execute(ctx)
	if err != nil {
		return fmt.Errorf("execute stream failed: %w", err)
	}

	// Send initial heartbeat to identify ourselves (RunningJobs is 0 on connect)
	if err := stream.Send(&workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_Heartbeat{
			Heartbeat: &workerv1.Heartbeat{
				WorkerId:    w.Name,
				Timestamp:   time.Now().Unix(),
				RunningJobs: 0,
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to send initial heartbeat: %w", err)
	}

	// Start heartbeat sender
	go w.grpcHeartbeatLoop(ctx, stream)

	// Receive loop
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recv error: %w", err)
		}

		switch p := msg.Payload.(type) {
		case *workerv1.ServerMessage_Job:
			go w.handleGRPCJob(ctx, p.Job, stream)
		case *workerv1.ServerMessage_CancelJob:
			w.handleGRPCCancel(p.CancelJob)
		case *workerv1.ServerMessage_Ping:
			// Respond with heartbeat
			stream.Send(&workerv1.WorkerMessage{
				Payload: &workerv1.WorkerMessage_Heartbeat{
					Heartbeat: &workerv1.Heartbeat{
						WorkerId:    w.Name,
						Timestamp:   time.Now().Unix(),
						RunningJobs: int32(w.activeJobCount()),
					},
				},
			})
		}
	}
}

// handleGRPCJob processes a job received via gRPC stream.
func (w *Worker) handleGRPCJob(ctx context.Context, job *workerv1.Job, stream workerv1.WorkerService_ExecuteClient) {
	if w.draining.Load() {
		w.sendOnStream(stream, &workerv1.WorkerMessage{
			Payload: &workerv1.WorkerMessage_JobResult{
				JobResult: &workerv1.JobResult{BuildId: job.BuildId, Status: "errored", Error: "worker is draining"},
			},
		})
		return
	}

	buildID, _ := strconv.ParseUint(job.BuildId, 10, 32)

	// Create cancellable context for this job
	jobCtx, jobCancel := context.WithCancel(ctx)
	w.trackJob(job.BuildId, jobCancel)
	defer func() {
		w.untrackJob(job.BuildId)
		jobCancel()
	}()

	// Send acceptance
	w.sendOnStream(stream, &workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_JobAccepted{
			JobAccepted: &workerv1.JobAccepted{BuildId: job.BuildId, StartedAt: time.Now().Unix()},
		},
	})

	item := workitem.Body{
		TeamCanonical:     job.Team,
		PipelineCanonical: job.PipelineName,
		JobName:           job.JobName,
		BuildID:           uint32(buildID),
		BuildNumber:       job.BuildNumber,
		VersionID:         job.VersionId,
		ResourceCanonical: job.ResourceCanonical,
		RetryBuildNumber:  job.RetryBuildNumber,
		RetryBuildID:      job.RetryBuildId,
	}

	cwd, err := w.createWorkDir()
	if err != nil {
		w.logger.Error("failed to create work dir", "error", err)
		w.sendOnStream(stream, &workerv1.WorkerMessage{
			Payload: &workerv1.WorkerMessage_JobResult{
				JobResult: &workerv1.JobResult{BuildId: job.BuildId, Status: "errored", Error: err.Error(), FinishedAt: time.Now().Unix()},
			},
		})
		return
	}
	defer os.RemoveAll(cwd)

	w.processMessage(jobCtx, item, cwd)

	// The build status is determined by processMessage internally,
	// which calls updateBuild/failBuild. Send completion notification.
	w.sendOnStream(stream, &workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_JobResult{
			JobResult: &workerv1.JobResult{BuildId: job.BuildId, Status: "completed", FinishedAt: time.Now().Unix()},
		},
	})
}

// handleGRPCCancel processes a cancellation message from the server.
func (w *Worker) handleGRPCCancel(cancel *workerv1.CancelJob) {
	w.logger.Info("received cancel for build", "build_id", cancel.BuildId, "reason", cancel.Reason)
	w.cancelJob(cancel.BuildId)
}

// grpcHeartbeatLoop sends periodic heartbeats over the gRPC stream.
func (w *Worker) grpcHeartbeatLoop(ctx context.Context, stream workerv1.WorkerService_ExecuteClient) {
	// Also send HTTP heartbeats for the existing worker registry
	w.sendHeartbeat(ctx)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sendHeartbeat(ctx)
			stream.Send(&workerv1.WorkerMessage{
				Payload: &workerv1.WorkerMessage_Heartbeat{
					Heartbeat: &workerv1.Heartbeat{
						WorkerId:    w.Name,
						Timestamp:   time.Now().Unix(),
						RunningJobs: int32(w.activeJobCount()),
					},
				},
			})
		}
	}
}

// sendOnStream sends a message on the given gRPC stream, logging errors.
func (w *Worker) sendOnStream(stream workerv1.WorkerService_ExecuteClient, msg *workerv1.WorkerMessage) {
	if stream == nil {
		return
	}
	if err := stream.Send(msg); err != nil {
		w.logger.Error("failed to send gRPC message", "error", err)
	}
}
