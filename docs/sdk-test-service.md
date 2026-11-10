# SDK conformance: the test service and the harness

This page is normative for the **test service** an SDK ships and descriptive for the **harness** that drives
it. Together they check a server SDK against [`sdk-contract.md`](sdk-contract.md) from outside, in any
language: the SDK wraps itself in a small HTTP server (the test service), and the harness
(`packages/sdk-conformance`) plays both the application calling the SDK and the ToggleSlash server the SDK
talks to.

```
harness ──(this protocol)──▶ test service ──(SDK calls)──▶ SDK ──(HTTP)──▶ harness's fake server
```

The TypeScript SDK's test service is [`packages/sdk/test-service/main.ts`](../packages/sdk/test-service/main.ts).
No other language has one yet.

## Running it

From the repository root:

```bash
bun run conformance -- --service "bun packages/sdk/test-service/main.ts"
bun run conformance -- --service-url http://127.0.0.1:8123      # a service you started yourself
bun run conformance -- --service "<command>" --only "429"        # scenarios and vectors whose name contains 429
```

With `--service`, the harness runs the command with `sh -c` from the repository root, waits up to 30 s for
the line `listening http://127.0.0.1:<port>` on its stdout, and kills the command's whole process group at
the end. If no such line comes, the harness fails and prints what the service wrote to stderr.

It prints one line per scenario (`pass`, `skip` with the capability it needs, or `FAIL` with the reason,
the steps up to the one that failed, and every request the fake server recorded), then any failed vector,
then a summary:

```
scenarios: 79 passed, 0 failed, 2 skipped; vectors: 1369 passed, 0 failed, 2 skipped
```

It exits 0 when nothing failed, 1 when something did, and 2 on a usage error. `bunx nx test
sdk-conformance` runs the same thing against the TypeScript service, in about 12 s.

## The protocol

The service listens on `127.0.0.1`, prints `listening http://127.0.0.1:<port>` on a line of its own once
it does, and speaks JSON. It holds any number of clients at once; the harness runs several scenarios in
parallel, each with its own client. Durations are whole milliseconds, in fields ending in `Ms`.

| Request | Body | Answer |
| --- | --- | --- |
| `GET /` | | `{ "name": string, "capabilities": string[] }` |
| `POST /clients` | the options below | `201 { "id": string }` |
| `POST /clients/{id}/ready` | | `{ "ready": boolean }` once `ready` settles |
| `POST /clients/{id}/evaluate` | `{ flagKey, context, fallback, accessor }` | `{ value, variationIndex, reason }` |
| `GET /clients/{id}/status` | | the SDK's `status`, as section 8 shapes it |
| `GET /clients/{id}/hook-events` | | `{ "events": [ ... ] }`, every `afterEvaluation` event so far |
| `GET /clients/{id}/errors` | | `{ "count": number, "messages": string[] }`, every `onError` call so far |
| `POST /clients/{id}/close` | | `{}` once `close` has finished |

**Options** for `POST /clients`: `url`, `sdkKey`, `stream` (boolean), `pollIntervalMs`,
`flushIntervalMs`, `requestTimeoutMs`, each passed to the SDK when present. Then three switches the service
implements itself:

- `hook: true` installs an `afterEvaluation` hook that records each event.
- `hookThrows: true` installs one that records each event and then throws.
- `onErrorThrows: true` makes the `onError` the service installs (which always records) throw after
  recording.

**Capabilities.** The only one is `integer-accessor`: the language has an integer type and an integer
accessor (contract section 4, "Typed languages"). A scenario that `requires` a capability the service does
not list is skipped and reported as skipped.

**Accessors.** `evaluate` names the accessor the service calls: `bool`, `string`, `number`, `int` (only
with `integer-accessor`), `json` or `any` (the generic method, or the closest the language has). Each
answers `variationDetail`'s three fields. A service maps these onto its SDK's methods and says how in a
README next to it.

**Hook events** are `{ flagKey, context, value, variationIndex, reason, etag, flagVersion }`, in the
language's own field names mapped back to these.

