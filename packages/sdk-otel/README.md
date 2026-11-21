# @slashsuite/toggleslash-sdk-otel

An evaluation hook for the ToggleSlash TypeScript SDK. Each evaluation adds a `feature_flag.evaluation` event to the
active OpenTelemetry span, with the ruleset hash (`toggleslash.ruleset.etag`) that the server's lookup route can
turn back into the flags. Published on npm by `release.yml`; it depends on `@opentelemetry/api` only, and the application owns
the tracer provider and the exporter.

```sh
npm install @slashsuite/toggleslash-sdk-otel @opentelemetry/api
```

```ts
createClient({ url, sdkKey, hooks: createTracingHooks({ project: "shop", environment: "production" }) });
```

`includeContextId` and `includeValue` are off by default. The attributes, how to go from a trace to the flags, and
what was and was not checked are in [`docs/tracing.md`](https://github.com/slashsuite/toggleslash/blob/main/docs/tracing.md).
