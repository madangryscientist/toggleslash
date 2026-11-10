import type { Client, SdkErrorCode, VariationDetail } from "@hackcove/toggleslash-sdk";
import {
  ErrorCode,
  type EvaluationContext,
  type JsonValue,
  type Provider,
  type ResolutionDetails,
  StandardResolutionReasons,
} from "@openfeature/server-sdk";

type SdkReasonKind = VariationDetail<unknown>["reason"]["kind"];

const REASONS: Record<SdkReasonKind, string> = {
  OFF: StandardResolutionReasons.DISABLED,
  TARGET_MATCH: StandardResolutionReasons.TARGETING_MATCH,
  RULE_MATCH: StandardResolutionReasons.TARGETING_MATCH,
  FALLTHROUGH: StandardResolutionReasons.DEFAULT,
  PREREQUISITE_FAILED: "PREREQUISITE_FAILED",
  ERROR: StandardResolutionReasons.ERROR,
};

const ERROR_CODES: Record<SdkErrorCode, ErrorCode> = {
  NOT_READY: ErrorCode.PROVIDER_NOT_READY,
  FLAG_NOT_FOUND: ErrorCode.FLAG_NOT_FOUND,
  TYPE_MISMATCH: ErrorCode.TYPE_MISMATCH,
  INVALID_CONTEXT: ErrorCode.INVALID_CONTEXT,
  MALFORMED_FLAG: ErrorCode.PARSE_ERROR,
  MALFORMED_SEGMENT: ErrorCode.PARSE_ERROR,
  SEGMENT_NOT_FOUND: ErrorCode.GENERAL,
  PREREQUISITE_CYCLE: ErrorCode.GENERAL,
  PREREQUISITE_DEPTH_EXCEEDED: ErrorCode.GENERAL,
  EVALUATION_FAILED: ErrorCode.GENERAL,
};

/** The SDK context for an OpenFeature one: `targetingKey` is the key, the rest are attributes. */
function toSdkContext(
  context: EvaluationContext,
): { key: string; attributes: Record<string, unknown> } | null {
  const { targetingKey, ...rest } = context;
  if (typeof targetingKey !== "string" || targetingKey === "") return null;
  const attributes: Record<string, unknown> = {};
  for (const [name, value] of Object.entries(rest)) {
    attributes[name] = value instanceof Date ? value.toISOString() : value;
  }
  return { key: targetingKey, attributes };
}

/** A server-side OpenFeature provider over the SDK's client. It evaluates nothing itself. */
export class ToggleSlashProvider implements Provider {
  readonly metadata = { name: "ToggleSlash" } as const;
  readonly runsOn = "server";

  constructor(private readonly client: Client) {}

  async initialize(): Promise<void> {
    await this.client.ready();
  }

  async onClose(): Promise<void> {
    await this.client.close();
  }

  async resolveBooleanEvaluation(
    flagKey: string,
    defaultValue: boolean,
    context: EvaluationContext,
  ) {
    return this.resolve(flagKey, defaultValue, context);
  }

  async resolveStringEvaluation(flagKey: string, defaultValue: string, context: EvaluationContext) {
    return this.resolve(flagKey, defaultValue, context);
  }

  async resolveNumberEvaluation(flagKey: string, defaultValue: number, context: EvaluationContext) {
    return this.resolve(flagKey, defaultValue, context);
  }

  async resolveObjectEvaluation<T extends JsonValue>(
    flagKey: string,
    defaultValue: T,
    context: EvaluationContext,
  ) {
    return this.resolve(flagKey, defaultValue, context);
  }

  private resolve<T>(
    flagKey: string,
    defaultValue: T,
    context: EvaluationContext,
  ): ResolutionDetails<T> {
    const sdkContext = toSdkContext(context);
    if (sdkContext === null) {
      return {
        value: defaultValue,
        reason: StandardResolutionReasons.ERROR,
        errorCode: ErrorCode.TARGETING_KEY_MISSING,
      };
    }
    const detail = this.client.variationDetail(flagKey, sdkContext, defaultValue);
    const resolution: ResolutionDetails<T> = {
      value: detail.value,
      reason: REASONS[detail.reason.kind],
    };
    if (detail.variationIndex !== null) resolution.variant = String(detail.variationIndex);
    if (detail.reason.kind === "ERROR") {
      resolution.errorCode = ERROR_CODES[detail.reason.code];
      resolution.errorMessage = detail.reason.code;
    }
    return resolution;
  }
}