**Anything the SDK throws** (it MUST NOT, contract 5.1) is the service's to report, not to absorb: it answers
500 with the error text, and the harness fails the scenario. A request the service does not know answers
404.

## Scenarios

`fixtures/sdk/*.json` hold the scenarios, one file per area: `load`, `evaluation`, `stream`, `backoff`,
`rate-limit`, `status`, `usage`, `hooks`, `close` and `privacy`. Each file is `{ "scenarios": [ ... ] }`
and each scenario is:

```json
{ "name": "...", "contract": "7.4", "requires": ["integer-accessor"], "steps": [ ... ] }
```

`contract` names the section the scenario checks; `requires` is optional. Each scenario gets its own fake
server with its own key and one client. The server starts with a ruleset of two boolean flags, `checkout`
(serves `true`, variation 1) and `legacy` (serves `false`, variation 0), with ETag `"v1"`; every change
bumps the tag (`"v2"`, ...).

### Steps

Every step has a `do`. Strings in a step may use `{{url}}`, `{{key}}`, `{{etag}}` (the tag the server
holds now), `{{today}}` (the UTC day) and `{{sentinel}}` (this run's sentinel key, below); the string
`"{{context}}"` on its own is the default context.

| `do` | What it does |
| --- | --- |
| `ruleset` | replaces the server's ruleset and bumps its tag: `flags`, `segments`. A flag `{ "key", "serves": bool }` is a boolean flag; with `"count": n` it is n of them, `{{i}}` in the key numbering them from 0 |
| `script` | queues answers for a `route` (`ruleset`, `stream` or `events`), used in order before the server answers normally. An answer has `status` (200), `body` or `json`, `headers`, `hang` (never answer), `hangBody` (send the headers, never finish the body), `sse` (chunks to write on a stream it opens), `end` (end that stream after them) and `times` (a number or `"always"`) |
| `unscript` | drops what is queued for a `route` |
| `create` | creates the client. Defaults: `url` `{{url}}`, `sdkKey` `{{key}}`, `stream` true, `pollIntervalMs` 30000, `flushIntervalMs` 30000, `requestTimeoutMs` 2000; `options` overrides them |
| `ready` | calls `ready`; `expect` is the answer, `withinMs` and `atLeastMs` bound how long it took |
| `evaluate` | calls `evaluate` with `flag`, `fallback` (required), `context` (default `{{context}}`) and `accessor` (default `any`), and matches the answer against `expect` field by field. `times: n` repeats the call; `each: n` makes n calls with `{{i}}` in the flag key counting from 0; `withinMs` bounds each |
| `close` | calls `close`; `calls: n` makes n calls at once; `withinMs` bounds them |
| `wait` | sleeps `ms` |
| `waitUntil` | runs its checks every 25 ms until they all pass, failing after `withinMs` |
| `expect` | runs its checks once |
| `mark` | records a point named `name`: the requests so far, the time on both clocks and the `onError` count |
| `publish` | sends `event: changed` with the current tag on every open stream |
| `write` | writes `chunks` (strings, or arrays of bytes) to every open stream, 25 ms apart |
| `dropStreams` | ends every open stream |
| `down`, `up` | stops the fake server (connections are refused) and starts it again on the same port |

### Checks

A check is a field on an `expect` or `waitUntil` step.

- `requests` (one object or a list): the requests the server received, filtered by `route` and `since` (a
  mark), and `nth` (one of them; negative counts from the end). Then `count`; per request `answered` (the
  status the server gave), `method`, `path` and `headers` (a `null` value means the header is absent);
  `after` (how long after the mark the first came), `gaps` (one range per gap between consecutive
  requests) and `someGapBelow`; for usage, `events` (the entries, summed by flag, variation and day, must
  equal these) and `batchSizes`.
- `status`: the fields given must match; `lastSyncAt` takes `null`, `"set"` or `{ "after": mark, "before":
  mark }`. Every status read also checks the shape of section 8: four fields, each of its kind, no key.
- `errors`: the `onError` count, a number or a range, optionally `since` a mark.
- `hookEvents`: `count`, and `events`, matched in order field by field.
- `openStreams`: how many streams the server holds open.

A range is `{ "min": n, "max": n }`, either end optional, or a number for exactly that.

### What every scenario checks

After its last step the harness closes the client (if the scenario did not) and checks every request the
fake server recorded:

- **Privacy (section 12).** Each run makes a sentinel context key and a sentinel attribute value, and adds
  the attribute to every context a scenario sends (the default context carries both). Neither may appear
  in any request's path, headers or body, or in an `onError` message, and no `status` may hold the
  sentinel key or the SDK key.
- **Protocol.** Every request goes to one of the three SDK routes with `Authorization: Bearer <key>` and no
  header outside what an HTTP library sends by itself (`accept`, `accept-encoding`, `connection`,
  `user-agent` and the like) plus `if-none-match` and `content-type`. Every usage request is
  `application/json` and exactly `{ "events": [...] }`, each entry exactly `flag`, `variationIndex`, `day`
  and `count`.

### Timing

The harness runs on real time, with short options and wide windows. It never asserts an exact millisecond:
a window allows for the contract's jitter (a wait is 0.5 to 1 of its ceiling), for scenarios running in
parallel and for a slow machine. Two of the contract's times are too long to test this way and are not:
the 60 s quiet-stream watchdog (7.2) and the 5 min cap on `Retry-After` (7.4).

## Evaluation vectors

Every vector in `fixtures/evaluation/` except `buckets.json` (which tests the bucket function alone) also
runs through the service: the fake server serves the vector's flags and segments, the service evaluates its
flag and context with a `null` fallback and the `any` accessor, and the answer must equal the vector's
`value`, `variationIndex` and `reason`. Vectors with the same flags and segments share a client.

Two vectors are skipped, by name:

- `load: flags that is not an array counts as empty`
- `load: segments that is not an array counts as empty`

The evaluator counts those as empty; contract 5.5 makes such a ruleset `bad_ruleset`, so an SDK holds no
ruleset and answers `NOT_READY`. The status scenarios cover what an SDK does with them instead.

## What a scenario cannot reach

- **Section 6, concurrency.** Races between threads are not observable through a request/answer protocol
  with any reliability; each language's own tests own them.
- **The 60 s watchdog** (7.2) and **the 300 s `Retry-After` cap** (7.4): too long for real time.
- **The day boundary** of usage counts (9): it needs a clock the harness does not control.
- **A context or hook that raises inside the SDK's own process** (a throwing getter, a hook returning a
  rejected promise): the protocol carries JSON, which cannot hold one. `EVALUATION_FAILED` is not reached.
