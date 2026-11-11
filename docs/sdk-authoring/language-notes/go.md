# Go

What the Go runs learned. Each fact cites its run: prompt `8a59987`, FS-96, 2026-11-10, unless it is marked
(FS-115), the run at prompt `180a02d` on 2026-11-11, (FS-124), the run at prompt `8527564` on 2026-11-11, or
(FS-126), the run at prompt `26834d8` on 2026-11-11, or (FS-131), the run at prompt `6125364` on 2026-11-11, or (FS-133), the run at prompt `2a00dd1` on 2026-11-11, or (FS-135), the run at prompt `9fc2851` on 2026-11-11.

- **`encoding/json`.** It errors on `1e999` and replaces a lone surrogate with U+FFFD, which the spec
  forbids for both. Write the JSON reader, and hold strings as WTF-8 so a lone surrogate survives.
- **Headers `net/http` adds itself.** `User-Agent: Go-http-client/<n>`, `Accept-Encoding: gzip` (not sent when the transport's
  `DisableCompression` is true), `Host` and `Content-Length`. `Connection` did not show on a loopback request
  (FS-131). List each in `RUN.md` with why it identifies no one (CHECKLIST.md, "Code and documents").
- **Redirects.** `http.Client` follows them and may resend `Authorization` on a same-host redirect. The
  run set `CheckRedirect` to return `http.ErrUseLastResponse`, which makes a 3xx an `http_3xx` error.
- **A network error's text carries the URL** (`*url.Error`). Report fixed text only (`network`, `timeout`).
- **Bounding a body read.** `context.WithTimeoutCause` on the request covers the body read as well as the
  connect.
- **A hung test handler must drain the request body first.** `net/http` does not notice a client
  disconnect until the handler reads or writes, so a handler that only sleeps hangs the test.
- **`-race` needs cgo.** A C compiler must exist. This is the PROMPT.md "Entry points" rule, not a second one: a Go run pins `zig` in its
  `mise.toml`, so one file names every tool, and its workflow installs it with the rest (FS-115, FS-133). Name it by path, `CC="$(mise where zig)/zig cc"`: a `zig` elsewhere on PATH (such as
  `~/.local/bin`) can shadow the pinned one, and `mise exec --` does not undo that (FS-126).
- **`mise ls-remote go` can hide the newest releases.** Pin the newest version it lists and say so in
  `RUN.md` (FS-115).
- **Lint.** The run used `go vet`, `golangci-lint` and `gofmt -l`. `usestdlibvars` is noisy in table tests
  (it wants `http.StatusOK` for every literal); the run dropped it.
- **Stopping a test.** `pkill -f 'go test'` kills the shell that ran it. Stop a test by its process id.
- **Printing the vector and pin counts.** `bin/test` runs `go test -v -run '^(TestVectors|TestBucketPins)$' ./internal/evaluator`,
  because the vectors and the bucket pins are counted by two tests and plain `go test` hides `t.Logf` on a pass, and prints the one
  line `vectors: <n> pins: <n>` that PROMPT.md "Entry points" requires (FS-131, FS-135).
- **Errors collected by `onError`.** `Ready()` can settle before `onError` runs, so a test that reads them after
  `Ready()` waits for them (FS-131).
- **A test callback that needs the client.** It can run before `New` returns, so read the client through an
  `atomic.Pointer`, not a plain variable (FS-131).
- **The SDK's own goroutine (contract 6.4).** Go has no goroutine identity; do not parse ids out of `runtime.Stack`. Each goroutine the
  SDK starts marks itself (an atomic counter) while it runs an application callback, and `Close` does not wait for a goroutine so
  marked, since it is doing nothing more until the callback returns (FS-135).
- **A recovered panic is a raised error.** A panic recovered from a hook or the evaluator is passed to `onError` as an error that carries
  the panic's value: contract section 12's "An error a hook or the evaluator raised is passed to `onError` as raised" covers it (FS-135).
