# Server SDK contract

This page is normative. It defines what every ToggleSlash **server SDK** does, in no particular
language. The TypeScript SDK (`packages/sdk`, [`sdk.md`](sdk.md)) is the reference implementation: every
rule below that is not marked "new" is something it does today, and where this page and that code were
written apart, the code won. A [conformance harness](sdk-test-service.md) drives an SDK from outside and checks it
against this page; a test of an SDK is a test of this page. To build an SDK in a new language, start from
[`sdk-authoring/PROMPT.md`](sdk-authoring/PROMPT.md).

The words MUST, MUST NOT, SHOULD, SHOULD NOT and MAY are used as in RFC 2119. Every number is stated.
Durations are written in seconds (s) or milliseconds (ms); a language uses its own duration type for
them. Two groups of rules are new and exist because the TypeScript SDK cannot exercise them:
[section 6](#6-concurrency) (runtimes with threads) and the integer accessor rule in
[section 4](#4-evaluation-results) (languages with a separate integer type).

The wire facts (routes, headers, event shapes) come from [`api.md`](api.md) and
[`architecture.md`](architecture.md). Evaluation itself is [`evaluation.md`](evaluation.md) and
[`fixtures/evaluation/`](../fixtures/evaluation), which an SDK passes in full.

## 1. Scope

A server SDK:

1. holds one **server** key (`fs_server_...`), which is bound to one environment of one project;
2. keeps that environment's ruleset in memory, current with the server;
3. evaluates flags **locally** to `evaluation.md`, for a context the caller passes;
4. counts which variations it served and reports the counts.

It is not a client-side SDK. Browser and mobile code, where the server evaluates and a client key is
used, is [`sdk-browser.md`](sdk-browser.md) and out of scope here. A client key cannot read the ruleset,
so a server SDK given one never loads anything (every attempt is `http_403`).

## 2. Naming

The canonical operations are:

| Operation | What it is |
| --- | --- |
| `create` | builds a client from options and starts its loops. It does not wait for the network. |
| `ready` | waits for the first ruleset load to finish; answers whether it worked. |
| `variation` | the flag's value for a context, or the caller's fallback. |
| `variationDetail` | like `variation`, with the variation index and the reason. |
| `status` | what the SDK has loaded and how its stream is doing. |
| `close` | stops every loop and sends the pending usage counts. |

The canonical options are below.

| Option | Default | Meaning |
| --- | --- | --- |
| `url` | required | the server's base URL. Trailing `/` characters are removed. |
| `sdkKey` | required | a server key. |
| `stream` | on | follow `GET /sdk/v1/stream`. Off means poll only. |
| `pollInterval` | 30 s | how often to poll when the stream is down or off, and the longest reconnect wait. |
| `flushInterval` | 30 s | how often usage counts are sent. |
| `requestTimeout` | 10 s | the longest any one request may take, body included. |
| `hooks.afterEvaluation` | none | called after each evaluation ([section 10](#10-hooks-and-errors)). |
| `onError` | ignore | told about every failure the SDK absorbs. |

A language uses its own casing and idioms for all of these: `variation_detail`, `VariationDetail`, an
options struct, a builder, functional options, a duration type in place of the number. It MUST keep the
meaning, the defaults and the names recognisable, and it MUST document its mapping in a table in its
documentation (a README, or for the TypeScript SDK `sdk.md`) that names, for every canonical operation and option, what it is called in that language. A
language MAY add options (an injected HTTP client, a clock or a source of jitter for its tests).

The values the SDK exposes keep their canonical spelling, mapped onto the language's usual type for a
closed set (an enum, a constant, a symbol) without losing the spelling: the reason kinds (`OFF`,
`PREREQUISITE_FAILED`, `TARGET_MATCH`, `RULE_MATCH`, `FALLTHROUGH`, `ERROR`), the error codes of
[section 4](#4-evaluation-results), the stream states and the `lastError` codes of
[section 8](#8-status).

Behaviour for a non-positive or non-numeric duration is not specified; the TypeScript SDK does not check
one. An SDK SHOULD fall back to the default for it, and it is not tested.

## 3. Contexts

A context is a single context or a multi context, exactly as [`evaluation.md`](evaluation.md#context)
defines them, including what is valid. No `kind` means `user`. An SDK MUST accept both shapes, and MAY
add a typed builder for them ([section 13](#13-what-a-language-may-add)). An invalid context is not an
exception: the evaluator answers `ERROR INVALID_CONTEXT`, and the caller gets its fallback.

## 4. Evaluation results

`variationDetail` returns three things: the **value** (the served value, or the caller's fallback), the
**variation index** (a whole number, or null, or the language's "none", whenever the value is the
fallback) and the **reason**.

The reason is one of the kinds of `evaluation.md`. An `ERROR` reason carries a code: the evaluator's
(`INVALID_CONTEXT`, `FLAG_NOT_FOUND`, `SEGMENT_NOT_FOUND`, `MALFORMED_FLAG`, `MALFORMED_SEGMENT`,
`PREREQUISITE_CYCLE`, `PREREQUISITE_DEPTH_EXCEEDED`) or one of the SDK's own:

| Code | When |
| --- | --- |
| `NOT_READY` | no ruleset has loaded yet. |
| `TYPE_MISMATCH` | the flag served a value of a different kind than the fallback. |
| `EVALUATION_FAILED` | the evaluation raised something it should not have. The failure goes to `onError`. |

The result is built in this order:

1. No ruleset held: the fallback, no variation index, reason `ERROR NOT_READY`.
2. Evaluate to `evaluation.md`. If the result has no variation index (an `ERROR`, or `OFF` or
   `PREREQUISITE_FAILED` with no off variation), the caller gets its fallback, no variation index and
   **the evaluator's reason unchanged**.
3. Otherwise compare the served value with the fallback (below). A mismatch gives the fallback, no
   variation index and `ERROR TYPE_MISMATCH`.
4. Otherwise the value, its variation index and the evaluator's reason. This is the only result that is
   counted ([section 9](#9-usage-counts)).
5. If step 2 to 4 raised anything, the caller gets the fallback, no variation index and
   `ERROR EVALUATION_FAILED`.

**The mismatch rule.** Compare JSON kinds only: boolean, number, string, array, object. Array and object
are distinct kinds. There is no integer versus float distinction at this level, so `1` and `1.5` are
both "number". A **null fallback accepts any served value** (it says nothing about the kind the caller
wants). A **null served value against a non-null fallback is a mismatch.**

**Typed languages (new).** A language with a separate integer type MUST apply one more rule to an
integer accessor: if the served number has a fractional part, or is outside the integer type's range,
the caller gets its fallback with `TYPE_MISMATCH`. A language whose integer type is unbounded (Python,
Ruby) uses the signed 64-bit range. `2.0` counts as the integer `2`. This is deliberately
stricter than the TypeScript SDK, which has one number type.

A language MAY choose any of these shapes, and its mapping table names the one it chose: typed methods
(`boolVariation`, `stringVariation`, `intVariation`, `floatVariation`, `jsonVariation` or the language's
equivalents), one generic method over the fallback's type, or both. Whichever it is, each one applies the
rules of this section, and `variationDetail` exists for each.

## 5. Guarantees

1. **It never throws or rejects at a caller.** `create`, `ready`, `variation`, `variationDetail`,
   `status` and `close` MUST NOT raise, throw, panic, return a rejected future or terminate the process,
   whatever the server, the network, the context, a hook or `onError` does. A failure becomes the
   caller's fallback with an `ERROR` reason, or goes to `onError`, or both.
2. **`variation` and `variationDetail` never do I/O and never wait.** They read memory and run the
   evaluator. They MUST NOT block on the network, on a load in progress or on the usage flush.
   `status` is the same.
3. **`ready` settles after the first attempt.** It answers true if a ruleset has loaded and false if
   not, and it does not wait for the server to come back: it is bounded by `requestTimeout`. It MAY be
   called any number of times, before or after the load, and gives the same answer: the answer is fixed when the first attempt settles, so a later load does not
   turn a false into a true. After `close` it
   answers false if it had not settled.
4. **Last good ruleset.** While the server is unreachable, the SDK keeps evaluating from the last ruleset
   it held. At start, before any load, every evaluation is the fallback with `NOT_READY` and the SDK keeps
   trying.
5. **Only a ruleset replaces a ruleset.** An answer that is not a ruleset (a bad gateway page, `{}`, a
   truncated body, a JSON value that is not an object) is an error and MUST NOT replace the ruleset
   held. A ruleset here is a JSON object whose `flags` and `segments` are both arrays; anything else is
   `bad_ruleset`. Inside that shape, a bad flag or segment is the evaluator's business, not the loader's
   (`evaluation.md`, "Loading a ruleset").

The time bounds in this section hold for work the SDK controls; an application callback (`onError`, a hook)
that blocks, blocks whatever called it.

## 6. Concurrency

New, and a MUST for every language it names.

**Languages with threads (or goroutines, tasks on a thread pool, fibres on several cores):**

1. Every operation in [section 2](#2-naming) MUST be safe to call from many threads at once, including
   `close` racing `variation`, and two `close` calls racing each other.
2. Evaluation MUST NOT take a lock that a network call holds, and MUST NOT wait for one. A slow or hung
   server cannot slow an evaluation down.
3. The ruleset swap MUST be atomic: a reader sees the old ruleset or the new one, never a mix. The ruleset
   and the ETag that names it SHOULD swap together, so the `etag` a hook reports belongs to the ruleset
   the evaluation used.
4. The SDK's callbacks (`afterEvaluation`, `onError`) SHOULD NOT be called while the SDK holds a lock of
   its own, so a callback that calls back into the client cannot deadlock it.
5. `status` SHOULD be one consistent snapshot, not four reads at four moments.
6. A client that is never closed SHOULD NOT keep a process alive by itself where the runtime has the idea
   (a daemon thread, a background task).

**Single-threaded runtimes (an event loop):** nothing a callback does may stall the loop beyond its own
run. `variation` MUST NOT yield to the
loop or wait on a promise, future or timer.

## 7. Sync protocol

The SDK keeps a **ruleset** and the **ETag** the server gave it. All requests carry
`Authorization: Bearer <sdkKey>` and go to `url` plus the path below. The URL is not parsed beyond
removing trailing `/` characters.

### 7.1 The ruleset

`GET /sdk/v1/ruleset`. If the SDK holds a ruleset and an ETag it sends `If-None-Match: <the ETag, exactly
as received, quotes included>`; it sends the header in no other case.

| Answer | What the SDK does |
| --- | --- |
| `200` | read the body as JSON. If it has the shape of [section 5](#5-guarantees) item 5, build the ruleset, keep the `ETag` response header (or none if it has none), and it is a **sync**. Else `bad_ruleset`. |
| `304` | the ruleset held is current: a **sync**. The body is not read. |
| `429` | read `Retry-After` ([7.4](#74-429-and-retry-after)), then the `http_429` error. |
| anything else | the `http_<status>` error. |
| a network failure | `network`. |
| no answer within `requestTimeout` | `timeout`. The limit covers the whole request, **reading the body included**. |

A **sync** sets `lastSyncAt` to now and clears `lastError`. An error sets `lastError` and calls `onError`,
and leaves the ruleset and ETag as they were.

At `create` the SDK does one ruleset request at once, without waiting for any timer. The end of that
request, whether or not it worked, is the end of the first load, and settles `ready` (true if a ruleset
is now held).

### 7.2 The stream

When `stream` is on, after the first load the SDK opens `GET /sdk/v1/stream` with
`Accept: text/event-stream`. It is Server-Sent Events:

- `event: connected` with `data:` the ruleset's ETag, once, when the stream opens;
- `event: changed` with `data:` the new ETag, on each change;
- a comment line `: keep-alive` every 25 s.

The `data` is the ETag exactly as the ruleset route's `ETag` header gives it, quotes included, and is all
an event carries.

**Reading events.** Parse Server-Sent Events as the WHATWG standard does (LF, CRLF and a lone CR end a line, a
line starting with `:` is a comment, UTF-8 and line endings may be split across reads). The SDK reads only the
`event` and `data` fields: several `data` lines of one event are joined with LF, an event with no `data` line
is dropped, and the event name is `message` when none was given. An event still incomplete when the stream
ends is discarded. The SDK acts on `connected` and `changed` and ignores any other event.

**On `connected` or `changed`**, with `tag` the data:

1. `connected` sets the stream state to `connected` and clears `lastError`.
2. If a ruleset is held and `tag` equals the ETag held (an exact string comparison), it is a **sync**
   and nothing is fetched.
3. Otherwise the SDK does the ruleset request of [7.1](#71-the-ruleset). If that request fails, the SDK
   ends this stream at once and treats it as **not connected** for the backoff below: a stream that stays
   up would never retry the fetch.

**Bounds.**

- Opening the stream is bounded by `requestTimeout`, from the request until the response headers arrive.
  Past that: `timeout`.
- Once open, a **watchdog of 60 s**: the SDK drops the stream if no bytes at all arrive for 60 s. Every
  read, a keep-alive comment included, restarts the 60 s. Dropped for it: `timeout`.
- A status other than `200`, or an answer with no body, is `http_<status>` (and a `429` is read as in
  [7.4](#74-429-and-retry-after)). A stream the server ends cleanly is not an error and leaves `lastError`
  alone.
- Content types are not checked.

### 7.3 The loop

After the first load the SDK runs this until `close`:

```
failures = 0
loop:
    if stream is on: wait until any Retry-After hold has passed (7.4)
    connected = stream is on ? follow the stream (7.2) : false
    failures  = connected ? 0 : failures + 1
    wait delay(failures)                    (and at least until any Retry-After hold has passed)
    if not connected: do the ruleset request (7.1)    # one poll
```

`connected` is true only if the stream's `connected` event arrived **and** the stream was not ended by a
failed fetch. So:

- a stream that **never connected** (would not open, was refused, was dropped before `connected`, or
  failed the fetch `connected` asked for) waits, then **polls once**, then tries the stream again;
- a stream that **was up and dropped** waits, then tries the stream again **without a poll**: reconnecting
  announces the tag with `connected`, which catches up on its own;
- while the stream is up, the SDK never polls. (A deployment where something buffers the stream is noticed
  only when the 60 s watchdog drops it; such a deployment should turn `stream` off.)

`delay(failures)`:

- `stream` off: `pollInterval`, every time. The loop is then: wait `pollInterval`, poll, repeat.
- `stream` on: `ceiling = min(pollInterval, 1 s * 2^min(failures, 16))`, and the wait is `ceiling`
  multiplied by a jitter drawn uniformly from `[0.5, 1)`. So the wait is between 50 % and 100 % of the
  ceiling. With the default `pollInterval` of 30 s the ceilings are 2 s after a first failure, then 4,
  8, 16 and 30 s. A stream that was up and dropped (`failures` is 0) has a ceiling of 1 s, so it waits 0.5
  to 1 s.

The poll after the wait does not reset `failures`; only a stream that connected does.

### 7.4 429 and Retry-After

Only a `429` answer is read for `Retry-After`. The header counts only if it is whole decimal seconds
(one or more decimal digits and nothing else; surrounding spaces are ignored); a date, a fraction or a negative number is
ignored, and the usual backoff is then the only wait. The wait is **capped at 5 minutes** (300 s).

A `429` from the ruleset route or the stream route sets a hold: nothing is sent to **either** until the
hold has passed, which is a time, not a sleep. Before each stream attempt the SDK waits it out, and each
wait of the loop above lasts at least until it. A later `429` can lengthen the hold and never shortens
it. The first load is not held (nothing could have told the SDK yet). The `429` is an error as in
[7.1](#71-the-ruleset): `http_429` in `lastError`, and `onError` is called. Evaluation keeps answering
from the ruleset held. A `429` on the events route holds only the usage reports
([section 9](#9-usage-counts)).

## 8. Status

`status` is synchronous, reads memory, makes no request and never fails. It returns four fields:

| Field | Value |
| --- | --- |
| `etag` | the ETag of the ruleset held, as received; none before the first load. |
| `lastSyncAt` | when the SDK last confirmed its ruleset matches the server, as a point in time in the language's type (the TypeScript SDK gives epoch milliseconds); none if never. |
| `stream` | one of `off`, `connecting`, `connected`, `disconnected`, `closed`. |
| `lastError` | why the most recent ruleset or stream attempt failed, or none. |

**`stream`:**

- `off`: `stream` is off. It stays `off` until `close`.
- `connecting`: the first load, and each stream attempt while it is opening.
- `connected`: the stream's `connected` event arrived and the stream is still open.
- `disconnected`: the last stream attempt ended, by failing or by dropping, and the SDK is waiting to
  retry, polling meanwhile as [7.3](#73-the-loop) says.
- `closed`: after `close` has been called, whatever it was before, `off` included.

**`lastError`** is one of exactly these codes, and none otherwise:

| Code | Meaning |
| --- | --- |
| `http_<status>` | the server answered a status other than the expected one: `http_401`, `http_403`, `http_429`, `http_500` and so on. |
| `timeout` | the request exceeded `requestTimeout`, the stream did not open within it, or went 60 s without a byte. |
| `bad_ruleset` | a `200` whose body is not JSON, or is not a ruleset. |
| `network` | the connection failed, or any other failure. |

`lastError` is set when a ruleset or stream attempt fails, and **cleared** by the next sync (a `200`, a
`304`, a stream event naming the tag held) and by a stream `connected` event. A stream that ends cleanly
leaves it alone. Nothing sets it, and `onError` is not called, for a failure caused by `close`
(the aborted requests).

**`lastSyncAt`** moves on a `200`, a `304`, and a stream event whose tag equals the one held. **It does
not move on a keep-alive**, so an old `lastSyncAt` with `stream` at `connected` is normal: nothing has
changed. It does not move on an error, and it is never reset.

**What status never contains:** a URL, a header, a key, an error message, a context, a flag key or a
count. Usage failures are not in it either; they go to `onError`. It is local: the server never
sees it.

## 9. Usage counts

**What counts.** An evaluation counts when the caller got the flag's value from the ruleset: the result of
[section 4](#4-evaluation-results) step 4, meaning a variation index exists and the kind matched. It does
not count a fallback, a `TYPE_MISMATCH`, `NOT_READY`, `EVALUATION_FAILED`, an unknown flag or any other
`ERROR`. A hook that fails does not change it. `variation` and `variationDetail` count once per call.

**What is kept.** Counts are summed per (flag key, variation index, UTC day). The day is the UTC date at
the moment of the evaluation, as `yyyy-MM-dd`. Recording is an in-memory add, cheap enough for a request
path.

**What is sent.** `POST /sdk/v1/events` with `Content-Type: application/json` and the key as above, body:

```json
{ "events": [ { "flag": "new-checkout", "variationIndex": 1, "count": 12, "day": "2026-11-10" } ] }
```

Exactly those four fields, and nothing else: no context, no context key, no attribute, no key. (The
server refuses an entry with any other field.) The server takes 1000 events and 128 KiB a request; the
SDK sends **at most 500 entries per request**. A flush with more than 500 entries is several requests, one
after another. A flush with nothing pending sends no request.

**When.** Every `flushInterval` (the first flush one interval after `create`; the next one interval after
the previous flush finished), and on `close`. Flushes are serial: one that starts while another is running
waits for it.

**Results of a request.**

| Answer | Counts in that request |
| --- | --- |
| `2xx` | sent. The next request goes out. |
| `5xx`, `408`, `429`, a network failure, a timeout | **kept** for the next flush, together with the entries after it that were not tried. The flush stops there. |
| any other answer (a `400`, a `401`, a `413`, a `404`) | **dropped**: the server would refuse them again. The flush goes on with the next request. |

Every failed request calls `onError`. Counts kept are merged with counts recorded since. A request is
bounded by `requestTimeout`.

**429.** A `429` with a valid `Retry-After` ([7.4](#74-429-and-retry-after)) holds the usage reports: no
flush sends anything until the hold has passed (a flush during it returns at once, keeping its counts).
This hold is separate from the ruleset's. A `429` without a valid `Retry-After` is kept and retried at the
next flush, with no hold.

**After close.** Nothing is counted after `close` is called. See [section 11](#11-close).

## 10. Hooks and errors

**`afterEvaluation`** is called after every `variation` and `variationDetail`, a fallback included, once
per call, **on the caller's thread, synchronously, before the call returns**, with an event:

| Field | Value |
| --- | --- |
| `flagKey` | the key the caller asked for. |
| `context` | the context the caller passed (the SDK itself sends none of it anywhere). |
| `value` | what the caller is about to get. |
| `variationIndex` | as `variationDetail`; none for a fallback. |
| `reason` | as `variationDetail`. |
| `etag` | the ETag of the ruleset held at this evaluation, as received (the same value as `status`'s `etag` then); none before the first load. |
| `flagVersion` | the flag's `version` in that ruleset, or none when the ruleset has no such flag or the flag is malformed. |

The hook's return value is ignored. A hook that is not callable is ignored. **A hook never reaches the
caller**: an error it raises (or, in a runtime with futures, a future it later fails) goes to `onError`,
and the caller gets the same answer, and the evaluation is counted, with or without the hook.

A hook that blocks blocks only its own caller; that is the application's choice. The SDK does not run a
hook on its own threads.

**`onError`** receives every failure the SDK absorbs, as the language's error value: a failed ruleset or
stream attempt (not after `close`), a failed usage request, an evaluation that raised, a hook that raised,
and counts lost at `close` ([section 11](#11-close)). It is for logging and metrics and is never needed
for correctness. **If `onError` itself raises, that is ignored**, with nothing else done about it. Which
thread calls it is the SDK's choice, and the language's mapping documents it.

## 11. Close

`close` MUST, in order:

1. stop following the server: no new ruleset request, stream attempt or poll starts, and the ones in
   flight are abandoned;
2. stop the flush loop;
3. wait for the loops and for a flush already running;
4. send the final usage flush ([section 9](#9-usage-counts)). It is bounded by `requestTimeout` per
   request, and it does not wait out a `Retry-After`: if a hold is still in force, the counts still
   pending are lost, and `onError` is told (the TypeScript SDK's message is "closed with N usage counts
   unsent: the server asked to wait", where N is the number of pending entries, one per flag, variation and
   day, not the sum of the counts).

`close` never fails, and it is **safe to call twice** (or concurrently): every call waits for the same
shutdown and returns when it is done. A `ready` that had not settled settles false.

After `close` begins: evaluation **still answers from the ruleset held**; nothing is counted; `status`
reads `closed` for `stream`; no ruleset or stream request reaches the server (only the usage requests of step 4 and one already in flight); and nothing reaches `onError` for the
ruleset and stream requests that `close` abandoned.

## 12. Privacy

Nothing about an end user leaves the process through the SDK. The only requests are the three above
(ruleset, stream, events), to the one `url`, carrying the key and, for events, counts. No context, context
key or attribute is in any request, a log line, `status`, or an error value the SDK itself builds for
`onError`. (An error a hook or the evaluator raised is passed to `onError` as raised; a hook is the
application's code, and it receives the context.) The SDK
sends **no telemetry of its own**: no analytics, no crash reports, no check for a newer version. It MAY
send the standard headers of its HTTP library (a default `User-Agent`); it MUST NOT add a header that
identifies a user or a machine. A context goes to a hook only because the application wrote the hook.

## 13. What a language MAY add

An SDK MAY add idiomatic extras that change none of the above:

- a logger adapter (a function that turns `onError` values into the language's logging);
- a context builder or typed context classes;
- typed accessors or a generic accessor ([section 4](#4-evaluation-results));
- a `flush` operation that sends the pending counts now, under the rules of
  [section 9](#9-usage-counts);
- options and test seams for its own tests (an injected HTTP client, clock or jitter source);
- an OpenFeature provider, in a separate package, that evaluates nothing itself;
- a tracing hook, in a separate package, built on `afterEvaluation`.

An extra MUST NOT make a guarantee of [section 5](#5-guarantees) weaker, and MUST NOT send anything the
contract says the SDK does not send.
