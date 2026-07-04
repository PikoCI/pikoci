package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func validWorkerToken(secret []byte) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
	})
	s, _ := token.SignedString(secret)
	return s
}

// --- Unit tests ---

func TestServer_ValidateWorkerToken(t *testing.T) {
	ctx := context.TODO()
	secret := []byte("test-secret")
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, nil, testLogger)

	// Valid global worker token
	tc, err := s.validateWorkerToken(ctx, validWorkerToken(secret))
	assert.NoError(t, err)
	assert.Empty(t, tc)

	// Non-worker token (user token, missing is_from_worker)
	token2 := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": map[string]interface{}{"username": "test"},
	})
	tokenStr2, _ := token2.SignedString(secret)
	_, err = s.validateWorkerToken(ctx, tokenStr2)
	assert.Error(t, err)

	// Invalid token string
	_, err = s.validateWorkerToken(ctx, "invalid-token")
	assert.Error(t, err)

	// Wrong signing secret
	wrongSecret := []byte("wrong-secret")
	_, err = s.validateWorkerToken(ctx, validWorkerToken(wrongSecret))
	assert.Error(t, err)

	// Empty token
	_, err = s.validateWorkerToken(ctx, "")
	assert.Error(t, err)
}

// fakeTeamSaltLookup implements TeamSaltLookup for testing.
type fakeTeamSaltLookup struct {
	salts map[string]string
	err   error
}

func (f *fakeTeamSaltLookup) FindWorkerTokenSalt(ctx context.Context, tc string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.salts[tc], nil
}

func TestServer_ValidateWorkerToken_TeamScoped(t *testing.T) {
	ctx := context.TODO()
	secret := []byte("test-secret")
	salt := "test-salt-uuid"

	tsl := &fakeTeamSaltLookup{salts: map[string]string{"teamA": salt}}
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, tsl, testLogger)

	// Valid team token with correct salt
	teamToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
		"team_canonical": "teamA",
		"salt":           salt,
	})
	tokenStr, _ := teamToken.SignedString(secret)
	tc, err := s.validateWorkerToken(ctx, tokenStr)
	assert.NoError(t, err)
	assert.Equal(t, "teamA", tc)

	// Team token with wrong salt
	badSaltToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
		"team_canonical": "teamA",
		"salt":           "wrong-salt",
	})
	tokenStr, _ = badSaltToken.SignedString(secret)
	_, err = s.validateWorkerToken(ctx, tokenStr)
	assert.Error(t, err)

	// Team token with empty salt in DB (token revoked)
	tsl2 := &fakeTeamSaltLookup{salts: map[string]string{"teamB": ""}}
	s2 := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, tsl2, testLogger)
	revokedToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
		"team_canonical": "teamB",
		"salt":           "old-salt",
	})
	tokenStr, _ = revokedToken.SignedString(secret)
	_, err = s2.validateWorkerToken(ctx, tokenStr)
	assert.Error(t, err)

	// Team token missing salt claim
	noSaltToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
		"team_canonical": "teamA",
	})
	tokenStr, _ = noSaltToken.SignedString(secret)
	_, err = s.validateWorkerToken(ctx, tokenStr)
	assert.Error(t, err)

	// Global token (no team claim) → accepted, returns empty team canonical
	globalToken := validWorkerToken(secret)
	tc, err = s.validateWorkerToken(ctx, globalToken)
	assert.NoError(t, err)
	assert.Empty(t, tc)
}

func TestServer_ValidateWorkerToken_NilSaltLookup_TeamToken(t *testing.T) {
	ctx := context.TODO()
	secret := []byte("test-secret")
	// Server with nil teamSaltLookup (embedded mode)
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, nil, testLogger)

	teamToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
		"team_canonical": "teamA",
		"salt":           "some-salt",
	})
	tokenStr, _ := teamToken.SignedString(secret)
	_, err := s.validateWorkerToken(ctx, tokenStr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "team salt lookup not configured")
}

