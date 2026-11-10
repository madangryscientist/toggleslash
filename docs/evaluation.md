# Flag evaluation

This page is normative. It defines what a flag evaluates to, for every implementation:
`packages/engine` (TypeScript) and `apps/server/ToggleSlash.Evaluation` (C#). The shared vectors in
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

- **Target:** `{ "variation": <index>, "contextKeys": [<string>...], "contextKind": <kind> }`.
  `contextKind` is optional (see "Context kinds"); the target lists keys of that kind.
- **Prerequisite:** `{ "flagKey": <key>, "variation": <count> }`. The variation is an index into the
  *prerequisite's* variations, so it is not range-checked here; one that does not exist can never be
  met.
- **Serve:** exactly one of `"variation": <index>` or `"rollout": <rollout>`.
- **Rollout:** `{ "variations": [{ "variation": <index>, "weight": <count> }...], "contextKind": <kind> }`,
  with at least one entry in `variations`. `contextKind` is optional and picks the key the rollout
  buckets by (see "Bucketing"). Weights are thousandths of a percent and sum to exactly 100000. A weight of 0 is legal, and a
  variation may be listed twice.
- **Rule:** `{ "clauses": [<clause>...] }` plus a serve. All clauses must match. No clauses always
  matches.

### Clause

An attribute clause is `{ "attribute": <key>, "op": <op>, "values": [...], "negate": <boolean>, "contextKind": <kind> }`.
`attribute` is a name or a `/` reference; see "Reading the attribute". `contextKind` is optional and
names the context the attribute is read from; a value that is not a valid kind makes the flag
malformed (or, in a segment rule, the segment).
`op` is one of `in`, `contains`, `startsWith`, `endsWith`, `lt`, `lte`, `gt`, `gte`, `semverEq`,
`semverLt`, `semverLte`, `semverGt`, `semverGte`, `before`, `after`. `negate` is optional. Each value is a string, a finite number or a boolean; anything else is malformed.

A segment clause is `{ "op": "segmentMatch", "values": [<string>...], "negate": <boolean> }`. Its
values are segment keys. An `attribute` or a `contextKind` on it is ignored.

There is no regex op, by design: a regex is a ReDoS risk on a hot path, and JavaScript and .NET do not
agree on regex semantics. An unknown op makes the flag malformed.

### Segment

| Field | Type | Notes |
| --- | --- | --- |
| `key` | key | |
| `salt` | string | |
| `included` | array of string | user keys |
| `excluded` | array of string | user keys |
| `includedContexts` | array of context list, optional | keys of any kind |
| `excludedContexts` | array of context list, optional | keys of any kind |
| `rules` | array of segment rule | |

A context list is `{ "contextKind": <kind>, "values": [<string>...] }`. Here `contextKind` is required.

A segment rule is `{ "clauses": [<attribute clause>...], "weight": <count up to 100000>, "rolloutContextKind": <kind> }`.
`weight` and `rolloutContextKind` are optional; `rolloutContextKind` picks the key `weight` buckets by. A `segmentMatch` clause inside a segment rule makes the segment malformed, so segments do not
nest and cannot form a cycle.

### Context kinds

A **kind** is a string that matches `^[A-Za-z0-9._-]+$` (ASCII letters, digits, `.`, `_` and `-`, at
least one) and is neither `kind` nor `multi`. Everywhere a kind is read and is absent, it means
`"user"`. A kind is a free string: there is no list of kinds to declare.

In a flag or a segment, a `contextKind` on an attribute clause, a target or a rollout, or a
`rolloutContextKind`, that is present and is not a valid kind is malformed (a segment clause ignores
its `contextKind`), as is an `includedContexts` or `excludedContexts` that is not an array, or one whose
entry is not an object, has no valid `contextKind`, or has `values` that are not an array of strings.
They are checked at load with the other fields: the flag is `ERROR MALFORMED_FLAG`, and a segment
is malformed, so a flag that refers to it is `ERROR MALFORMED_SEGMENT`.

### Context

A context is one of two shapes.

**Single:**

```json
{ "kind": "user", "key": "user-1", "attributes": { "plan": "pro", "age": 30, "tags": ["a", "b"] } }
```

`kind` is optional, and absent is `"user"`, so `{ "key": "user-1" }` is a user context. `key` is a
required key. `attributes` is an optional object.

**Multi:**

```json
{
  "kind": "multi",
  "contexts": {
    "user": { "key": "user-1", "attributes": { "plan": "pro" } },
    "org": { "key": "acme", "attributes": { "seats": 40 } }
  }
}
```

`contexts` is an object with at least one property. Each property name is the entry's kind, and each
entry is `{ "key": <key>, "attributes": <object> }` with `attributes` optional. An entry has no `kind`
of its own.

**Valid.** A context is valid when it is an object and one of these holds:

- It has no `kind`, or a `kind` that is a valid kind; a `key` that is a non-empty string; and an
  `attributes` that is an object or absent.
- Its `kind` is `"multi"`; it has no `key` and no `attributes`; its `contexts` is an object with at
  least one property; every property name is a valid kind; and every entry is an object with a key
  that is a non-empty string, an `attributes` that is an object or absent, and no `kind`.

Anything else is not valid: a `kind` that is not a string, does not match the pattern, or is `kind`;
a multi context with a `key` or `attributes`, or whose `contexts` is not an object or is empty, or
names a kind that is not valid, or holds an entry that is not an object, has no valid key, has
`attributes` that are not an object, or has a `kind`. Null is absent here as everywhere, so
`"kind": null` is a user context. Other unknown properties are ignored, as elsewhere.

**Normalising.** Evaluation reads a valid context as a map from kind to `{ key, attributes }`: a single
context is one entry under its kind, and a multi context is its `contexts`. Everything below reads
that map. "The context has kind K" means the map has an entry for K, and "the key of kind K" is that
entry's key.

### Loading a ruleset

Flags and segments are validated once, when the ruleset is loaded, and loading never fails: a bad
entry becomes an `ERROR` when it is evaluated, and a `flags` or `segments` that is not an array counts
as empty, as does a ruleset that is not an object. If two flags or two segments share a key, the first one
in the array wins, even when it is the malformed one. An entry with no string `key` cannot be referred
to, and is dropped.

## Evaluation

`evaluate(ruleset, flagKey, context)` returns a result and **never throws**. This is the order, and the
first step that decides, decides:

1. **Context.** A context that is not valid (see "Context"): `ERROR INVALID_CONTEXT`. This comes
   first, before the flag is looked up.
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
7. **Targets.** The first target in array order that matches: `TARGET_MATCH`. A target matches when
   the context has the target's kind (`contextKind`, absent is `user`) and the key of that kind is in
   its `contextKeys`. So a user target never matches a context with no user kind.
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

- **Which context.** The attribute is read from the context of the clause's `contextKind` (absent is
  `user`), and everything below, `key` included, reads that context. If the context has no such kind,
  the attribute is missing, so the clause fails whatever `negate` says (see "Matching").
