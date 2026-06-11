// Package grpc implements the gRPC transport layer for communicating with
// standalone PikoCI workers via bidirectional streaming.
package grpc

import (
	"fmt"
	"sync"

	workerv1 "github.com/pikoci/pikoci/gen/worker/v1"
)

// WorkerStream represents a single connected worker's gRPC stream.
type WorkerStream struct {
	WorkerID string
	MaxJobs  int32
	Send     chan *workerv1.ServerMessage
	Done     chan struct{}

	mu            sync.Mutex
	runningBuilds map[string]struct{} // buildID → present
}

// NewWorkerStream creates a WorkerStream for the given worker.
func NewWorkerStream(workerID string, maxJobs int32) *WorkerStream {
	return &WorkerStream{
		WorkerID:      workerID,
		MaxJobs:       maxJobs,
		Send:          make(chan *workerv1.ServerMessage, 16),
		Done:          make(chan struct{}),
		runningBuilds: make(map[string]struct{}),
	}
}

// AddBuild marks a build as running on this worker.
func (ws *WorkerStream) AddBuild(buildID string) {
	ws.mu.Lock()
	ws.runningBuilds[buildID] = struct{}{}
	ws.mu.Unlock()
}

// RemoveBuild removes a build from this worker's running set.
func (ws *WorkerStream) RemoveBuild(buildID string) {
	ws.mu.Lock()
	delete(ws.runningBuilds, buildID)
	ws.mu.Unlock()
}

// HasBuild returns true if the worker is running the given build.
func (ws *WorkerStream) HasBuild(buildID string) bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	_, ok := ws.runningBuilds[buildID]
	return ok
}

// RunningCount returns the number of builds currently running on this worker.
func (ws *WorkerStream) RunningCount() int {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return len(ws.runningBuilds)
}

// HasCapacity returns true if the worker can accept more jobs.
func (ws *WorkerStream) HasCapacity() bool {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return int32(len(ws.runningBuilds)) < ws.MaxJobs
}

// WorkerStreamManager tracks all connected worker streams.
type WorkerStreamManager struct {
	mu      sync.RWMutex
	streams map[string]*WorkerStream // workerID → stream
}

// NewWorkerStreamManager creates a new stream manager.
func NewWorkerStreamManager() *WorkerStreamManager {
	return &WorkerStreamManager{
		streams: make(map[string]*WorkerStream),
	}
}

// Register adds a worker stream. If a stream already exists for the workerID,
// the old one is closed first.
func (m *WorkerStreamManager) Register(ws *WorkerStream) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.streams[ws.WorkerID]; ok {
		close(old.Done)
	}
	m.streams[ws.WorkerID] = ws
}

// Unregister removes a worker stream.
func (m *WorkerStreamManager) Unregister(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.streams, workerID)
}

// Get returns the stream for a worker, or nil if not connected.
func (m *WorkerStreamManager) Get(workerID string) *WorkerStream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streams[workerID]
}

// SendToWorker sends a message to a specific worker. Returns an error if the
// worker is not connected or the send buffer is full.
func (m *WorkerStreamManager) SendToWorker(workerID string, msg *workerv1.ServerMessage) error {
	m.mu.RLock()
	ws, ok := m.streams[workerID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("worker %q not connected", workerID)
	}
	select {
	case ws.Send <- msg:
		return nil
	default:
		return fmt.Errorf("worker %q send buffer full", workerID)
	}
}

// FindIdleWorker returns a connected worker with capacity, or nil if none available.
func (m *WorkerStreamManager) FindIdleWorker() *WorkerStream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ws := range m.streams {
		if ws.HasCapacity() {
			return ws
		}
	}
	return nil
}

// WorkerForBuild finds which worker is running a given build.
// Returns nil if no worker has the build.
func (m *WorkerStreamManager) WorkerForBuild(buildID string) *WorkerStream {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ws := range m.streams {
		if ws.HasBuild(buildID) {
			return ws
		}
	}
	return nil
}

// ConnectedCount returns the number of connected workers.
func (m *WorkerStreamManager) ConnectedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.streams)
}
