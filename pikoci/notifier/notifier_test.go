package notifier

import (
	"sync"
	"testing"
	"time"
)

func TestNotifyWakesWaiters(t *testing.T) {
	n := New()
	ch, cleanup := n.Wait()
	defer cleanup()

	n.Notify()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected waiter to be woken up")
	}
}

func TestCleanupRemovesWaiter(t *testing.T) {
	n := New()
	_, cleanup := n.Wait()
	cleanup()

	n.mu.Lock()
	if len(n.waiters) != 0 {
		t.Fatalf("expected 0 waiters after cleanup, got %d", len(n.waiters))
	}
	n.mu.Unlock()
}

func TestMultipleWaiters(t *testing.T) {
	n := New()

	const count = 5
	channels := make([]<-chan struct{}, count)
	cleanups := make([]func(), count)
	for i := range count {
		channels[i], cleanups[i] = n.Wait()
		defer cleanups[i]()
	}

	n.Notify()

	for i, ch := range channels {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("waiter %d was not woken up", i)
		}
	}
}

func TestNotifyWithoutWaiters(t *testing.T) {
	n := New()
	// Should not panic
	n.Notify()
}

func TestConcurrentNotifyAndWait(t *testing.T) {
	n := New()
	var wg sync.WaitGroup

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, cleanup := n.Wait()
			defer cleanup()
			n.Notify()
			select {
			case <-ch:
			case <-time.After(time.Second):
				t.Error("waiter not woken up")
			}
		}()
	}

	wg.Wait()
}