- **A name that does not start with `/` is a literal name, unchanged.** `a.b` still reads the attribute
  named `a.b`, and `a/b` reads the attribute named `a/b`. `key` reads the context key.
- **A name that starts with `/` is a reference.** Drop the leading `/` and split the rest on `/` into
  components. In each component, decode escapes in one left-to-right pass: `~1` is `/`, `~0` is `~`. So
  `/~1a` reads the attribute named `/a`, `/a~0b` reads `a~b`, and `/~01` reads `~1` (not `/`).
- **An invalid reference makes the flag malformed at load, like an unknown op.** A reference is invalid
  when any component is empty (`/`, `//`, `/a/`, `/a//b`) or when a `~` is followed by anything but `0`
  or `1`, or ends the component (`/a~`, `/a~2`). The flag is `ERROR MALFORMED_FLAG`; in a segment rule,
  the segment is malformed, as for any other bad clause there. A `~` in a literal name (no leading `/`)
  is an ordinary character.
- **Reading a reference.** The first component is read exactly as a literal name is: `key` reads the
  context key (so `/key` equals `key`), and anything else reads the context's own `attributes` property
  of that name. Each further component steps into the current value: the current value must be a JSON
  object (not an array, not null, not a string), and the component names one of its own properties.
  Anything else makes the attribute missing: a step into an array (`/tags/0` is missing even when `tags`
  is `["a"]`; there are no array indices), into a string (so `/key/x` is missing), a number, a boolean,
  null, or an object without that own property. Own properties only at every step, as today:
  `/a/toString` is missing unless `a` sets it, and `__proto__` is an ordinary name.
