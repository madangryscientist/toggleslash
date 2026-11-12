# Java test service

The conformance harness drives the Java SDK through this small HTTP service
(`docs/sdk-test-service.md` is the protocol). It lives in `test-service/src`, which `pom.xml` adds as a
test source root, and it is built with the test classes. `bin/test-service` runs it from `target/` and
prints `listening http://127.0.0.1:<port>`.

It advertises one capability, `integer-accessor`.

## Accessors

`POST /clients/{id}/evaluate` takes an `accessor` name. Each name calls one SDK method with the
request's flag key, context and fallback, and the `...Detail` form answers `value`,
`variationIndex` and `reason`.

| accessor | SDK method | fallback must be |
| --- | --- | --- |
| `bool` | `Client.boolVariationDetail` | a boolean |
| `string` | `Client.stringVariationDetail` | a string |
| `number` | `Client.doubleVariationDetail` | a number |
| `int` | `Client.longVariationDetail` | a number (read as a whole number) |
| `json` | `Client.jsonVariationDetail` | anything; `null` accepts any served kind |
| `any` | `Client.jsonVariationDetail` | anything; `null` accepts any served kind |

A fallback of the wrong kind for a typed accessor answers 400, because the Java signature cannot take
it. A served number that is not finite is written back as JSON `null`, as the protocol says.

## Routes

| route | SDK call |
| --- | --- |
| `GET /` | none: the name and capabilities |
| `POST /clients` | `Client.create(Options)` |
| `POST /clients/{id}/ready` | `Client.ready()` |
| `POST /clients/{id}/evaluate` | an accessor, above |
| `POST /clients/{id}/status` | `Client.status()` |
| `GET /clients/{id}/hook-events` | the events the `afterEvaluation` hook recorded |
| `GET /clients/{id}/errors` | the messages `onError` recorded |
| `POST /clients/{id}/close` | `Client.close()` |
