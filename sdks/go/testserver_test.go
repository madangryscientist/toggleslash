package toggleslash

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeServer is the one loopback fake server the native tests share. It
// records every request's method, path, headers and body, so the header audit
// and the privacy checks read from it.
type fakeServer struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	requests []requestRecord

	ruleset func(w http.ResponseWriter, r *http.Request, body []byte)
	stream  func(w http.ResponseWriter, r *http.Request, body []byte)
	events  func(w http.ResponseWriter, r *http.Request, body []byte)
}

type requestRecord struct {
	method  string
	path    string
	host    string
	headers http.Header
	body    []byte
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fake := &fakeServer{t: t}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (s *fakeServer) url() string { return s.server.URL }

func (s *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading a request body: %v", err)
	}
	record := requestRecord{
		method:  r.Method,
		path:    r.URL.Path,
		host:    r.Host,
		headers: r.Header.Clone(),
		body:    body,
	}
	s.mu.Lock()
	s.requests = append(s.requests, record)
	handler := s.handlerFor(r.URL.Path)
	s.mu.Unlock()
	if handler == nil {
		http.NotFound(w, r)
		return
	}
	handler(w, r, body)
}

func (s *fakeServer) handlerFor(path string) func(http.ResponseWriter, *http.Request, []byte) {
	switch path {
	case "/sdk/v1/ruleset":
		return s.ruleset
	case "/sdk/v1/stream":
		return s.stream
	case "/sdk/v1/events":
		return s.events
	}
	return nil
}

// recorded answers every request seen on one path.
func (s *fakeServer) recorded(path string) []requestRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]requestRecord, 0, len(s.requests))
	for _, record := range s.requests {
		if record.path == path {
			out = append(out, record)
		}
	}
	return out
}

// count is how many requests reached one path.
func (s *fakeServer) count(path string) int { return len(s.recorded(path)) }