func TestServer_ValidateWorkerToken_SaltLookupDBError(t *testing.T) {
	ctx := context.TODO()
	secret := []byte("test-secret")
	tsl := &fakeTeamSaltLookup{err: fmt.Errorf("connection refused")}
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, tsl, testLogger)

	teamToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
		"team_canonical": "teamA",
		"salt":           "some-salt",
	})
	tokenStr, _ := teamToken.SignedString(secret)
	_, err := s.validateWorkerToken(ctx, tokenStr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to look up team salt")
}

func TestServer_Register_TeamScoped(t *testing.T) {
	secret := []byte("test-secret")
	salt := "test-salt"
	tsl := &fakeTeamSaltLookup{salts: map[string]string{"teamA": salt}}
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, tsl, testLogger)

	teamToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
		"team_canonical": "teamA",
		"salt":           salt,
	})
	tokenStr, _ := teamToken.SignedString(secret)

	resp, err := s.Register(context.TODO(), &workerv1.RegisterRequest{
		WorkerId:    "tw1",
		WorkerToken: tokenStr,
		MaxJobs:     2,
		Tags:        []string{"gpu"},
	})
	require.NoError(t, err)
	assert.True(t, resp.Accepted)

	// Verify teamCanonical is stored in registeredWorker
	s.registeredMu.Lock()
	rw, ok := s.registeredWorkers["tw1"]
	s.registeredMu.Unlock()
	assert.True(t, ok)
	assert.Equal(t, "teamA", rw.teamCanonical)
	assert.Equal(t, int32(2), rw.maxJobs)
}

func TestServer_TryDispatch_PropagatesTeamCanonical(t *testing.T) {
	var capturedWC workitem.WorkerContext
	dispatcher := &fakeDispatcher{
		nextWorkFn: func(ctx context.Context, wc workitem.WorkerContext) (*workitem.Item, error) {
			capturedWC = wc
			return nil, nil
		},
	}

	s := NewServer(dispatcher, notifier.New(), NewWorkerStreamManager(), nil, nil, testLogger)
	ws := NewWorkerStream("w1", 2, []string{"gpu"}, true, "teamA")

	s.tryDispatch(context.Background(), ws)

	assert.Equal(t, "teamA", capturedWC.TeamCanonical)
	assert.Equal(t, []string{"gpu"}, capturedWC.Tags)
	assert.True(t, capturedWC.ExclusiveTags)
}

func TestServer_Register_ValidToken(t *testing.T) {
	secret := []byte("test-secret")
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, nil, testLogger)

	resp, err := s.Register(context.TODO(), &workerv1.RegisterRequest{
		WorkerId:    "w1",
		WorkerToken: validWorkerToken(secret),
		MaxJobs:     2,
	})
	require.NoError(t, err)
	assert.True(t, resp.Accepted)

	// Verify entry was stored
	s.registeredMu.Lock()
	rw, ok := s.registeredWorkers["w1"]
	s.registeredMu.Unlock()
	assert.True(t, ok)
	assert.Equal(t, int32(2), rw.maxJobs)
}

func TestServer_Register_InvalidToken(t *testing.T) {
	secret := []byte("test-secret")
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, nil, testLogger)

	resp, err := s.Register(context.TODO(), &workerv1.RegisterRequest{
		WorkerId:    "w2",
		WorkerToken: "bad-token",
		MaxJobs:     1,
	})
	require.NoError(t, err)
	assert.False(t, resp.Accepted)

	// Verify entry was NOT stored
	s.registeredMu.Lock()
	_, ok := s.registeredWorkers["w2"]
	s.registeredMu.Unlock()
	assert.False(t, ok)
}

func TestServer_Register_ConsumedByExecute(t *testing.T) {
	secret := []byte("test-secret")
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, nil, testLogger)

	// Register stores entry
	s.Register(context.TODO(), &workerv1.RegisterRequest{
		WorkerId:    "w1",
		WorkerToken: validWorkerToken(secret),
		MaxJobs:     3,
	})

	// Simulate what Execute does: consume the entry
	s.registeredMu.Lock()
	rw, ok := s.registeredWorkers["w1"]
	if ok {
		delete(s.registeredWorkers, "w1")
	}
	s.registeredMu.Unlock()

	assert.True(t, ok)
	assert.Equal(t, int32(3), rw.maxJobs)

	// Entry should be gone — second Execute would fail
	s.registeredMu.Lock()
	_, ok = s.registeredWorkers["w1"]
	s.registeredMu.Unlock()
	assert.False(t, ok)
}

