# Build a ToggleSlash server SDK

You are building a **server SDK** for ToggleSlash, a source-available feature flag service, in one language,
inside this repository. Nobody answers questions while you work. Where this prompt leaves something open on
purpose, decide and write the reason down; where it is unclear, assume, record it as a guess, and go on.
Both go in your run report (phase 6), which the next version of this prompt is written from.

## Your input

Your brief names one required value and up to three optional ones:

| Input | Required | When it is absent |
| --- | --- | --- |
| language | yes | |
| runtime (CPython or PyPy, the JVM or GraalVM, MRI or JRuby) | no | the language's current mainstream runtime |
| minimum version | no | the oldest version still supported upstream (in security support) that has what you need |
| package manager or build tool | no | the language's current mainstream choice |

Every value you choose because it was absent is a **decision**: say why in `DESIGN.md` and list it under
decisions in `RUN.md`. It is not a guess.

The SDK lives in `sdks/<lang>`, where `<lang>` is fixed: `python`, `go`, `java`, `csharp`, `ruby`, and for
any other language its common lowercase name with no symbols (`cpp`, `fsharp`, `kotlin`). Every command
below runs from the repository root with this set first:

```sh
L=sdks/<lang>        # for example L=sdks/go
```

## What done means

The SDK meets [`docs/sdk-contract.md`](../sdk-contract.md) (the contract) and evaluates flags exactly as
[`docs/evaluation.md`](../evaluation.md) (the spec) says. Two executable checks prove most of it: every
vector in `fixtures/evaluation/`, run by your own tests, and the conformance harness, run against your test
service. **A passing harness is necessary and not sufficient.** It cannot see threads, long timers, the day
boundary or a failure inside your own process; [`CHECKLIST.md`](CHECKLIST.md) lists what it cannot see, and
you walk it in phase 6.

## Repository rules

These hold for every file you write, code, comments, docs, commit messages and the PR included.

1. **No em dash (U+2014) and no en dash (U+2013), anywhere.** Use a full stop, comma, colon or brackets; for
   a range write "to". `bun scripts/check-dashes.ts` fails on one, and CI runs it on every tracked file.
2. **Only `$L/` and `.github/workflows/sdk-<lang>.yml` change.** Nothing else in the repository: not the
   contract, the spec, the harness, the scenarios, the vectors, this directory, `AGENTS.md`, `package.json`
   or another SDK. If you need another change, do not make it: report it under proposed changes, or under
   stuck points if it blocks you.
3. **No publishing.** No registry account, no package upload, no name reservation, no tag. Publishing is
   a human step.
4. **Scratch goes under `$TMPDIR`**, never a hard-coded `/tmp`. Tests that need a directory make one there
   and remove it.
5. **Commits are Conventional Commits** (`<type>(<scope>): <subject>`). Use the subject your brief gives. If
   it gives none, use `chore(sdk-<lang>): a <Language> server SDK built from the authoring prompt`. Never
   `feat`: CI fails a `feat` commit that does not change `docs/feature-parity.json`, which rule 2 keeps you
   out of. The planner updates that file.
6. **No `Co-Authored-By` trailer and no AI attribution line** in a commit, the PR or any file.
7. **Secrets come from the environment, never the tree.** The SDK and its tests need none: the harness
   makes up its own keys.
8. **Branch from `origin/main`** and land as the "Landing" section of [`AGENTS.md`](../../AGENTS.md) says;
   where your brief adds landing steps (a ticket line, a handoff), follow those too. Your gate is the checks
   of phases 0 to 6, not the `AGENTS.md` gate.

## Entry points

Every SDK has the same three executable `sh` scripts, so CI and people run any language the same way:

| Script | Does | Exits |
| --- | --- | --- |
| `$L/bin/lint` | the language's lint and format check, read only | 0 when clean |
| `$L/bin/test` | builds, then runs the native tests, the vectors included | 0 when all pass |
| `$L/bin/test-service` | starts the test service in the foreground | runs until killed |

