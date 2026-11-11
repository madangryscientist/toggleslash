package toggleslash

import (
	"runtime"
	"sync"
)

// goid is the id of the calling goroutine, read from its stack header
// ("goroutine 18 [running]:"). Go has no goroutine-local state, so this is the
// only way to tell that a callback is running on one of the SDK's own
// goroutines. It costs a stack read, so only Ready and Close call it.
func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	text := buf[:n]
	const prefix = "goroutine "
	if len(text) < len(prefix) {
		return 0
	}
	var id uint64
	for _, b := range text[len(prefix):] {
		if b < '0' || b > '9' {
			break
		}
		id = id*10 + uint64(b-'0')
	}
	return id
}

// goroutineSet holds the ids of the goroutines the SDK started.
type goroutineSet struct {
	mu  sync.Mutex
	ids map[uint64]struct{}
}

// enter records the calling goroutine and returns the function that forgets it.
func (s *goroutineSet) enter() func() {
	id := goid()
	s.mu.Lock()
	if s.ids == nil {
		s.ids = map[uint64]struct{}{}
	}
	s.ids[id] = struct{}{}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.ids, id)
		s.mu.Unlock()
	}
}

// contains reports whether the calling goroutine is one of the SDK's.
func (s *goroutineSet) contains() bool {
	id := goid()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.ids[id]
	return ok
}
