package toggleslash

import "sync"

// task is a goroutine Close waits for. A goroutine that is running an application callback is not waited
// for: Close may be called from that very callback, and Go has no goroutine identity to tell it apart, so
// waiting would deadlock. Such a goroutine does nothing more once the callback returns, because the client
// is closed by then.
type task struct {
	done chan struct{}

	mu   sync.Mutex
	busy int           // callbacks running now
	woke chan struct{} // closed, and replaced, each time a callback starts
}

func newTask() *task {
	return &task{done: make(chan struct{}), woke: make(chan struct{})}
}

// callback runs f, an application callback, and tells every waiter not to wait on this goroutine meanwhile.
func (t *task) callback(f func()) {
	t.mu.Lock()
	t.busy++
	close(t.woke)
	t.woke = make(chan struct{})
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.busy--
		t.mu.Unlock()
	}()
	f()
}

// wait returns when the goroutine has finished or is inside a callback.
func (t *task) wait() {
	for {
		t.mu.Lock()
		busy, woke := t.busy > 0, t.woke
		t.mu.Unlock()
		if busy {
			return
		}
		select {
		case <-t.done:
			return
		case <-woke:
		}
	}
}