Each starts with `#!/bin/sh` and `set -eu`, does `cd "$(dirname "$0")/.."` before anything else, calls
each tool through `mise exec -- <tool>` when `mise` is on `PATH` (a mise shim fails in a directory where
its tool is not active, and the root `.tool-versions` is a different set) and plainly otherwise (in CI a
setup step puts the tools on `PATH`), and needs nothing beyond what `$L/mise.toml` pins and a stock
`ubuntu-latest` runner has (`sh`, a C compiler). Build output goes in a directory `$L/.gitignore` ignores.
`bin/test` builds every time (the test service included; in an interpreted language that is a byte-compile or
syntax check of every source, test and test-service file, so a syntax error in a file no test imports still fails), never reports a cached result, runs under the
language's race detector where it has one, and prints `vectors: <n> pins: <n>` even when all pass. `bin/lint`
runs native tools only. `bin/test-service` ends with `exec` so the harness's kill reaches the service itself. In a
compiled language `bin/test` builds the test service's binary as its last step, and `bin/test-service` runs that binary and builds
it only when it is missing (the harness waits 30 s for `listening`, which a cold compile can exceed). `chmod +x`
all three and commit them with the executable bit.

## Phase 0: read and set up

Read these in this order. Read the contract and the test-service page in full; you will come back to them.

1. [`docs/sdk-contract.md`](../sdk-contract.md): every rule your SDK keeps. Sections 4 to 12 are the work.
2. [`docs/evaluation.md`](../evaluation.md): the evaluator you port in phase 2. Read "Reading JSON",
   "Patterns", "Bucketing" and "The vectors" twice: they say what a JSON parser, a regex engine and a hash
   input must and must not do, and those are where ports go wrong.
3. `fixtures/evaluation/*.json`: open two files (`clauses.json`, `buckets.json`) to see the shapes.
4. [`docs/sdk-test-service.md`](../sdk-test-service.md): the protocol your test service speaks, how the
   harness runs, and "What a scenario cannot reach".
5. `fixtures/sdk/*.json`: skim the scenario names; they are the contract as tests.
6. `packages/sdk/test-service/main.ts` and its `README.md`: the reference test service, about 140 lines.
7. [`docs/sdk.md`](../sdk.md): the TypeScript SDK's page, an example of the mapping table you will write.
8. [`docs/api.md`](../api.md), "SDK read API" only: the wire, where the contract leaves a detail open.
9. `docs/sdk-authoring/language-notes/<lang>.md`, if it exists: what earlier runs learned. It never
   overrides the contract. If there is none, say so in `RUN.md`: every language choice is then a decision.
10. [`CHECKLIST.md`](CHECKLIST.md) and [`RUN-TEMPLATE.md`](RUN-TEMPLATE.md): what you are checked against.

Record the prompt version for your report before you change anything:

```sh
git log -1 --format=%H -- docs/sdk-authoring
```

Then set up. Pin the toolchain in `$L/mise.toml`: a `[tools]` table with the exact current stable release
(look it up with the toolchain manager, such as `mise ls-remote <tool>`, and the vendor's release or support
page, and record what you used in `RUN.md`; do not copy a version from this page), plus any build or lint tool the `bin/` scripts call
that mise installs. That pin is what you develop and test on. For a language with long-term-support releases, pin the
newest LTS and say so in `DESIGN.md`. The minimum version is separate: the SDK declares it in its
package metadata, and `DESIGN.md` says why. A release whose support ends on the run date or earlier is not in support. Trust and install the pin, and install the
repository's own packages, which the harness needs:

```sh
bun install --frozen-lockfile
(cd "$L" && mise trust && mise install)
```

**Check.** `(cd "$L" && <version command>)`, such as `go version`, prints the pinned version.

## Phase 1: design

Write `$L/DESIGN.md` before any code. It holds:

1. **The mapping table** (contract section 2). A Markdown table, one row per name (not combined rows as in
   `docs/sdk.md`), whose first cell is written exactly `` | `name` | `` with one space either side and no
   padding, and whose other columns say what it is in your language. One row each for
   the six operations (`create`, `ready`, `variation`, `variationDetail`, `status`, `close`), the eight
   options (`url`, `sdkKey`, `stream`, `pollInterval`, `flushInterval`, `requestTimeout`,
   `hooks.afterEvaluation`, `onError`, each with its default), and two more: `accessor shape` (typed
   methods, one generic method, or both: contract section 4, and which accessors exist) and `onError thread`
   (which thread or task calls `onError`: contract section 10). Say under the table how the closed sets of contract
   section 2 (reason kinds, error codes, stream states, `lastError` codes) appear, keeping their spelling,
   and how an option whose default is on or non-zero (`stream`, the intervals) tells "not given" from
   false or zero.
2. **Concurrency** (contract section 6): what holds the ruleset and its ETag and how one atomic swap
   replaces both, which threads or tasks the SDK starts and how `close` stops and joins them, which locks
   exist and why no evaluation can wait on one a request holds, and that no SDK lock is held while
   `afterEvaluation` or `onError` runs. A single-threaded runtime says how `variation` never yields.
