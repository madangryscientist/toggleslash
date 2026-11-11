package toggleslash

import (
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPSWorksAndAnUntrustedCertificateFails proves the client speaks TLS and verifies the server: the same
// server answers once its certificate is trusted and is refused as a network failure when it is not.
func TestHTTPSWorksAndAnUntrustedCertificateFails(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sdk/v1/ruleset" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"tls"`)
		_, _ = io.WriteString(w, twoFlags)
	}))
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	trusted := New(srv.URL, testKey, WithStream(false), withRootCAs(pool))
	defer trusted.Close()
	if !trusted.Ready() || trusted.Status().ETag != `"tls"` {
		t.Errorf("trusted: status = %+v", trusted.Status())
	}

	untrusted := New(srv.URL, testKey, WithStream(false))
	defer untrusted.Close()
	if untrusted.Ready() {
		t.Error("an untrusted certificate was accepted")
	}
	if got := untrusted.Status().LastError; got != SyncNetwork {
		t.Errorf("lastError = %q", got)
	}
}
