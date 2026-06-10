// Package notifier provides a broadcast mechanism for notifying workers
// that new work is available via Go channels.
package notifier

import "sync"

// WorkNotifier broadcasts wake-up signals to waiting workers.
// Workers call Wait to block until Notify is called.
type WorkNotifier struct {
	mu      sync.Mutex
	waiters []chan struct{}
}

// New creates a new WorkNotifier.
func New() *WorkNotifier { return &WorkNotifier{} }

// Notify wakes all waiting workers.
func (n *WorkNotifier) Notify() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, ch := range n.waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Wait returns a channel that receives when Notify is called,
// and a cleanup function that removes the waiter.
func (n *WorkNotifier) Wait() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.waiters = append(n.waiters, ch)
	n.mu.Unlock()
	return ch, func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		for i, c := range n.waiters {
			if c == ch {
				n.waiters = append(n.waiters[:i], n.waiters[i+1:]...)
				return
			}
		}
	}
}