3. **HTTP and SSE.** The HTTP client, and how `requestTimeout` bounds a whole request **body included**
   (contract 7.1; many clients bound only the connect or each read, which the harness catches). How the
   stream is opened with only its open bounded by `requestTimeout`, and the 60 s watchdog on reads after
   that (7.2). How Server-Sent Events are parsed to 7.2's rules; an SSE library is acceptable only if it
   keeps all of them, and a short hand-written parser usually does.
4. **Dependencies.** Runtime dependencies: none where the standard library has HTTP and JSON. Each one that
   remains gets a line saying why the standard library will not do. Test and lint dependencies are listed
   with their purpose.
5. **JSON.** The parser, and how it keeps what "Reading JSON" and "The vectors" in the spec require:
   numbers as IEEE-754 binary64 (`1e999` is Infinity, not an error; `9007199254740993` reads as
   `9007199254740992`), `-0.0`, and lone surrogates (`"\ud800"`) kept as they are, and how your string type
   holds one. This covers the context too: the vectors' contexts carry `1e999` and lone surrogates, and
   reach the SDK through the test service, so the public API must accept a context in a form that keeps
   them (the SDK's own JSON value type, or raw JSON beside a native map). If the standard parser cannot,
   say what you do instead.
6. **The minimum runtime** and why; the **toolchain** choices (lint and format tool, test framework, build
   tool), each the language's current mainstream one, with the reason. The integer accessor (phase 4 says which
   languages have one) uses the language's own integer type, the 64-bit
   signed one where there are several.
7. **The package layout**: the directories under `$L`, and where the evaluator, the client, the tests and
   the test service live. The package or module name follows the language's convention for a package
   inside a repository; where that convention puts a host in the name, use this repository's
   `github.com/madangryscientist/toggleslash/sdks/<lang>`. Where the convention is reverse-domain (Maven,
   and similar), use the reverse-domain form of that repository path. Nothing is registered under it.
   Where `<lang>` is not a valid package name (Go's `go` is a keyword), name the package `toggleslash`.

**Check.** Every required row exists (prints nothing and exits 0):

```sh
missing=0
for n in create ready variation variationDetail status close url sdkKey stream pollInterval \
  flushInterval requestTimeout hooks.afterEvaluation onError 'accessor shape' 'onError thread'; do
  grep -qF "| \`$n\` |" "$L/DESIGN.md" || { echo "missing row: $n"; missing=1; }
done
test "$missing" = 0
```

## Phase 2: evaluator

Port [`docs/evaluation.md`](../evaluation.md) into the SDK as its own module with no network code in it.
The spec and the vectors are normative. You MAY read the two existing implementations as references,
`packages/engine/src` (TypeScript) and `apps/server/ToggleSlash.Evaluation` (C#). Where either, or any
other implementation in this repository you read, disagrees with the spec or the contract, the spec or the
contract wins and you record the disagreement in `DESIGN.md` with both readings and the file and line of
each. It is not a guess: the rule settles it.

- **`matches` follows the spec's own matcher** ("Patterns": a parser to a small program run as a Pike VM),
  never the language's regular expression engine, not even as a fast path. The spec says why: platform
  engines differ on `\d`, `\w`, `.`, Unicode and anchors, and a backtracking engine is a denial of service
  on the evaluation path.
- **Hash with SHA-256 from the standard library** if it has one, over the bytes "Bucketing" names.
- **Strings are code points** where the spec says so (patterns, and lone surrogates in hashing). Know what
  your language's string type is (bytes, UTF-16 units, code points) before you write either.

Write a native test that, at test time, reads every file in `fixtures/evaluation/` from the repository
(by a path relative to the test, such as `../../fixtures/evaluation`; never a copy), and:

1. for every file but `buckets.json`, loads each vector's `flags` and `segments` with the JSON
   parser the client uses, into the evaluator's ruleset (before the client's contract 5.5 shape check:
   the two `load:` vectors the harness skips for that check run here), evaluates `flagKey` for `context`, and compares
   `value`, `variationIndex` and `reason` with `expected`, deeply, by the spec's equality (numbers by
   IEEE-754, so `-0` equals `0`), naming the file and vector on a failure;
2. for `buckets.json`, checks both `uint32` and `bucket` for every pin;
3. fails if it read no files or no vectors, and reports how many it ran.

Create `$L/bin/test` now.