- **That `variation` is synchronous** (5.2): only bounded from outside (`withinMs`), not proved.

## Coverage of the TypeScript SDK's tests

Every test in `packages/sdk/test/*.test.ts`, and the scenario that covers it, or why none can. A scenario
named here is in the file named before the colon.

### load.test.ts

| Test | Scenario |
| --- | --- |
| first load fetches the ruleset with the key and serves from it | load: the first load fetches the ruleset with the key and serves from it |
| first load answers variationDetail with the index and the reason | evaluation: targets and rules are evaluated with the evaluator |
| first load evaluates with the engine, rules and all | evaluation: targets and rules are evaluated with the evaluator; every vector |
| first load joins the url without doubling its slash | load: a URL ending in a slash is joined without doubling it |
| is asked for with If-None-Match and a 304 keeps what is held | load: a poll sends If-None-Match with the tag held, and a 304 keeps the ruleset |
| picks up a change on the next poll, then asks with the new tag | load: a change is picked up at the next poll, and the poll after asks with the new tag |
| is not fetched again when the stream announces the tag already held | load: a stream announcing the tag held fetches nothing |
| returns the fallback for a bad context, and says why | evaluation: a context with no key gets the fallback and INVALID_CONTEXT |
| returns the fallback when the served value is not the type the caller expects | evaluation: a value of another kind than the fallback is TYPE_MISMATCH |
| lets a null fallback stand for any type | evaluation: a null fallback accepts a value of any kind |
| returns the fallback when evaluating throws | none: it needs a context whose getter throws, which JSON cannot carry |
| returns the fallback for a flag that is off and has no off variation | evaluation: a flag that is off with no off variation gets the fallback and the reason OFF |
| does not block: it is synchronous and needs no ready() | load: evaluating before the first load answers NOT_READY at once (bounded, not proved synchronous) |