func TestServer_CancelBuild(t *testing.T) {
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), nil, nil, testLogger)

	// No worker has the build
	err := s.CancelBuild("b1", "user cancelled")
	assert.Error(t, err)

	// Register a worker with the build
	ws := NewWorkerStream("w1", 2, nil, false, "")
	ws.AddBuild("b1")
	s.streams.Register(ws)

	err = s.CancelBuild("b1", "user cancelled")
	require.NoError(t, err)

	// Verify the cancel message was sent
	msg := <-ws.Send
	cancel := msg.GetCancelJob()
	require.NotNil(t, cancel)
	assert.Equal(t, "b1", cancel.BuildId)
	assert.Equal(t, "user cancelled", cancel.Reason)
}

func TestServer_SendWorkItem(t *testing.T) {
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), nil, nil, testLogger)
	ws := NewWorkerStream("w1", 2, nil, false, "")

	item := &workitem.Item{
		Type: "job",
		Body: workitem.Body{
			TeamCanonical:     "main",
			PipelineCanonical: "test-pipeline",
			JobName:           "build",
			BuildID:           42,
			BuildNumber:       "1",
			VersionID:         10,
		},
	}

	s.sendWorkItem(ws, item)

	msg := <-ws.Send
	job := msg.GetJob()
	require.NotNil(t, job)
	assert.Equal(t, "main", job.Team)
	assert.Equal(t, "test-pipeline", job.PipelineName)
	assert.Equal(t, "build", job.JobName)
	assert.Equal(t, "42", job.BuildId)
	assert.Equal(t, "1", job.BuildNumber)
	assert.Equal(t, uint32(10), job.VersionId)
	assert.Equal(t, "job", job.Type)

	// Worker should track the build
	assert.True(t, ws.HasBuild("42"))
}

func TestServer_SendWorkItem_ResourceCheck(t *testing.T) {
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), nil, nil, testLogger)
	ws := NewWorkerStream("w1", 2, nil, false, "")

	item := &workitem.Item{
		Type: "check",
		Body: workitem.Body{
			TeamCanonical:     "main",
			PipelineCanonical: "test-pipeline",
			ResourceCanonical: "my-repo",
		},
	}

	s.sendWorkItem(ws, item)

	msg := <-ws.Send
	job := msg.GetJob()
	require.NotNil(t, job)
	assert.Equal(t, "check", job.Type)
	assert.Equal(t, "my-repo", job.ResourceCanonical)
	assert.True(t, ws.HasBuild("check-test-pipeline-my-repo"))
}

func TestServer_TryDispatch_FillsCapacity(t *testing.T) {
	callCount := 0
	dispatcher := &fakeDispatcher{
		nextWorkFn: func(ctx context.Context, wc workitem.WorkerContext) (*workitem.Item, error) {
			callCount++
			if callCount > 2 {
				return nil, nil // no more work
			}
			return &workitem.Item{
				Type: "job",
				Body: workitem.Body{
					TeamCanonical:     "main",
					PipelineCanonical: "pipe",
					JobName:           "j",
					BuildID:           uint32(callCount),
					BuildNumber:       "1",
				},
			}, nil
		},
	}

	s := NewServer(dispatcher, notifier.New(), NewWorkerStreamManager(), nil, nil, testLogger)
	ws := NewWorkerStream("w1", 3, nil, false, "")

	s.tryDispatch(context.Background(), ws)

	// Should have dispatched 2 items (callCount went to 3 returning nil)
	assert.Equal(t, 2, ws.RunningCount())
}

func TestServer_HandleJobResult_NotifiesAndRemovesBuild(t *testing.T) {
	n := notifier.New()
	s := NewServer(nil, n, NewWorkerStreamManager(), nil, nil, testLogger)
	ws := NewWorkerStream("w1", 2, nil, false, "")
	ws.AddBuild("b1")

	// Set up a waiter before the notification
	ch, cleanup := n.Wait()
	defer cleanup()

	s.handleJobResult(context.Background(), ws, &workerv1.JobResult{
		BuildId: "b1",
		Status:  "succeeded",
	})

	// Build should be removed
	assert.False(t, ws.HasBuild("b1"))

	// Notifier should have been triggered
	select {
	case <-ch:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected notifier to be triggered")
	}
}