- **The value a reference reaches** follows the "Matching" rules unchanged: null is missing (fails
  whatever `negate` says), an array is one candidate per element, an object is present and never matches.
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
| `semverEq`, `semverLt`, `semverLte`, `semverGt`, `semverGte` | both strings, both valid versions (below), and `a` equals or orders that way against `v` |
| `before`, `after` | `a` and `v` are both instants (below), and `a` is strictly earlier (`before`) or strictly later (`after`) than `v` |

- **Number equality and order are IEEE-754.** `1` equals `1.0`, and `-0` equals `0`.
- **String equality and the three substring ops are ordinal:** the same UTF-16 code units, case-sensitive,
  with no Unicode normalization. `é` is not `e` plus U+0301. A soft hyphen is not ignorable. An empty
  value matches any string, and no number. In .NET pass `StringComparison.Ordinal` every time: the
  default `StartsWith(string)` and `EndsWith(string)` are culture-sensitive, and that is the difference
  these vectors catch.
- **String order is by UTF-16 code unit,** `string.CompareOrdinal` in C# and `<` in JavaScript, never a
  culture's collation. So `"B" < "a"`, `"10" < "9"`, and `"😀"` (0xD83D first) sorts before `"～"`
  (0xFF5E), which code point order would reverse. ISO-8601 dates in one format order correctly as
  strings, but only while they share one format and offset, so a date comparison is expressed with
  `before` and `after` (below).
- **Booleans have no order.** `lt` and friends never match them.

### Semantic versions

The five `semver` ops compare versions, because `lt` on strings puts `"10.0.0"` before `"9.0.0"`.

- **Valid forms.** Full SemVer 2.0.0: `MAJOR.MINOR.PATCH`, then an optional `-prerelease`, then an
  optional `+build`. Also two short forms with no suffix: `MAJOR` and `MAJOR.MINOR`, where a missing part
  reads as 0, so `2.1` equals `2.1.0`. Anything else is not a version: `v1.2.3`, ` 1.2.3` (nothing is
  trimmed), `1.2-beta`, `1.2.3.4` and the empty string.
- **Numeric parts.** ASCII `0` to `9` only, either `0` or with no leading zero (`01.2.3` is not a
  version), of any length. A non-ASCII digit, such as U+0661, is not a digit.
- **Prerelease and build.** Dot-separated identifiers of ASCII letters, digits and `-`, none empty. A
  prerelease identifier made only of digits has no leading zero (`1.0.0-01` is not a version, `1.0.0-0a`
  is). A build identifier may have leading zeros.
- **Order** is SemVer 2.0.0 section 11. Major, minor and patch compare numerically. A version with a
  prerelease is below the same version without one. Prerelease identifiers compare left to right:
  digits-only identifiers numerically, others by ASCII code unit, a digits-only identifier below an
  alphanumeric one, and a longer list wins when all the earlier identifiers are equal. So
  `1.0.0-alpha < 1.0.0-alpha.1 < 1.0.0-alpha.beta < 1.0.0-beta < 1.0.0-beta.2 < 1.0.0-beta.11 < 1.0.0-rc.1 < 1.0.0`.
  Build is ignored for equality and order: `1.0.0+a` `semverEq` `1.0.0+b` is true.
- **A number is never converted.** Digit strings compare by length, then by code unit, so
  `1.0.99999999999999999999` is above `1.0.99999999999999999998` in every language.
- **No match, no error.** The candidate and the value must both be strings and valid versions, otherwise
  that pair does not match. So the rules under "Matching" hold: under `negate` a present attribute that is
  not a version matches, a missing attribute fails whatever `negate` says, and an array attribute is one
  candidate per element. A semver clause with an invalid version in `values` does not make a flag
  malformed.
- **Writing is stricter than reading.** The server's targeting API refuses a semver clause whose `values`
  hold anything but a string spelling a valid version, because a typo there would silently never match.
  Empty `values` saves, as for every op, and never matches.

Implementations read the version with a hand-written walk over the characters, not a regular expression:
`\d` matches Unicode digits in .NET, and a library would bring in its own idea of what is valid.

### Dates

`before` and `after` compare points in time, because `lt` on ISO-8601 strings breaks as soon as two
dates carry different offsets or formats. Evaluation never reads a clock, so there is no "now" and no
`on` op: a rule compares the context's date to a date written in the rule.

