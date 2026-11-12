package toggleslash

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

// allowedHeaders are the ones the protocol names plus the ones net/http sends on its own. Anything else could
// identify a user or a machine (contract 12).
var allowedHeaders = map[string]bool{
	"authorization":     true,
	"accept":            true,
	"if-none-match":     true,
	"content-type":      true,
	"content-length":    true,
	"user-agent":        true,
	"accept-encoding":   true,
	"host":              true,
	"connection":        true,
	"transfer-encoding": true,
	"te":                true,
	"date":              true,
}

func TestTheHeadersOnEachRoute(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	// One client follows the stream; the other polls, so its second ruleset request is the conditional one.
	streaming := start(t, server, func(cfg *config) { cfg.stream = true })
	polling := start(t, server, func(cfg *config) {
		cfg.pollInterval = 20 * time.Millisecond
		cfg.flushInterval = 30 * time.Millisecond
	})
	if !streaming.Ready() || !polling.Ready() {
		t.Fatal("a first load failed")
	}
	waitFor(t, 2*time.Second, "a connected stream", func() bool {
		return streaming.Status().Stream == StreamConnected
	})
	_ = polling.BoolVariation("checkout", Context{"key": "u"}, false)
	waitFor(t, 2*time.Second, "a conditional ruleset request", func() bool {
		for _, request := range server.seenRequests() {
			if request.path == "/sdk/v1/ruleset" && request.headers.Get("If-None-Match") == `"v1"` {
				return true
			}
		}
		return false
	})
	waitFor(t, 2*time.Second, "a usage request", func() bool {
		return server.countFor("/sdk/v1/events") > 0
	})
	streaming.Close()
	polling.Close()

	routes := map[string]bool{}
	// The table in RUN.md is this log, which is captured from a real loopback request per route.
	captured := map[string][]string{}
	for _, request := range server.seenRequests() {
		route := request.path
		if route == "/sdk/v1/ruleset" && request.headers.Get("If-None-Match") != "" {
			route += " (conditional)"
		}
		routes[route] = true
		if request.headers.Get("Authorization") != "Bearer fs_server_test" {
			t.Errorf("%s did not carry the key as a bearer token", request.path)
		}
		for name := range request.headers {
			if !allowedHeaders[lower(name)] {
				t.Errorf("%s carried a header outside the allowed set: %s", request.path, name)
			}
		}
		if _, seen := captured[route]; !seen {
			names := make([]string, 0, len(request.headers))
			for name := range request.headers {
				names = append(names, lower(name))
			}
			sort.Strings(names)
			lines := []string{fmt.Sprintf("host=%q", request.host)}
			for _, name := range names {
				lines = append(lines, fmt.Sprintf("%s=%q", name, request.headers.Get(name)))
			}
			captured[route] = lines
		}
	}
	for route, lines := range captured {
		t.Logf("%s: %s", route, strings.Join(lines, " | "))
	}
	for _, want := range []string{"/sdk/v1/ruleset", "/sdk/v1/ruleset (conditional)", "/sdk/v1/stream", "/sdk/v1/events"} {
		if !routes[want] {
			t.Errorf("no request reached %s", want)
		}
	}
}

func lower(text string) string {
	out := []byte(text)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 'a' - 'A'
		}
	}
	return string(out)
}

func TestATrustedCertificateWorksOverHTTPS(t *testing.T) {
	certificate, pool := selfSignedCertificate(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("ETag", `"v1"`)
		_, _ = writer.Write([]byte(`{"flags":[` + flagJSON("checkout", true, 1) + `],"segments":[]}`))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	t.Cleanup(server.Close)

	client := start(t, &fakeServer{server: server}, func(cfg *config) {
		cfg.url = server.URL
		cfg.roundTripper = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	})
	if !client.Ready() {
		t.Fatalf("the https load failed: %v", client.Status())
	}
	if got := client.BoolVariation("checkout", Context{"key": "u"}, false); !got {
		t.Error("the https load served nothing")
	}
}

func TestAnUntrustedCertificateIsANetworkError(t *testing.T) {
	certificate, _ := selfSignedCertificate(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"flags":[],"segments":[]}`))
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	t.Cleanup(server.Close)

	var reported recorder
	client := start(t, &fakeServer{server: server}, func(cfg *config) {
		cfg.url = server.URL
		cfg.requestTimeout = 2 * time.Second
		// No second attempt while the first error is checked.
		cfg.pollInterval = 10 * time.Second
		cfg.onError = reported.add
	})
	if client.Ready() {
		t.Fatal("an untrusted certificate was accepted")
	}
	// ready and the onError call for one failed attempt have no set order, so the report is waited for.
	waitFor(t, 2*time.Second, "the handshake failure to be reported", func() bool {
		return reported.count() > 0
	})
	if got := client.Status().LastError; got != LastErrorNetwork {
		t.Errorf("lastError was %q, expected network", got)
	}
	var carried *Error
	if !errors.As(reported.first(), &carried) || carried.Code != LastErrorNetwork {
		t.Errorf("the handshake failure was reported as %v, expected the network code", reported.first())
	}
}

func TestTheURLIsJoinedWithoutDoublingTheSlash(t *testing.T) {
	cases := []struct {
		base, path, want string
		ok               bool
	}{
		{"http://127.0.0.1:8080", "/sdk/v1/ruleset", "http://127.0.0.1:8080/sdk/v1/ruleset", true},
		{"http://127.0.0.1:8080///", "/sdk/v1/ruleset", "http://127.0.0.1:8080/sdk/v1/ruleset", true},
		{"https://example.test/prefix", "/sdk/v1/stream", "https://example.test/prefix/sdk/v1/stream", true},
	}
	for _, one := range cases {
		got, err := buildURL(one.base, one.path)
		if !one.ok {
			if err == nil {
				t.Errorf("%q was accepted", one.base)
			}
			continue
		}
		if err != nil || got != one.want {
			t.Errorf("buildURL(%q) gave %q, %v", one.base, got, err)
		}
	}
	for _, base := range []string{"", "   ", "ftp://example.test", "example.test", "http://"} {
		if _, err := buildURL(base, "/sdk/v1/ruleset"); err == nil {
			t.Errorf("%q was accepted", base)
		}
	}
}