### stream.test.ts

| Test | Scenario |
| --- | --- |
| delivers a change without waiting for a poll | stream: a published change arrives through the stream without a poll |
| ignores keep-alive comments and a tag it already holds | stream: a keep-alive comment and an event naming the tag held fetch nothing |
| reconnects after it drops, and catches up on what it missed | stream: a stream that was up and dropped reconnects without a poll and catches up |
| backs off while the stream will not open, up to the poll interval, and polls meanwhile | backoff: the wait grows while the stream will not open, with one poll between attempts; backoff: the wait never passes the poll interval |
| falls back to polling: a change reaches a client whose stream is down | stream: a change reaches a client whose stream will not open, at the next poll |
| retries a refresh that failed after a change, instead of staying stale on a healthy stream | stream: a refresh that failed after a change is retried, and the stream reopens |
| loads on a healthy stream after a first load that failed | stream: a healthy stream loads the ruleset after a failed first load |
| drops a stream that goes quiet and opens another | none: the watchdog is 60 s |
| starts the backoff over after a stream that connected | backoff: only a stream that connected starts the backoff over |
| serves the caller's fallback when it was never reached, and loads once it is | load: a server never reached serves the fallback, and the first ruleset loaded once it is reached |
| serves the last ruleset when it goes away later, and recovers | stream: a server gone later leaves the last ruleset served, and the SDK recovers when it returns |
| does not hang ready() on a server that never answers | status: a ruleset that never answers is a timeout |
| keeps the last ruleset when the server sends something that is not one | status: an answer that is not a ruleset is bad_ruleset and replaces nothing |
| keeps the last ruleset on a 401 or a 500 | status: a refused ruleset gives its HTTP status; stream: a refresh that failed after a change is retried, and the stream reopens |
| absorbs a fetch that throws at once, and an onError that throws | status: a server that refuses connections is a network error; hooks: an onError that throws is ignored |

### status.test.ts

| Test | Scenario |
| --- | --- |
| reports nothing held before the first load | status: nothing is held before the first load |
| reports the ETag, the sync time and a connected stream after a load | status: after the first load: the tag, a sync time, connected |
| reports off without a stream, and a 304 moves the sync time and keeps the ETag | status: with the stream off a 304 moves the sync time and keeps the tag |
| moves the ETag and the sync time when a published change is loaded | status: a published change moves the tag and the sync time |
| reports a disconnected stream when it drops, and connected again once it reconnects | status: a dropped stream reads disconnected, then connected again |
| holds the ETag through a server that is down, reports a network error, and clears it on recovery | stream: a server gone later leaves the last ruleset served, and the SDK recovers when it returns |
| reports the HTTP status of a refused ruleset | status: a refused ruleset gives its HTTP status |
| reports a bad ruleset when the body has no flags and segments | status: a first answer that is not a ruleset leaves nothing held |
| reports a timeout when the ruleset never answers | status: a ruleset that never answers is a timeout |
| reports closed after close(), and still answers | status: after close the status reads closed and still holds the tag |
| makes no request | status: a keep-alive does not move the sync time, an event naming the tag held does (no request between two status reads) |
| holds no SDK key | every status read (the shape check) |

### hooks.test.ts

