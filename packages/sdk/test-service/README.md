# The TypeScript SDK's test service

`main.ts` wraps `createClient` in the protocol of [`docs/sdk-test-service.md`](../../../docs/sdk-test-service.md),
so the conformance harness can drive the SDK from outside:

```bash
bun run conformance -- --service "bun packages/sdk/test-service/main.ts"
```

It advertises no capability: TypeScript has one number type, so there is no integer accessor.

| Protocol | TypeScript SDK |
| --- | --- |
| `POST /clients` | `createClient(options)`; `pollIntervalMs`, `flushIntervalMs`, `requestTimeoutMs` and `stream` pass through, `onError` always records |
| `ready` | `client.ready()` |
| `evaluate`, every accessor (`bool`, `string`, `number`, `json`, `any`) | `client.variationDetail(flagKey, context, fallback)`: the SDK has one generic method, and the fallback's kind does the work |
| `status` | `client.status()` |
| `hook-events` | the events `hooks.afterEvaluation` received, as the SDK built them |
| `errors` | what `onError` received, each as its message |
| `close` | `client.close()` |

It catches nothing the SDK throws: a call that raises answers 500 with the error text.
