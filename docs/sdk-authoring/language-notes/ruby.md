# Ruby

What the first Ruby run learned. Each fact cites that run: prompt `d8735fb`, FS-99, 2026-11-10.

- **Runtime.** MRI (CRuby), minimum 3.3 (the oldest release still in security maintenance in that run; it
  has `Data.define` and `Hash#except`). The GVL makes the interpreter's own state safe, so the design is a
  thread-safety one, not a speed one: the SDK's state is one frozen object swapped under a `Mutex`.
- **No race detector.** A stress test of every operation from many threads with `close` racing, a bound on
  every wait (`Timeout`), stands in. The run covered the two-close race in the same test.
- **Warnings as errors.** `-w` on every file (`ruby -wc`), and `Warning.warn` patched in the test helper to
  fail the run, stand in for `-W error`. A test that patches `Warning` itself must make the warning raise,
  since one that only records it lets a warning in the SDK pass unnoticed (run 29, FS-174).
- **Test and lint tools.** Minitest (ships with Ruby, no dependency) and RuboCop with one pinned
  `.rubocop.yml` (target 3.3, new cops enabled, line length 120). Pin RuboCop as `gem:rubocop` in `mise.toml`.
- **Running RuboCop.** `mise exec -- rubocop` can pick the system ruby and fail with `cannot load such file --
  erb`. Run the gem's script under the pinned ruby with `GEM_HOME` set to `$(mise where gem:rubocop)/libexec`.
  In CI the mise action's `install_args: ruby gem:rubocop` form works and leaves the root `.tool-versions`
  alone.
- **Never run `rubocop -A` over the tree.** `Style/RedundantFreeze` froze counters that are mutated and
  `Style/NumericPredicate` turned a comparison that can return `nil` into `.zero?`, a `NoMethodError`. Fix by
  hand, and disable a cop inline with the reason.
- **Cops that do not exist or fight each other.** `Minitest/MultipleAssertions` does not exist in RuboCop
  1.92.2, so naming it stops the run. `Style/StringLiterals` and `Style/StringLiteralsInInterpolation` must
  be set to the same style or they fight, and `sdks/ruby/.rubocop.yml` sets both to `double_quotes` (run 29,
  FS-174).
- **A blocked `IO.select` cannot be woken from another thread.** In MRI, closing an IO from another thread
  does not wake `IO.select` or a blocked read, so `close` aborts a request with a self-pipe the select
  waits on; and every SDK thread sets `report_on_exception = false` (run 29, FS-174).
- **Unbounded `Integer`.** State the signed 64-bit rule in the integer accessor yourself (contract 4,
  "Typed languages"): `2.0` is `2`, a fraction or a number outside 64 bits is `TYPE_MISMATCH`.
- **Gotchas the vectors found.** `"".split('.', -1)` is `[]`, not `[""]` (a semver of `""`);
  `Float("1e999")` can warn under `-w`, so read a number through `Rational` and `Rational#to_f` (run 29, FS-174);
  a regexp scan over invalid UTF-8 raises, so scan bytes (a `/n` regexp) when parsing untrusted text.
- **No HTTP server in the standard library.** The test service is a hand-written `TCPServer` on
  `127.0.0.1` port 0, one thread per connection.