// --- gRPC integration tests (real server + client) ---

// startTestServer starts a gRPC server on a random port and returns the client and cleanup function.
func startTestServer(t *testing.T, svc WorkDispatcher, n *notifier.WorkNotifier, secret []byte) (workerv1.WorkerServiceClient, *Server, func()) {
	t.Helper()
	sm := NewWorkerStreamManager()
	srv := NewServer(svc, n, sm, secret, nil, testLogger)
	grpcSrv := grpc.NewServer()
	workerv1.RegisterWorkerServiceServer(grpcSrv, srv)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go grpcSrv.Serve(lis)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	client := workerv1.NewWorkerServiceClient(conn)

	return client, srv, func() {
		conn.Close()
		grpcSrv.Stop()
		lis.Close()
	}
}

func TestGRPC_ExecuteRejectsUnregisteredWorker(t *testing.T) {
	secret := []byte("test-secret")
	client, _, cleanup := startTestServer(t, &fakeDispatcher{}, notifier.New(), secret)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.Execute(ctx)
	require.NoError(t, err)

	// Send heartbeat without calling Register first
	err = stream.Send(&workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_Heartbeat{
			Heartbeat: &workerv1.Heartbeat{
				WorkerId:    "unregistered-worker",
				Timestamp:   time.Now().Unix(),
				RunningJobs: 0,
			},
		},
	})
	require.NoError(t, err)

	// Server should reject with Unauthenticated
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not called Register")
}

func TestGRPC_RegisterThenExecuteSucceeds(t *testing.T) {
	secret := []byte("test-secret")
	n := notifier.New()
	client, srv, cleanup := startTestServer(t, &fakeDispatcher{}, n, secret)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Step 1: Register
	resp, err := client.Register(ctx, &workerv1.RegisterRequest{
		WorkerId:    "w1",
		WorkerToken: validWorkerToken(secret),
		MaxJobs:     2,
	})
	require.NoError(t, err)
	assert.True(t, resp.Accepted)

	// Step 2: Execute
	stream, err := client.Execute(ctx)
	require.NoError(t, err)

	// Step 3: Send initial heartbeat
	err = stream.Send(&workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_Heartbeat{
			Heartbeat: &workerv1.Heartbeat{
				WorkerId:    "w1",
				Timestamp:   time.Now().Unix(),
				RunningJobs: 0,
			},
		},
	})
	require.NoError(t, err)

	// Give the server a moment to process
	time.Sleep(50 * time.Millisecond)

	// Worker should be registered in the stream manager
	ws := srv.Streams().Get("w1")
	require.NotNil(t, ws, "worker should be in stream manager after Execute")
	assert.Equal(t, int32(2), ws.MaxJobs)
}

func TestGRPC_RegisterWithBadTokenRejected(t *testing.T) {
	secret := []byte("test-secret")
	client, _, cleanup := startTestServer(t, &fakeDispatcher{}, notifier.New(), secret)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.Register(ctx, &workerv1.RegisterRequest{
		WorkerId:    "evil",
		WorkerToken: "forged-token",
		MaxJobs:     99,
	})
	require.NoError(t, err)
	assert.False(t, resp.Accepted)

	// Even if they try Execute, it should fail
	stream, err := client.Execute(ctx)
	require.NoError(t, err)
	err = stream.Send(&workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_Heartbeat{
			Heartbeat: &workerv1.Heartbeat{WorkerId: "evil"},
		},
	})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not called Register")
}

func TestGRPC_RegisterCannotBeReusedTwice(t *testing.T) {
	secret := []byte("test-secret")
	client, _, cleanup := startTestServer(t, &fakeDispatcher{}, notifier.New(), secret)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Register once
	resp, _ := client.Register(ctx, &workerv1.RegisterRequest{
		WorkerId:    "w1",
		WorkerToken: validWorkerToken(secret),
		MaxJobs:     1,
	})
	assert.True(t, resp.Accepted)

	// First Execute works
	stream1, _ := client.Execute(ctx)
	stream1.Send(&workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_Heartbeat{
			Heartbeat: &workerv1.Heartbeat{WorkerId: "w1"},
		},
	})
	// Give server time to consume the registration
	time.Sleep(50 * time.Millisecond)

	// Second Execute with same worker_id should fail (registration consumed)
	stream2, _ := client.Execute(ctx)
	stream2.Send(&workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_Heartbeat{
			Heartbeat: &workerv1.Heartbeat{WorkerId: "w1"},
		},
	})
	_, err := stream2.Recv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not called Register")
}

