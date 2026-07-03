package grpc

import (
	"testing"

	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerStream_BuildTracking(t *testing.T) {
	ws := NewWorkerStream("w1", 2, nil, false, "")

	assert.True(t, ws.HasCapacity())
	assert.Equal(t, 0, ws.RunningCount())
	assert.False(t, ws.HasBuild("b1"))

	ws.AddBuild("b1")
	assert.True(t, ws.HasBuild("b1"))
	assert.Equal(t, 1, ws.RunningCount())
	assert.True(t, ws.HasCapacity())

	ws.AddBuild("b2")
	assert.Equal(t, 2, ws.RunningCount())
	assert.False(t, ws.HasCapacity())

	ws.RemoveBuild("b1")
	assert.False(t, ws.HasBuild("b1"))
	assert.True(t, ws.HasCapacity())
	assert.Equal(t, 1, ws.RunningCount())
}

func TestWorkerStreamManager_RegisterUnregister(t *testing.T) {
	m := NewWorkerStreamManager()

	ws := NewWorkerStream("w1", 2, nil, false, "")
	m.Register(ws)
	assert.Equal(t, 1, m.ConnectedCount())
	assert.Equal(t, ws, m.Get("w1"))

	m.Unregister("w1")
	assert.Equal(t, 0, m.ConnectedCount())
	assert.Nil(t, m.Get("w1"))
}

func TestWorkerStreamManager_RegisterReplacesOld(t *testing.T) {
	m := NewWorkerStreamManager()

	old := NewWorkerStream("w1", 2, nil, false, "")
	m.Register(old)

	newWs := NewWorkerStream("w1", 3, nil, false, "")
	m.Register(newWs)

	// Old stream's Done should be closed
	select {
	case <-old.Done:
		// expected
	default:
		t.Fatal("old stream Done channel should be closed")
	}

	assert.Equal(t, newWs, m.Get("w1"))
	assert.Equal(t, 1, m.ConnectedCount())
}

func TestWorkerStreamManager_SendToWorker(t *testing.T) {
	m := NewWorkerStreamManager()

	// Error when worker not connected
	err := m.SendToWorker("w1", &workerv1.ServerMessage{})
	require.Error(t, err)

	ws := NewWorkerStream("w1", 2, nil, false, "")
	m.Register(ws)

	msg := &workerv1.ServerMessage{
		Payload: &workerv1.ServerMessage_Ping{
			Ping: &workerv1.Ping{Timestamp: 123},
		},
	}
	err = m.SendToWorker("w1", msg)
	require.NoError(t, err)

	received := <-ws.Send
	assert.Equal(t, msg, received)
}

func TestWorkerStreamManager_FindIdleWorker(t *testing.T) {
	m := NewWorkerStreamManager()

	assert.Nil(t, m.FindIdleWorker())

	ws1 := NewWorkerStream("w1", 1, nil, false, "")
	ws1.AddBuild("b1") // at capacity
	m.Register(ws1)

	assert.Nil(t, m.FindIdleWorker())

	ws2 := NewWorkerStream("w2", 2, nil, false, "")
	m.Register(ws2)

	idle := m.FindIdleWorker()
	assert.Equal(t, ws2, idle)
}

func TestWorkerStreamManager_HasTeamWorkers(t *testing.T) {
	m := NewWorkerStreamManager()

	assert.False(t, m.HasTeamWorkers("teamA"))

	ws1 := NewWorkerStream("w1", 1, nil, false, "teamA")
	m.Register(ws1)
	assert.True(t, m.HasTeamWorkers("teamA"))
	assert.False(t, m.HasTeamWorkers("teamB"))

	ws2 := NewWorkerStream("w2", 1, nil, false, "")
	m.Register(ws2)
	assert.True(t, m.HasTeamWorkers("teamA"))
	assert.False(t, m.HasTeamWorkers("teamB"))

	m.Unregister("w1")
	assert.False(t, m.HasTeamWorkers("teamA"))
}

func TestWorkerStreamManager_WorkerForBuild(t *testing.T) {
	m := NewWorkerStreamManager()

	assert.Nil(t, m.WorkerForBuild("b1"))

	ws1 := NewWorkerStream("w1", 2, nil, false, "")
	ws1.AddBuild("b1")
	m.Register(ws1)

	ws2 := NewWorkerStream("w2", 2, nil, false, "")
	ws2.AddBuild("b2")
	m.Register(ws2)

	found := m.WorkerForBuild("b1")
	assert.Equal(t, ws1, found)

	found = m.WorkerForBuild("b2")
	assert.Equal(t, ws2, found)

	assert.Nil(t, m.WorkerForBuild("b3"))
}
