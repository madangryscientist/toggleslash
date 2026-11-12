package toggleslash

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// testConfig is a client that polls and never streams, with short intervals, unless a test changes a field.
func testConfig(url string) config {
	return config{
		url:            url,
		sdkKey:         "fs_server_test",
		stream:         false,
		pollInterval:   50 * time.Millisecond,
		flushInterval:  time.Hour,
		requestTimeout: 2 * time.Second,
		now:            time.Now,
		monotonic:      time.Now,
		jitter:         func() float64 { return 0.5 },
		streamIdle:     defaultStreamIdle,
	}
}

// recordedRequest is one request the fake server saw.
type recordedRequest struct {
	method  string
	path    string
	host    string // net/http reads Host from the request line into r.Host, not into the header map
	headers http.Header
	body    []byte
	at      time.Time
}

// fakeServer is a loopback HTTP server for the native tests. It records every request, header included, and
// answers with handler when a test installs one, or with the default ruleset, stream and events answers.
type fakeServer struct {
	server *httptest.Server
	mu     sync.Mutex
	seen   []recordedRequest
	handle http.HandlerFunc

	flags    string
	version  int
	requests int
	streams  map[chan string]struct{}
}

func newFakeServer(t *testing.T, flags string) *fakeServer {
	t.Helper()
	fake := &fakeServer{flags: flags, version: 1, streams: map[chan string]struct{}{}}
	fake.server = httptest.NewServer(fake)
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeServer) url() string { return f.server.URL }

func (f *fakeServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	f.mu.Lock()
	f.seen = append(f.seen, recordedRequest{
		method:  request.Method,
		path:    request.URL.Path,
		host:    request.Host,
		headers: request.Header.Clone(),
		body:    body,
		at:      time.Now(),
	})
	handler := f.handle
	f.requests++
	f.mu.Unlock()
	if handler != nil {
		handler(writer, request)
		return
	}
	f.defaultAnswer(writer, request)
}

func (f *fakeServer) defaultAnswer(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/sdk/v1/ruleset":
		f.mu.Lock()
		etag := fmt.Sprintf("\"v%d\"", f.version)
		flags := f.flags
		f.mu.Unlock()
		if request.Header.Get("If-None-Match") == etag {
			writer.Header().Set("ETag", etag)
			writer.WriteHeader(http.StatusNotModified)
			return
		}
		writer.Header().Set("ETag", etag)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, fmt.Sprintf(`{"flags":[%s],"segments":[]}`, flags))
	case "/sdk/v1/stream":
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(writer, "event: connected\ndata: %s\n\n", f.etag())
		flush(writer)
		events := make(chan string, 16)
		f.mu.Lock()
		f.streams[events] = struct{}{}
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			delete(f.streams, events)
			f.mu.Unlock()
		}()
		for {
			select {
			case text := <-events:
				_, _ = io.WriteString(writer, text)
				flush(writer)
			case <-request.Context().Done():
				return
			}
		}
	case "/sdk/v1/events":
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"accepted":0,"unknownFlags":0}`)
	default:
		writer.WriteHeader(http.StatusNotFound)
	}
}

func flush(writer http.ResponseWriter) {
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

// set installs a handler that replaces the default answers.
func (f *fakeServer) set(handler http.HandlerFunc) {
	f.mu.Lock()
	f.handle = handler
	f.mu.Unlock()
}

// publish bumps the ruleset version and announces the new tag on every open stream.
func (f *fakeServer) publish(flags string) {
	f.mu.Lock()
	f.flags = flags
	f.version++
	tag := fmt.Sprintf("\"v%d\"", f.version)
	f.mu.Unlock()
	f.announce("event: changed\ndata: " + tag + "\n\n")
}

// etag is the tag the server holds now.
func (f *fakeServer) etag() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fmt.Sprintf("\"v%d\"", f.version)
}

// announce writes text to every open stream.
func (f *fakeServer) announce(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for events := range f.streams {
		select {
		case events <- text:
		default:
		}
	}
}

// openStreams is how many stream bodies the server holds open.
func (f *fakeServer) openStreams() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.streams)
}

func (f *fakeServer) seenRequests() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest{}, f.seen...)
}

func (f *fakeServer) countFor(path string) int {
	count := 0
	for _, request := range f.seenRequests() {
		if request.path == path {
			count++
		}
	}
	return count
}

// flagJSON is one valid boolean flag that serves `serves` at variation 1 and false at variation 0.
func flagJSON(key string, serves bool, version int) string {
	variation := 0
	if serves {
		variation = 1
	}
	return fmt.Sprintf(`{"key":%q,"variations":[false,true],"on":true,"offVariation":0,`+
		`"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":%d},`+
		`"salt":"s","version":%d}`, key, variation, version)
}

// recorder collects what an OnError or a hook reports, safely: a callback runs on the SDK's goroutines.
type recorder struct {
	mu       sync.Mutex
	received []error
}

func (r *recorder) add(err error) {
	r.mu.Lock()
	r.received = append(r.received, err)
	r.mu.Unlock()
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.received)
}

func (r *recorder) messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.received))
	for _, err := range r.received {
		out = append(out, err.Error())
	}
	return out
}

func (r *recorder) first() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.received) == 0 {
		return nil
	}
	return r.received[0]
}

// waitFor polls until check is true, failing the test after the timeout.
func waitFor(t *testing.T, timeout time.Duration, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// selfSignedCertificate mints a certificate for 127.0.0.1 that the test trusts through a pool, so the TLS path
// is exercised with verification intact.
func selfSignedCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "toggle slash test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("minting a certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("reading the certificate back: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}