- **An instant** is a finite number, read as Unix milliseconds (fractional and negative values included,
  compared as binary64), or a string in exactly this form: `YYYY-MM-DDTHH:MM:SS`, then optionally `.` and
  one or more digits, then `Z`, `+HH:MM` or `-HH:MM`.
- **Strict form.** Every digit is ASCII `0` to `9` (a non-ASCII digit, such as U+0661, is not a digit).
  `T` and `Z` are uppercase only. There is no space separator, no missing seconds, no missing offset and
  nothing is trimmed. `2026-01-01`, `2026-01-01T00:00:00` and `2026-01-01 00:00:00Z` are not instants.
- **Ranges.** Year `0001` to `9999` (`0000` is not a date: it keeps every day count non-negative, so
  integer division agrees across languages). Month `01` to `12`. Day `01` to the length of that month,
  with February 29 only in a leap year: divisible by 4, except centuries not divisible by 400, so
  `2000-02-29` is valid and `1900-02-29` and `2023-02-29` are not. Hour `00` to `23`, minute `00` to
  `59`, second `00` to `59` (there is no leap second `60`). Offset hour `00` to `23`, offset minute
  `00` to `59`. `-00:00` equals `Z`.
- **Fraction.** Truncated to milliseconds: take its first three digits, padding with zeros on the right.
  `.5` is 500 ms and `.1239` is 123 ms, so `.1239` equals `.123`.
- **The value.** `days * 86400000 + ((hour * 60 + minute) * 60 + second) * 1000 + fractionMs -
  (offsetSign * (offsetHour * 60 + offsetMinute)) * 60000`, where `offsetSign` is +1 for `+` and -1 for
  `-`, and the whole offset term is 0 for `Z`. `days` is the number of days since 1970-01-01 in the
  proleptic Gregorian calendar, by the "days from civil" algorithm in integer arithmetic. For years 0001
  to 9999 every intermediate value before its last subtraction is non-negative, so truncating division
  (C#) and `Math.floor` (TypeScript) agree. Every result fits a binary64 exactly, from
  `-62135596800000` (`0001-01-01T00:00:00Z`) to `253402300799999` (`9999-12-31T23:59:59.999Z`).
- **Strings and numbers mix.** Both sides become binary64 numbers first, so a string candidate against a
  number value, a number against a string, and two strings are all one comparison of two instants.
  `before` is `a < v` and `after` is `a > v`: **equal instants match neither**, so
  `2026-01-01T02:00:00+02:00`, `2026-01-01T00:00:00Z` and `1767225600000` are all the same instant.
- **No match, no error.** A candidate or value that is not a finite number and not a valid date string
  (a boolean, an object, a candidate `1e999`, `"2026-01-01"`, `"2026-01-01T00:00:00"`) makes that pair
  not match. So the rules under "Matching" hold: a missing attribute fails whatever `negate` says,
  `negate` flips a present non-date to a match, and an array attribute is one candidate per element. A
  string or boolean in `values` that is not an instant does not make the flag malformed. A non-finite
  number in `values` (`1e999`) is still malformed at load, as for every op.
- **Writing is stricter than reading.** The server's targeting API refuses a `before` or `after` clause
  whose `values` hold anything but a finite number or a valid date string, because a typo there would
  silently never match. Empty `values` saves, as for every op, and never matches (under `negate`, always
  matches).

Implementations read the string with a hand-written walk over the characters: no regular expression, no
`Date.parse`, `DateTime.Parse` or `DateTimeOffset.Parse`, and no date library, because each brings its own
idea of what is valid and which calendar and culture apply.

### segmentMatch

True when the context is in any listed segment. `negate` flips it, and a missing attribute cannot arise.
No values means false, and under `negate` true.

**In a segment,** in this order:

1. Excluded: the key of kind `user` is in `excluded`, or for any entry of `excludedContexts` the
   context has that entry's `contextKind` and the key of that kind is in its `values`. Not in the
   segment.
2. Included: the same test with `included` and `includedContexts`. In the segment.
3. Any rule whose clauses all match (each reading its own `contextKind`), and whose `weight` is absent
   or above the rule's bucket: in the segment. The bucket is computed as for a rollout (see
   "Bucketing"), with the segment's key and salt and the kind `rolloutContextKind` (absent is `user`).
4. Otherwise not in the segment.

