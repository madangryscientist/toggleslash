import type { DetailReason, EvaluationEvent, Hooks } from "@hackcove/featureslash-sdk";
import { type Attributes, trace } from "@opentelemetry/api";

const EVENT_NAME = "feature_flag.evaluation";
const PROVIDER_NAME = "FeatureSlash";

export interface TracingOptions {
  /** The project key the SDK key belongs to: the `{project}` of the lookup route. */
  project: string;
  /** The environment key the SDK key belongs to: the `{env}` of the lookup route. */
  environment: string;
  /** Add `feature_flag.context.id`, the context's key (default false: it is an end user's identifier). */
  includeContextId?: boolean;
  /** Add `feature_flag.result.value`, the value the caller got (default false: it can be large or private). */
  includeValue?: boolean;
}

/**
 * The semantic conventions' well-known `feature_flag.result.reason` values where one applies. The two kinds
 * with no well-known equivalent keep their own lowercase name, which the conventions allow.
 */
const REASONS: Record<DetailReason["kind"], string> = {
  OFF: "disabled",
  TARGET_MATCH: "targeting_match",
  RULE_MATCH: "targeting_match",
  FALLTHROUGH: "fallthrough",
  PREREQUISITE_FAILED: "prerequisite_failed",
  ERROR: "error",
};

/** `error.type`, only for an error: the conventions' own values where one fits, else `general`. */
function errorType(reason: DetailReason): string | null {
  if (reason.kind !== "ERROR") return null;
  switch (reason.code) {
    case "FLAG_NOT_FOUND":
      return "flag_not_found";
    case "INVALID_CONTEXT":
      return "invalid_context";
    case "NOT_READY":
      return "provider_not_ready";
    case "TYPE_MISMATCH":
      return "type_mismatch";
    default:
      return "general";
  }
}

/** The ETag as the lookup route's `{hash}`: lowercase hex, no quotes (and no weak marker). */
function hashOf(etag: string): string {
  return etag.replace(/^W\//, "").replaceAll('"', "").toLowerCase();
}

/** An attribute value is a primitive; a JSON object or array, or null, becomes its JSON text. */
function attributeValue(value: unknown): string | number | boolean {
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return value;
  }
  return JSON.stringify(value) ?? "null";
}

function requireText(name: string, value: unknown): string {
  if (typeof value !== "string" || value === "") {
    throw new TypeError(`createTracingHooks: \`${name}\` must be a non-empty string`);
  }
  return value;
}

/**
 * The SDK hooks that put each evaluation on the active span. Pass the result as the client's `hooks`:
 * `createClient({ url, sdkKey, hooks: createTracingHooks({ project, environment }) })`.
 *
 * With no active span, or one that is not recording, a hook call does nothing. The application owns the
 * tracer provider and the exporter; this package starts neither. `project` and `environment` are checked
 * when the hooks are made, so a wrong call fails at start-up and never on a request.
 */
export function createTracingHooks(options: TracingOptions): Hooks {
  const project = requireText("project", options.project);
  const environment = requireText("environment", options.environment);
  const { includeContextId = false, includeValue = false } = options;

  return {
    afterEvaluation(event: EvaluationEvent): void {
      const span = trace.getActiveSpan();
      if (span === undefined || !span.isRecording()) return;

      const attributes: Attributes = {
        "feature_flag.key": event.flagKey,
        "feature_flag.provider.name": PROVIDER_NAME,
        "feature_flag.result.reason": REASONS[event.reason.kind],
        "featureslash.project": project,
        "featureslash.environment": environment,
      };
      if (event.variationIndex !== null) {
        attributes["feature_flag.result.variant"] = String(event.variationIndex);
      }
      if (event.flagVersion !== null)
        attributes["feature_flag.version"] = String(event.flagVersion);
      if (event.etag !== null) attributes["featureslash.ruleset.etag"] = hashOf(event.etag);
      const type = errorType(event.reason);
      if (type !== null) attributes["error.type"] = type;
      if (includeContextId && typeof event.context.key === "string") {
        attributes["feature_flag.context.id"] = event.context.key;
      }
      if (includeValue) attributes["feature_flag.result.value"] = attributeValue(event.value);

      span.addEvent(EVENT_NAME, attributes);
    },
  };
}
