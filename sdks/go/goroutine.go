package toggleslash

import (
	"bytes"
	"runtime"
	"strconv"
	"sync"
)

// currentGoroutine is the id of the calling goroutine, read from its stack header ("goroutine 18 [running]:").
// Go has no API for it. It is used only by Close and Ready, never on the evaluation path.
func currentGoroutine() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	header := bytes.TrimPrefix(buf[:n], []byte("goroutine "))
	if end := bytes.IndexByte(header, ' '); end >= 0 {
		header = header[:end]
	}
	id, err := strconv.ParseUint(string(header), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// goroutineSet holds the ids of the goroutines the SDK itself runs. A callback running on one of them must not
// wait for the SDK to stop, since it would be waiting for itself.
type goroutineSet struct {
	mu  sync.Mutex
	ids map[uint64]struct{}
}

// add registers the calling goroutine and returns the function that removes it.
func (s *goroutineSet) add() func() {
	id := currentGoroutine()
	s.mu.Lock()
	if s.ids == nil {
		s.ids = make(map[uint64]struct{})
	}
	s.ids[id] = struct{}{}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.ids, id)
		s.mu.Unlock()
	}
}

// onSDKGoroutine reports whether the calling goroutine is one the SDK runs.
func (s *goroutineSet) onSDKGoroutine() bool {
	id := currentGoroutine()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.ids[id]
	return ok
}
