/**
 * A hook for the FeatureSlash TypeScript SDK that records each evaluation as a `feature_flag.evaluation`
 * event on the active OpenTelemetry span, with the ruleset hash that answers on the lookup route.
 * See `docs/tracing.md`.
 */
export { createTracingHooks, type TracingOptions } from "./hooks.ts";
