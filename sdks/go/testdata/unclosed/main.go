// Command unclosed creates a client against a server that answers, never closes it, and returns
// from main. The process must exit: an unclosed client holds no thing that keeps it alive.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

	toggleslash "github.com/madangryscientist/toggleslash/sdks/go"
)

func main() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sdk/v1/ruleset" {
			w.Header().Set("ETag", `"v1"`)
			_, _ = fmt.Fprint(w, `{"flags":[],"segments":[]}`)
			return
		}
		if r.URL.Path == "/sdk/v1/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "event: connected\ndata: \"v1\"\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	c := toggleslash.New(srv.URL, "key")
	if !c.Ready() {
		fmt.Fprintln(os.Stderr, "not ready")
		os.Exit(3)
	}
	fmt.Println("done")
	// No Close: main returns and the process must end.
}
