# Java SDK test service

The conformance test service for the Java SDK, to the protocol in `docs/sdk-test-service.md`. It wraps
`Client` in an HTTP server on `127.0.0.1` (a random port, printed as `listening http://127.0.0.1:<port>`) and
holds any number of clients. Run it with `bin/test-service`; the harness starts it itself:

```bash
bun run conformance -- --service sdks/java/bin/test-service
```

It advertises one capability, `integer-accessor`: Java has `long` and `Client.intVariation`.

## Accessors

| Accessor | `Client` method | Fallback it needs |
| --- | --- | --- |
| `bool` | `boolVariationDetail` | a boolean |
| `string` | `stringVariationDetail` | a string |
| `number` | `numberVariationDetail` | a number, as `double` |
| `int` | `intVariationDetail` | a number, as `long` |
| `json` | `variationDetail` | anything, `null` included |
| `any` | `variationDetail` | anything, `null` included |

A typed accessor given a fallback of another kind answers 400 (the protocol allows it). Every answer is
`variationDetail`'s three fields: `value`, `variationIndex` and `reason`, where `reason` is
`{ "kind": ..., "ruleIndex"?, "flagKey"?, "code"? }`.

## Options and switches

`url`, `sdkKey`, `stream`, `pollIntervalMs`, `flushIntervalMs` and `requestTimeoutMs` go to `Options.Builder`.
`hook` and `hookThrows` install an `afterEvaluation` hook that records its event (and throws, for
`hookThrows`). `onError` always records the message, and throws after recording when `onErrorThrows` is set.

The context is passed through unvalidated: an invalid one is a case the SDK must answer
(`ERROR INVALID_CONTEXT`). A call that throws answers 500 with the stack trace.
