package grpc

import (
	"log/slog"
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pikoci/pikoci/pikoci/notifier"
	"github.com/pikoci/pikoci/pikoci/workitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func TestServer_ValidateWorkerToken(t *testing.T) {
	secret := []byte("test-secret")
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, testLogger)

	// Valid worker token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
	})
	tokenStr, err := token.SignedString(secret)
	require.NoError(t, err)

	err = s.validateWorkerToken(tokenStr)
	assert.NoError(t, err)

	// Non-worker token
	token2 := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": map[string]interface{}{"username": "test"},
	})
	tokenStr2, err := token2.SignedString(secret)
	require.NoError(t, err)

	err = s.validateWorkerToken(tokenStr2)
	assert.Error(t, err)

	// Invalid token
	err = s.validateWorkerToken("invalid-token")
	assert.Error(t, err)
}

func TestServer_Register(t *testing.T) {
	secret := []byte("test-secret")
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), secret, testLogger)

	// Valid registration
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"is_from_worker": true,
	})
	tokenStr, err := token.SignedString(secret)
	require.NoError(t, err)

	resp, err := s.Register(nil, &workerv1.RegisterRequest{
		WorkerId:    "w1",
		WorkerToken: tokenStr,
		MaxJobs:     2,
	})
	require.NoError(t, err)
	assert.True(t, resp.Accepted)

	// Invalid token
	resp, err = s.Register(nil, &workerv1.RegisterRequest{
		WorkerId:    "w2",
		WorkerToken: "bad-token",
		MaxJobs:     1,
	})
	require.NoError(t, err)
	assert.False(t, resp.Accepted)
}

func TestServer_CancelBuild(t *testing.T) {
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), nil, testLogger)

	// No worker has the build
	err := s.CancelBuild("b1", "user cancelled")
	assert.Error(t, err)

	// Register a worker with the build
	ws := NewWorkerStream("w1", 2)
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
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), nil, testLogger)
	ws := NewWorkerStream("w1", 2)

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
	s := NewServer(nil, notifier.New(), NewWorkerStreamManager(), nil, testLogger)
	ws := NewWorkerStream("w1", 2)

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
