package toggleslash

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPSWorksAndAnUntrustedCertificateIsANetworkError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk/v1/ruleset" {
			w.Header().Set("ETag", `"t1"`)
			_, _ = w.Write([]byte(`{"flags":[` + boolFlag("a", true, 1) + `],"segments":[]}`))
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Trusted through the seam: the server's own certificate pool.
	pool := srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	c := New(srv.URL, testKey, WithStream(false), WithRequestTimeout(2*time.Second),
		func(cfg *config) {
			cfg.tweakTransport = func(tr *http.Transport) {
				tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
			}
		})
	defer c.Close()
	if !c.Ready() || !c.BoolVariation("a", user, false) {
		t.Fatalf("not served over TLS: %+v", c.Status())
	}

	// Not trusted: the default transport rejects the self-signed certificate, and the error text is fixed.
	errs := &errorLog{}
	d := New(srv.URL, testKey, WithStream(false), WithRequestTimeout(2*time.Second), WithOnError(errs.add))
	defer d.Close()
	if d.Ready() {
		t.Fatal("ready over an untrusted certificate")
	}
	if d.Status().LastError != LastErrorNetwork {
		t.Fatalf("lastError = %q", d.Status().LastError)
	}
}