So exclusion wins over inclusion across kinds: an org key in `excludedContexts` keeps the context out
even when its user key is in `included`.

## Bucketing

```text
bucket(key, salt, contextKey) =
  first 4 bytes of SHA-256(UTF-8 bytes of "<key>.<salt>.<contextKey>"), read as a big-endian uint32,
  modulo 100000
```

The result is an integer from 0 to 99999, read as an unsigned value: a sign-extended `int` gets every
hash with the top bit set wrong, which is why the pins include one. A flag uses its own key and salt; a
segment rule uses the segment's.

**The context key and its kind.** A rollout buckets by the kind in its `contextKind`, and a segment
rule by the kind in its `rolloutContextKind`; absent is `user`. The `contextKey` passed to `bucket` is:

- the key of that kind as it is, when the kind is `user`, so every user bucket is what it was before
  kinds existed;
- `<kind>:<key>` for any other kind, so the same key in two kinds lands in independent buckets.

If the context does not have that kind, the bucket is 0, and no hash is computed. So a rollout serves
its first entry with a non-zero weight, and a segment rule with a `weight` above 0 matches.

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

**Worked example for another kind.** Flag `checkout`, salt `a1b2c3`, a rollout with `contextKind`
`org`, and an org key `acme`:

```text
input     checkout.a1b2c3.org:acme
SHA-256   9dca54cd ...
uint32    0x9dca54cd = 2647282893
bucket    2647282893 % 100000 = 82893
```

Against the same 25000 / 25000 / 50000 rollout, 82893 is not below 50000, so the third entry is
served.

Edge cases:

- **The dots are not escaped.** `a.b` + `c` hashes the same as `a` + `b.c`. That only matters if
  someone picks keys and salts to collide, and then it only moves their own buckets, so the format stays
  trivial to implement.
- **A lone surrogate** in a string (JSON `"\ud800"`) is valid to a JSON parser. It is kept as it is
  in context keys, attribute values and clause values, and so compares equal only to the same lone
  surrogate, never to U+FFFD. Only hashing replaces it: the bytes hashed are the UTF-8 encoding, in
  which it becomes U+FFFD, the default of both JavaScript's `TextEncoder` and .NET's `UTF8Encoding`.
  An implementation whose string accessor throws on one (`JsonElement.GetString()` does) must read
  the string another way, since evaluation never throws.

### Why evaluation is synchronous

Web Crypto's `digest` is the only hash every JavaScript platform has, and it returns a promise. An async
`evaluate` would force every SDK call, at every flag check in a customer's code, to be awaited, and would
make a request's flag reads interleave. So the engine carries a small pure-JavaScript SHA-256
(`packages/engine/src/sha256.ts`) with no Node-only import, and `evaluate` stays synchronous. It hashes
at most a few short strings per call. Its output is tested against the FIPS 180-4 examples and against
`node:crypto` over every padding boundary; the C# implementation uses `SHA256.HashData`.

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
| `semver.json` | the five `semver` ops: the section 11 order, short forms, build, big numbers, what is not a version, `negate`, arrays |
| `dates.json` | `before` and `after`: equal instants, offsets, fractions, the calendar and leap years, the range ends, numbers as Unix milliseconds, what is not an instant, `negate`, arrays |
| `attributes.json` | `/` references: nesting, escapes, `key`, arrays and non-objects on the way, own properties, invalid references |
| `segments.json` | `segmentMatch`, excluded and included, segment rules and weights, bad segments |
| `rollouts.json` | the cumulative walk at bucket boundaries, hash inputs, malformed rollouts |
| `errors.json` | invalid contexts, malformed flags, what happens on load |
| `kinds.json` | context kinds and multi-contexts: valid and invalid contexts, `contextKind` on clauses, targets and rollouts, `includedContexts` and `excludedContexts`, `rolloutContextKind`, malformed kind fields |
| `buckets.json` | `{ flagKey, salt, contextKey, uint32, bucket }` pins from an independent SHA-256 |

Numbers that a loose reader would normalize are spelled literally, so read these files with a real JSON
parser: `1.0`, `-0.0`, `1e999`, `9007199254740993`. The expected values were written from this page, and the
bucket numbers come from Python's `hashlib`, never from the engine under test. The files are the source
of truth from here on: edit them directly.

A new implementation must pass every vector and every pin, unchanged. Changing an expected value changes
evaluation for every SDK and is a breaking change.
