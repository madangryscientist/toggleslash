# Flag evaluation

This page is normative. It defines what a flag evaluates to, for every implementation:
`packages/engine` (TypeScript) today and `apps/server` (C#) next. The shared vectors in
[`fixtures/evaluation/`](../fixtures/evaluation) are the executable form of it. If an implementation
and this page disagree, the implementation is wrong; if this page and a vector disagree, that is a
bug in the spec, and both change together.

Nothing here may depend on one language's runtime. Each comparison below says what it is in terms of
bytes, code units or IEEE-754 values, and where a platform default would differ, it says so.

## Data model

A ruleset is **one environment's** flags and segments, as JSON. The per-environment fields
(`on`, `offVariation`, `targets`, `prerequisites`, `rules`, `fallthrough`) are already resolved for
that environment by the time they get here.

### Reading JSON

- **Null and absent are the same** for every optional field and for an attribute's value.
- **Numbers are IEEE-754 binary64.** So `9007199254740993` reads as `9007199254740992`, and `1.0` and
  `1` are the same number.
- **An integer** is a finite number with no fractional part. `1.0` is a valid variation index. A weight
  of `0.5` is not valid.
- **A count** is an integer that is 0 or more.
- **A key** is a non-empty string. A `salt` may be empty.
- Unknown fields are ignored.

### Flag

| Field | Type | Notes |
| --- | --- | --- |
| `key` | key | |
| `variations` | array of any JSON value, at least one | `null` is a legal value |
| `on` | boolean | |
| `offVariation` | index into `variations`, optional | absent serves `null` when the flag is off |
| `targets` | array of target | required, may be empty |
| `prerequisites` | array of prerequisite | required, may be empty |
| `rules` | array of rule | required, may be empty |
| `fallthrough` | serve | required |
| `salt` | string | part of the bucket hash |
| `version` | count | carried for the API; evaluation does not read it |

- **Target:** `{ "variation": <index>, "contextKeys": [<string>...] }`.
- **Prerequisite:** `{ "flagKey": <key>, "variation": <count> }`. The variation is an index into the
  *prerequisite's* variations, so it is not range-checked here; one that does not exist can never be
  met.
- **Serve:** exactly one of `"variation": <index>` or `"rollout": <rollout>`.
- **Rollout:** `{ "variations": [{ "variation": <index>, "weight": <count> }...] }`, at least one
  entry. Weights are thousandths of a percent and sum to exactly 100000. A weight of 0 is legal, and a
  variation may be listed twice.
- **Rule:** `{ "clauses": [<clause>...] }` plus a serve. All clauses must match. No clauses always
  matches.

### Clause

An attribute clause is `{ "attribute": <key>, "op": <op>, "values": [...], "negate": <boolean> }`.
`op` is one of `in`, `contains`, `startsWith`, `endsWith`, `lt`, `lte`, `gt`, `gte`. `negate` is
optional. Each value is a string, a finite number or a boolean; anything else is malformed.

A segment clause is `{ "op": "segmentMatch", "values": [<string>...], "negate": <boolean> }`. Its
values are segment keys. An `attribute` on it is ignored.

There is no regex op, by design: a regex is a ReDoS risk on a hot path, and JavaScript and .NET do not
agree on regex semantics. An unknown op makes the flag malformed.

### Segment

| Field | Type | Notes |
| --- | --- | --- |
| `key` | key | |
| `salt` | string | |
| `included` | array of string | |
| `excluded` | array of string | |
| `rules` | array of segment rule | |

A segment rule is `{ "clauses": [<attribute clause>...], "weight": <count up to 100000> }`. `weight` is
optional. A `segmentMatch` clause inside a segment rule makes the segment malformed, so segments do not
nest and cannot form a cycle.

### Context

```json
{ "key": "user-1", "attributes": { "plan": "pro", "age": 30, "tags": ["a", "b"] } }
```

`key` is a required key. `attributes` is an optional object. There is one context kind.

### Loading a ruleset

Flags and segments are validated once, when the ruleset is loaded, and loading never fails: a bad
entry becomes an `ERROR` when it is evaluated, and a `flags` or `segments` that is not an array counts
as empty, as does a ruleset that is not an object. If two flags or two segments share a key, the first one
in the array wins, even when it is the malformed one. An entry with no string `key` cannot be referred
to, and is dropped.

## Evaluation

`evaluate(ruleset, flagKey, context)` returns a result and **never throws**. This is the order, and the
first step that decides, decides:

1. **Context.** Not an object, a missing, empty or non-string `key`, or `attributes` that is neither an
   object nor absent: `ERROR INVALID_CONTEXT`. This comes first, before the flag is looked up.
2. **Lookup.** No such flag: `ERROR FLAG_NOT_FOUND`.
3. **Header.** `key`, `on`, `variations` or `offVariation` invalid: `ERROR MALFORMED_FLAG`. These are
   what serving the off variation needs.
4. **Off.** `on` is false: `OFF`, serving `offVariation`. An off flag is a kill switch, so it comes
   before the rest of the validation: a bad rule, a missing segment or a broken prerequisite cannot
   stop it.
5. **Body.** The whole flag is checked, not only the path this context would take, so a flag either
   evaluates for every valid context or errors for every one of them. Anything invalid in the fields
   above: `ERROR MALFORMED_FLAG`. Otherwise each `segmentMatch` value in rule, clause and value order:
   missing is `ERROR SEGMENT_NOT_FOUND`, malformed is `ERROR MALFORMED_SEGMENT`; the first bad reference
   decides the code.
6. **Prerequisites**, see below. `PREREQUISITE_FAILED`, serving `offVariation`, or the prerequisite's
   `ERROR`.
7. **Targets.** The first target in array order that lists the context key: `TARGET_MATCH`.
8. **Rules.** The first rule in array order whose clauses all match: `RULE_MATCH`, with its index in
   `rules`.
9. **Fallthrough.** `FALLTHROUGH`.

## Result

```json
{ "value": true, "variationIndex": 1, "reason": { "kind": "RULE_MATCH", "ruleIndex": 0 } }
```

| `reason` | Extra field | `value`, `variationIndex` |
| --- | --- | --- |
| `{ "kind": "OFF" }` | | the off variation, or `null`, `null` if there is none |
| `{ "kind": "PREREQUISITE_FAILED", "flagKey": "..." }` | the direct prerequisite that failed | the off variation, or `null`, `null` |
| `{ "kind": "TARGET_MATCH" }` | | the served variation |
| `{ "kind": "RULE_MATCH", "ruleIndex": 0 }` | index in `rules` | the served variation |
| `{ "kind": "FALLTHROUGH" }` | | the served variation |
| `{ "kind": "ERROR", "code": "..." }` | the code | `null`, `null` |

A variation whose value is `null` is served with its index, so `variationIndex` tells it apart from "no
off variation".

### Error codes

| Code | Meaning |
| --- | --- |
| `INVALID_CONTEXT` | the context is not valid |
| `FLAG_NOT_FOUND` | the requested flag, or a prerequisite, does not exist |
| `SEGMENT_NOT_FOUND` | a `segmentMatch` names a segment that does not exist |
| `MALFORMED_FLAG` | a flag does not fit this page |
| `MALFORMED_SEGMENT` | a segment a flag refers to does not fit this page |
| `PREREQUISITE_CYCLE` | a flag is its own prerequisite, directly or not |
| `PREREQUISITE_DEPTH_EXCEEDED` | more than 32 prerequisite links from the requested flag |

## Prerequisites

Prerequisites are checked in array order, and the first one that is not met stops the walk, so a cycle
behind it is never seen.

A prerequisite is **met** when evaluating that flag for the same context reached a serve (`TARGET_MATCH`,
`RULE_MATCH` or `FALLTHROUGH`) and served the required variation. So an off prerequisite is not met,
even when its off variation is the required one, and neither is one whose own prerequisite failed.

An `ERROR` from a prerequisite is the result of the whole evaluation, with the same code: a missing
prerequisite is `ERROR FLAG_NOT_FOUND`. A failure is `PREREQUISITE_FAILED`, and `flagKey` is always the
requesting flag's direct prerequisite, not the flag deeper down that caused it.

**Entering a flag** checks these in this order. Implementations must keep this order, since vectors
pin it:

1. A flag that already finished in this evaluation returns that same result.
2. A flag still being evaluated: `ERROR PREREQUISITE_CYCLE`.
3. More than 32 links from the requested flag, which is at 0: `ERROR PREREQUISITE_DEPTH_EXCEEDED`.
4. Lookup: `ERROR FLAG_NOT_FOUND`.

**Each flag evaluates at most once per evaluation.** The reuse in step 1 is part of the contract, not an
optimization: without it a ladder of diamonds costs exponential time. It also makes the depth check
depend on order. If `x` is 33 links down one path and 1 link down another, the prerequisites `[x, a1]`
succeed and `[a1, x]` error. A cycle longer than the limit reports the depth error. An off flag ends
the walk at step 4 of evaluation, so an off flag in a cycle is `OFF`, not a cycle.

The limit also bounds recursion. No ruleset can overflow an implementation's stack.

## Clauses

### Reading the attribute

- `attribute` is the literal name. A dot is not a path: `a.b` reads the attribute named `a.b`.
- `key` always reads the context key. An attribute in `attributes` named `key` is ignored.
- Only the object's own properties count. `toString` and `constructor` are missing unless the context
  sets them, and `__proto__` is an ordinary name. In a language with prototypes, use an own-property
  check.

### Matching

1. **A missing or null attribute fails the clause, whatever `negate` says.** `negate` asks about a value
   the context has; "not on the pro plan" must not match a context that never said what plan it is on.
2. **An array attribute** is one candidate per element. It has the attribute, even when empty. Nested
   arrays, objects and nulls inside it never match anything.
3. **The base match** is true when any candidate matches any value under the op. No candidates or no
   values means false.
4. **`negate: true` flips the base match.** So a present attribute of the wrong type is a non-match, and
   under `negate` a match; an empty array and empty `values` also become a match.

An object attribute is present and never matches. There is no coercion anywhere: `"5"` is not `5`,
`0` is not `false`.

A number that is not finite never matches. JSON can spell one (`1e999` overflows binary64 to Infinity),
and it reads as a present attribute of the wrong type. .NET must check `double.IsFinite`, as JavaScript
checks `Number.isFinite`.

### Ops

For an attribute candidate `a` and a clause value `v`:

| Op | True when |
| --- | --- |
| `in` | `a` and `v` have the same type (string, number or boolean) and are equal |
| `contains` | both strings, and `v` occurs in `a` |
| `startsWith` | both strings, and `a` begins with `v` |
| `endsWith` | both strings, and `a` ends with `v` |
| `lt`, `lte`, `gt`, `gte` | both finite numbers, or both strings, and `a` orders that way against `v` |

- **Number equality and order are IEEE-754.** `1` equals `1.0`, and `-0` equals `0`.
- **String equality and the three substring ops are ordinal:** the same UTF-16 code units, case-sensitive,
  with no Unicode normalization. `é` is not `e` plus U+0301. A soft hyphen is not ignorable. An empty
  value matches any string, and no number. In .NET pass `StringComparison.Ordinal` every time: the
  default `StartsWith(string)` and `EndsWith(string)` are culture-sensitive, and that is the difference
  these vectors catch.
- **String order is by UTF-16 code unit,** `string.CompareOrdinal` in C# and `<` in JavaScript, never a
  culture's collation. So `"B" < "a"`, `"10" < "9"`, and `"😀"` (0xD83D first) sorts before `"～"`
  (0xFF5E), which code point order would reverse. ISO-8601 dates in one format order correctly as
  strings, which is how a date comparison is expressed.
- **Booleans have no order.** `lt` and friends never match them.

### segmentMatch

True when the context is in any listed segment. `negate` flips it, and a missing attribute cannot arise.
No values means false, and under `negate` true.

**In a segment,** in this order:

1. The context key in `excluded`: not in the segment.
2. The context key in `included`: in the segment.
3. Any rule whose clauses all match, and whose `weight` is absent or above
   `bucket(segmentKey, segmentSalt, contextKey)`: in the segment.
4. Otherwise not in the segment.

## Bucketing

```text
bucket(key, salt, contextKey) =
  first 4 bytes of SHA-256(UTF-8 bytes of "<key>.<salt>.<contextKey>"), read as a big-endian uint32,
  modulo 100000
```

The result is an integer from 0 to 99999, read as an unsigned value: a sign-extended `int` gets every
hash with the top bit set wrong, which is why the pins include one. A flag uses its own key and salt; a
segment rule uses the segment's.

A rollout walks its entries in array order, adding each weight to a running total, and serves the first
entry where `bucket < total`. The weights sum to 100000 and the bucket is below that, so the walk always
ends.

**Worked example.** Flag `checkout`, salt `a1b2c3`, context key `user-123`:

```text
input     checkout.a1b2c3.user-123
SHA-256   1db8bb72 b00530d6...
uint32    0x1db8bb72 = 498645874
bucket    498645874 % 100000 = 45874
```

Against a rollout of 25000 / 25000 / 50000, the running totals are 25000, 50000 and 100000. 45874 is
below 50000 and not below 25000, so the second entry is served.

Edge cases:

- **The dots are not escaped.** `a.b` + `c` hashes the same as `a` + `b.c`. That only matters if
  someone picks keys and salts to collide, and then it only moves their own buckets, so the format stays
  trivial to implement.
- **A lone surrogate** in a string is encoded as U+FFFD, the default of both JavaScript's `TextEncoder`
  and .NET's `UTF8Encoding`. JSON that holds one is not well-formed and `System.Text.Json` rejects it,
  so the shared vectors leave it out and the TypeScript tests pin it.

### Why evaluation is synchronous

Web Crypto's `digest` is the only hash every JavaScript platform has, and it returns a promise. An async
`evaluate` would force every SDK call, at every flag check in a customer's code, to be awaited, and would
make a request's flag reads interleave. So the engine carries a small pure-JavaScript SHA-256
(`packages/engine/src/sha256.ts`) with no Node-only import, and `evaluate` stays synchronous. It hashes
at most a few short strings per call. Its output is tested against the FIPS 180-4 examples and against
`node:crypto` over every padding boundary; a C# implementation uses `SHA256.HashData`.

## The vectors

`fixtures/evaluation/*.json` holds no JavaScript and needs nothing from one. Every file but `buckets.json`
is:

```json
{
  "vectors": [
    {
      "name": "unique within the file",
      "flagKey": "f",
      "flags": [],
      "segments": [],
      "context": { "key": "user-1" },
      "expected": { "value": null, "variationIndex": null, "reason": { "kind": "ERROR", "code": "FLAG_NOT_FOUND" } }
    }
  ]
}
```

To run one: load `flags` and `segments` as a ruleset, evaluate `flagKey` for `context`, and compare
with `expected`, deeply.

| File | Covers |
| --- | --- |
| `clauses.json` | every attribute op, type mismatches, missing attributes, `negate`, array attributes |
| `reasons.json` | the order of evaluation, targets, rules, non-boolean variation values |
| `prerequisites.json` | met, failed, cycles, the depth limit, reuse within an evaluation |
| `segments.json` | `segmentMatch`, excluded and included, segment rules and weights, bad segments |
| `rollouts.json` | the cumulative walk at bucket boundaries, hash inputs, malformed rollouts |
| `errors.json` | invalid contexts, malformed flags, what happens on load |
| `buckets.json` | `{ flagKey, salt, contextKey, uint32, bucket }` pins from an independent SHA-256 |

Numbers that a loose reader would normalize are spelled literally, so read these files with a real JSON
parser: `1.0`, `-0.0`, `1e999`, `9007199254740993`. The expected values were written from this page, and the
bucket numbers come from Python's `hashlib`, never from the engine under test. The files are the source
of truth from here on: edit them directly.

A new implementation must pass every vector and every pin, unchanged. Changing an expected value changes
evaluation for every SDK and is a breaking change.