**Check.** All pass, and the counts match the files (the second command prints vectors, then pins). Write the
client after this check passes.

```sh
"$L/bin/test"
bun -e 'const d="fixtures/evaluation/",f=require("fs"),r=n=>JSON.parse(f.readFileSync(d+n,"utf8"));let v=0;for(const n of f.readdirSync(d))if(n!=="buckets.json")v+=r(n).vectors.length;console.log(v,r("buckets.json").buckets.length)'
```

## Phase 3: client

Build the client on the evaluator, to the contract, section by section: options and naming (2), contexts
(3), results and the mismatch rule (4), the guarantees (5), concurrency (6), the sync protocol with the
stream, the loop, the backoff and `Retry-After` (7), status (8), usage counts (9), hooks and `onError`
(10), close (11) and privacy (12). Contract section 13 says what you may add. Test seams (an HTTP client,
a clock, a jitter source, a shorter watchdog, an evaluator that raises) are options on `create`, which may
be internal to the package, or the language's standard test facility for fake time. Never a global or an
environment variable the SDK reads.

Write native unit tests for what the harness cannot reach: the "Native tests" lines of
[`CHECKLIST.md`](CHECKLIST.md). Test long timers through the seams, never by waiting real minutes. Step a fake clock only when the SDK is
waiting on it (for example count the clock's armed timers), never after a fixed real pause: a pause passes on a quiet
machine and fails on a loaded one.

**Check.** All native tests pass, the phase 2 vectors included:

```sh
"$L/bin/test"
```

## Phase 4: test service

Write `$L/test-service/` to the protocol in [`docs/sdk-test-service.md`](../sdk-test-service.md), and
`$L/bin/test-service` to start it. Put the test service where your build tool compiles it with the
tests (where the build is several projects, it is one more project), and say where in `$L/test-service/README.md`. The reference is `packages/sdk/test-service/main.ts`.
What the protocol page says and a cold reader misses:

- Listen on `127.0.0.1`, port `0` (the OS picks), and print `listening http://127.0.0.1:<port>` with the
  real port on a line of its own, **then flush stdout**: it is a pipe, not a terminal, and an unflushed line
  never arrives. The harness waits 30 s for it, build time included, so `bin/test-service` must not run a
  long build: run the output `bin/test` built, and rebuild only when it is missing.
- Serve requests concurrently: scenarios run in parallel, and `POST /clients/{id}/ready` waits while other
  requests for the same client go on.
- Pass the context through as the JSON it arrived as. The SDK, not the service, answers an invalid one.
- Absorb no error the SDK returns or raises: answer 500 with the error text. Where the HTTP server would instead drop
  the connection (a panic), catch at the handler only to send that 500. Record every `onError` call and
  every hook event, and keep a client answerable after `close`.
- Map `accessor` onto your SDK's methods; write the mapping in `$L/test-service/README.md`, as
  `packages/sdk/test-service/README.md` does. `any` with a `null` fallback must work and return any JSON
  kind: every vector runs through it.
- **Capabilities.** Advertise `integer-accessor` when the language has an integer type distinct from its
  floating type: Python's `int` and Ruby's `Integer` count, as do Go, Java and C#. The fractional-part rule
  of contract section 4 then applies. The range rule applies to every SDK that
  advertises it, including Python and Ruby, whose integers are unbounded: bound the accessor to signed 64
  bits, because the `huge` scenario in `evaluation.json` (1e20) expects `TYPE_MISMATCH`.

**Check.** The harness exits 0. Its summary line reads `0 failed` twice, the only skipped scenarios are
those a capability you do not advertise rules out (none if you advertise `integer-accessor`, otherwise 2),
and the vectors show exactly the 2 named skips:

```sh
"$L/bin/test" && bun run conformance -- --service "$L/bin/test-service"
```

To work on one area, add `--only "<text>"`. A failing scenario prints its steps and every request the fake
server recorded; read those before you change code.

## Phase 5: package

1. `$L/.gitignore`: the language's build and cache output (phase 6's scope check stages with `git add -A`).
2. `$L/README.md`: a quickstart (install from a checkout of this repository, with no tag, create, `variation`, `close`), the
   mapping table or a link to it in `DESIGN.md`, the guarantees as a short list (contract section 5), and
   how to run `bin/lint`, `bin/test` and the harness. It names no registry and no published package.
3. `$L/bin/lint`: the native lint and format check, with no warnings allowed. The repository's own
   formatter, `oxfmt`, checks every TOML, YAML and JSON file, `$L/mise.toml` included, through `bun run
   format:check` (not `bin/lint`): format those with `bunx oxfmt <file>`. If a native tool and `oxfmt` disagree about one file, that is a stuck point.
