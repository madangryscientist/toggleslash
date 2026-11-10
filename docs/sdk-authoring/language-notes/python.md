# Python

What three Python runs learned: FS-95 (prompt `647f849`), FS-109 (prompt `7dc76f1`) and FS-111 (prompt `5540f5e`), all
2026-11-10. A fact marked "(FS-109)" or "(FS-111)" comes from that run; the rest from the first.

- **Big integer literals.** `json` hands back Python ints of any size. The spec reads a JSON number as
  binary64, so a served `2**63-1` is `2**63` and `TYPE_MISMATCH` on the integer accessor. Turn an integer
  literal beyond plus or minus 2^53 into a float while parsing, and keep
  the interpreter's integer-digit limit away from a literal of thousands of digits.
- **`http.client` and the stream watchdog.** It sets `connection.sock` to `None` after `getresponse()` for
  an answer that ends with the connection, so a `settimeout` call on it afterwards goes nowhere and the 60 s
  watchdog never fires. A seam-only test passed while this was broken; the loopback-socket test caught it.
- **No race detector.** Run the tests under `python -X dev -W error`, stress `close` against readers with
  a `threading.Barrier`, and use `Thread.join(timeout)` in the test so a deadlock fails instead of hanging.
- **`.gitignore`.** List `__pycache__/`, `.ruff_cache/`, `build/`, `dist/` and `*.egg-info/`.
- **No type checker is required (FS-109).** `ruff check` and `ruff format`, pinned in `mise.toml`, are the lint.
- **unittest needs nothing installed (FS-109).** `python -m unittest discover -s tests -t tests` finds `test_*.py`.
- **A `bool` is an `int` (FS-109).** A kind check tests `bool` before number, or `True` passes as a number.
- **A stream test server needs no special case for the `sock` quirk (FS-111).** A response with no `Content-Length`
  that is not chunked reproduces the `sock` is `None` case above.
- **A swallowed error on purpose (FS-111).** `ruff` accepts `contextlib.suppress` for it (rules `SIM105` and `S110`).