func TestGRPC_ServerDispatchesJobOnNotify(t *testing.T) {
	secret := []byte("test-secret")
	n := notifier.New()
	dispatched := make(chan struct{}, 1)
	dispatcher := &fakeDispatcher{
		nextWorkFn: func(ctx context.Context, wc workitem.WorkerContext) (*workitem.Item, error) {
			select {
			case dispatched <- struct{}{}:
			default:
			}
			return &workitem.Item{
				Type: "job",
				Body: workitem.Body{
					TeamCanonical:     "main",
					PipelineCanonical: "pipe",
					JobName:           "build",
					BuildID:           1,
					BuildNumber:       "1",
				},
			}, nil
		},
	}

	client, _, cleanup := startTestServer(t, dispatcher, n, secret)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Register + Execute
	client.Register(ctx, &workerv1.RegisterRequest{
		WorkerId:    "w1",
		WorkerToken: validWorkerToken(secret),
		MaxJobs:     1,
	})
	stream, _ := client.Execute(ctx)
	stream.Send(&workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_Heartbeat{
			Heartbeat: &workerv1.Heartbeat{WorkerId: "w1", RunningJobs: 0},
		},
	})

	// Wait for dispatchLoop to call NextWork (it dispatches immediately on connect)
	select {
	case <-dispatched:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop never called NextWork")
	}

	// Worker should receive a Job message
	msg, err := stream.Recv()
	require.NoError(t, err)
	job := msg.GetJob()
	require.NotNil(t, job)
	assert.Equal(t, "build", job.JobName)
	assert.Equal(t, "pipe", job.PipelineName)
}

func TestGRPC_ServerSendsCancelToWorker(t *testing.T) {
	secret := []byte("test-secret")
	n := notifier.New()
	// Return no work so dispatch doesn't interfere
	dispatcher := &fakeDispatcher{
		nextWorkFn: func(ctx context.Context, wc workitem.WorkerContext) (*workitem.Item, error) {
			return nil, nil
		},
	}

	client, srv, cleanup := startTestServer(t, dispatcher, n, secret)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Register + Execute
	client.Register(ctx, &workerv1.RegisterRequest{
		WorkerId:    "w1",
		WorkerToken: validWorkerToken(secret),
		MaxJobs:     2,
	})
	stream, _ := client.Execute(ctx)
	stream.Send(&workerv1.WorkerMessage{
		Payload: &workerv1.WorkerMessage_Heartbeat{
			Heartbeat: &workerv1.Heartbeat{WorkerId: "w1"},
		},
	})

	// Wait for worker to be registered in stream manager
	time.Sleep(100 * time.Millisecond)

	// Manually add a build to the worker's tracking
	ws := srv.Streams().Get("w1")
	require.NotNil(t, ws)
	ws.AddBuild("build-99")

	// Send cancel from server side
	err := srv.CancelBuild("build-99", "user clicked cancel")
	require.NoError(t, err)

	// Worker should receive CancelJob
	msg, err := stream.Recv()
	require.NoError(t, err)
	cancelMsg := msg.GetCancelJob()
	require.NotNil(t, cancelMsg)
	assert.Equal(t, "build-99", cancelMsg.BuildId)
	assert.Equal(t, "user clicked cancel", cancelMsg.Reason)
}

// --- Fakes ---

type fakeDispatcher struct {
	nextWorkFn func(ctx context.Context, wc workitem.WorkerContext) (*workitem.Item, error)
}

func (f *fakeDispatcher) NextWork(ctx context.Context, wc workitem.WorkerContext) (*workitem.Item, error) {
	if f.nextWorkFn != nil {
		return f.nextWorkFn(ctx, wc)
	}
	return nil, nil
}