4. `.github/workflows/sdk-<lang>.yml`, from this skeleton. Fill in only the toolchain step: pin every
   action by its full commit SHA with the tag in a comment, as `ci.yml` does (`gh api
   repos/<owner>/<repo>/commits/<tag> --jq .sha` resolves one; an action `ci.yml` does not use, such as
   `jdx/mise-action`, is resolved the same way, that answer is the pin, and a sibling `sdk-<lang>.yml` that
   uses it may be read for its inputs), and install the version `$L/mise.toml` pins
   and every tool the `bin/` scripts call. Either the language's official setup action, with the same
   version as `mise.toml` and a comment saying so, or `jdx/mise-action` limited to the tools in
   `$L/mise.toml` (unlimited, it also installs the root `.tool-versions`: .NET and a second Bun). Which one
   is a decision. For `jdx/mise-action` the inputs are `working_directory: sdks/<lang>` and `install_args` naming
   the tools, so a run need not read a sibling's workflow. Copy the checkout and Bun lines and `BUN_VERSION` from `ci.yml` as they are when you run.

```yaml
name: SDK (<Language>)

on:
  pull_request:
    paths: &paths
      - "sdks/<lang>/**"
      - "fixtures/**"
      - "docs/sdk-contract.md"
      - "docs/sdk-test-service.md"
      - "packages/sdk-conformance/**"
      - ".github/workflows/sdk-<lang>.yml"
  push:
    branches: [main]
    paths: *paths

permissions:
  contents: read

concurrency:
  group: sdk-<lang>-${{ github.ref }}
  cancel-in-progress: true

env:
  BUN_VERSION: <as in ci.yml>

jobs:
  sdk:
    name: <Language> SDK (lint, test, conformance)
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: <the actions/checkout line from ci.yml>
        with:
          persist-credentials: false
      - uses: <the oven-sh/setup-bun line from ci.yml>
        with:
          bun-version: ${{ env.BUN_VERSION }}
      # The toolchain step: the version sdks/<lang>/mise.toml pins.
      - run: bun install --frozen-lockfile
      - run: sdks/<lang>/bin/lint
      - run: sdks/<lang>/bin/test
      - run: bun run conformance -- --service sdks/<lang>/bin/test-service
```

**Check.** Lint and tests pass, and the repository's own format and dash checks pass over your files (the
dash check reads only files git tracks, hence the `git add`):

```sh
"$L/bin/lint" && "$L/bin/test" && git add -A && bun run format:check && bun scripts/check-dashes.ts
```

## Phase 6: review and report

1. Walk [`CHECKLIST.md`](CHECKLIST.md) line by line: check it, fix what fails, note the evidence (a test
   name, a command, a file and line). A line that does not apply says why; one you cannot meet is a stuck
   point.
2. Write `$L/RUN.md` from [`RUN-TEMPLATE.md`](RUN-TEMPLATE.md). Be candid: a guess you leave out is a gap
   the next run falls into again. A decision is a choice this prompt left open on purpose; a guess is a
   point where the prompt or a document should have told you and did not. Before listing one, search the
   contract, the protocol page, the checklist and the language notes for it: a point one of them answers is not a
   guess (cite it in `DESIGN.md` if it mattered). A tool the prompt does not name (a test framework, a linter,
   a type checker), and its configuration (a rule set, an ignore list), is a decision, not a guess. Something
   the prompt asks for that is slow or costly is not a guess either.

**Check.** The report has every section, and nothing outside your two paths changed:

```sh
for s in 'Prompt version' Results Decisions Guesses 'Stuck points' 'Proposed prompt changes' Checklist; do
  grep -q "^## $s" "$L/RUN.md" || echo "RUN.md lacks: $s"
done
W=".github/workflows/sdk-${L#sdks/}.yml"
git add -A && git diff --cached --name-only "$(git merge-base HEAD origin/main)" \
  | grep -v -e "^$L/" -e "^$W\$" || echo "scope ok"
```

It must print only `scope ok`.

## When you are stuck

A contract, harness, scenario or vector bug that blocks you (the harness and the contract disagree, a
vector contradicts the spec): stop. Do not change those files and do not code around the bug. Write
`RUN.md` with the blocker under stuck points, quoting both sides, and say in your PR or handoff that the
run is blocked. Anything less than a blocker is a guess: assume, record it, and go on.
