# Go

What the first Go run learned. Each fact cites that run: prompt `8a59987`, FS-96, 2026-11-10.

- **`encoding/json`.** It errors on `1e999` and replaces a lone surrogate with U+FFFD, which the spec
  forbids for both. Write the JSON reader, and hold strings as WTF-8 so a lone surrogate survives.
- **Headers `net/http` adds itself.** `User-Agent: Go-http-client/<n>`, `Accept-Encoding: gzip`, `Host` and
  `Content-Length`. List each in `RUN.md` with why it identifies no one (CHECKLIST.md, "Code and documents").
- **Redirects.** `http.Client` follows them and may resend `Authorization` on a same-host redirect. The
  run set `CheckRedirect` to return `http.ErrUseLastResponse`, which makes a 3xx an `http_3xx` error.
- **A network error's text carries the URL** (`*url.Error`). Report fixed text only (`network`, `timeout`).
- **Bounding a body read.** `context.WithTimeoutCause` on the request covers the body read as well as the
  connect.
- **A hung test handler must drain the request body first.** `net/http` does not notice a client
  disconnect until the handler reads or writes, so a handler that only sleeps hangs the test.
- **`-race` needs cgo.** A C compiler must exist (it does on `ubuntu-latest`).
- **Lint.** The run used `go vet`, `golangci-lint` and `gofmt -l`. `usestdlibvars` is noisy in table tests
  (it wants `http.StatusOK` for every literal); the run dropped it.
- **Stopping a test.** `pkill -f 'go test'` kills the shell that ran it. Stop a test by its process id.