| Test | Scenario |
| --- | --- |
| gets the key, context, variation, value, reason, ETag held and flag version | hooks: the hook gets the flag, the context, the result, the tag and the flag version |
| is told the ETag the evaluation used, not a later one | hooks: the hook gets the tag the evaluation used |
| is called for a fallback too, with a null index and a null ETag before the first load | hooks: a fallback before the first load reaches the hook with no index, tag or version |
| reports an unknown flag and a type mismatch with a null flag version only for the first | hooks: an unknown flag has no version, a mismatch keeps the flag's version |
| sends a throwing hook to onError and changes nothing for the caller | hooks: a hook that throws goes to onError, and the evaluation still answers and counts |
| sends a rejected promise to onError, and a hook that also throws from onError is ignored | hooks: an onError that throws is ignored (the rejected promise is not reachable: the hook is the service's) |

### usage.test.ts

| Test | Scenario |
| --- | --- |
| are batched, summed per flag, variation and day, and sent on close | usage: counts are summed per flag, variation and day, and sent on close |
| counts a flag the caller got the fallback for as nothing | usage: a fallback, a mismatch, an unknown flag and an invalid context count nothing |
| splits at the day boundary of the clock | none: it needs a clock the harness does not control |
| are sent every flush interval, not only on close | usage: counts are sent every flush interval, and a flush with nothing to send sends nothing |
| are kept when the server is down and sent, summed, when it is back | usage: counts refused with 5xx, 408 or 429 are kept and sent summed |
| are dropped when the server refuses the batch, since it would refuse it again | usage: counts refused with another 4xx are dropped |
| go in batches the server's limits allow | usage: a flush of more than 500 entries goes in requests of at most 500 |
| do not make close() wait longer than the request timeout | usage: a usage request is bounded by the request timeout, close included |
| close stops all requests, and is safe to call twice | close: close stops every request and is safe to call twice at once |
| close still evaluates afterwards, from the last ruleset, and counts nothing | close: evaluation still answers from the ruleset after close |
| close resolves ready() as false when closed before the first load | close: close before the first load settles ready false |

### rate-limit.test.ts

| Test | Scenario |
| --- | --- |
| %s waits out Retry-After to the millisecond (ruleset, stream, events) | rate-limit: a 429 on the ruleset with Retry-After holds the next poll; a 429 on the stream holds the stream and the ruleset together; a 429 on events holds only the usage reports (to a window, not the millisecond) |
| %s caps a long Retry-After at 5 minutes (ruleset, stream, events) | none: the cap is 300 s |
| ruleset falls back to polling with Retry-After %j | rate-limit: a Retry-After that is not whole seconds is ignored |
| stream falls back to its backoff with no Retry-After | rate-limit: a 429 on the stream with no valid Retry-After falls back to the backoff |
| does not go straight to the stream after a 429 on the first load | rate-limit: a 429 on the first load holds the first stream attempt |
| holds the fetch a stream asked for when it answers 429, and the stream reconnect with it | rate-limit: a 429 on a refresh the stream asked for holds the next refresh and the reconnect |
| keeps answering from the last ruleset while every route is limited | rate-limit: evaluation keeps answering while every route is limited |
| answers the fallback, without throwing, when the first load was limited | rate-limit: a 429 on the first load holds the first stream attempt |
| closes at once during a wait | rate-limit: close does not wait out a Retry-After hold |
| survive a 429 and go out, summed, once it has passed | rate-limit: a 429 on events holds only the usage reports |
| survive a 429 with no Retry-After | rate-limit: a 429 on events with no Retry-After keeps the counts for the next flush |
| are not sent by close during a wait, which says they were lost and does not throw | rate-limit: counts pending at close during a usage hold are lost, and onError is told |
| are sent by close once the wait has passed | rate-limit: counts pending at close are sent once the usage hold has passed |

### sse.test.ts

| Test | Scenario |
| --- | --- |
| reads events the server writes, and skips its keep-alive comments | stream: a keep-alive comment and an event naming the tag held fetch nothing |
| holds an event until its blank line arrives, however the text is cut | stream: CRLF line ends split across reads are read; UTF-8 split across reads does not break the stream |
| treats CRLF and a CRLF split between chunks as line ends | stream: CRLF line ends split across reads are read; a lone CR ends a line |
| joins data lines, defaults the event name and drops an event with no data | stream: several data lines are joined with LF; other events, nameless data and an event with no data are ignored |

Beyond these, the scenarios check what no TypeScript test does: the jitter, a stream that ends before
`connected`, the exact ETag spelling kept, a body that never finishes, a stream that never opens, an event
cut off by a drop, every request's headers, and the privacy of every request.
