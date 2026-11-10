import type {
  Client,
  ClientStatus,
  DetailReason,
  SdkErrorCode,
  VariationDetail,
} from "@hackcove/toggleslash-sdk";
import { ErrorCode, OpenFeature, StandardResolutionReasons } from "@openfeature/server-sdk";
import { afterEach, describe, expect, test } from "vitest";
import { ToggleSlashProvider } from "../src/index.ts";

interface Call {
  flagKey: string;
  context: { key: string; attributes?: Readonly<Record<string, unknown>> | null };
  fallback: unknown;
}

/** The canned answer is typed `unknown`; the fake hands it back as whatever the caller's fallback is. */
function isDetailFor<T>(
  _detail: VariationDetail<unknown>,
  _fallback: T,
): _detail is VariationDetail<T> {
  return true;
}

class FakeClient implements Client {
  calls: Call[] = [];
  readyCalls = 0;
  closeCalls = 0;
  readyResult = true;

  constructor(public answer: VariationDetail<unknown>) {}

  ready(): Promise<boolean> {
    this.readyCalls += 1;
    return Promise.resolve(this.readyResult);
  }

  variation<T>(_flagKey: string, _context: Call["context"], fallback: T): T {
    throw new Error(`the provider must use variationDetail, not variation (${String(fallback)})`);
  }

  variationDetail<T>(flagKey: string, context: Call["context"], fallback: T): VariationDetail<T> {
    this.calls.push({ flagKey, context, fallback });
    if (!isDetailFor(this.answer, fallback)) throw new Error("unreachable");
    return this.answer;
  }

  status(): ClientStatus {
    return { etag: null, lastSyncAt: null, stream: "connected", lastError: null };
  }

  close(): Promise<void> {
    this.closeCalls += 1;
    return Promise.resolve();
  }
}

const served = (value: unknown, variationIndex: number | null = 0): VariationDetail<unknown> => ({
  value,
  variationIndex,
  reason: { kind: "FALLTHROUGH" },
});

afterEach(async () => {
  await OpenFeature.close();
});

describe("resolution", () => {
  test("each method returns the client's value and variant and passes the key and default through", async () => {
    const cases = [
      {
        run: (p: ToggleSlashProvider) =>
          p.resolveBooleanEvaluation("f", false, { targetingKey: "u" }),
        value: true,
      },
      {
        run: (p: ToggleSlashProvider) => p.resolveStringEvaluation("f", "d", { targetingKey: "u" }),
        value: "blue",
      },
      {
        run: (p: ToggleSlashProvider) => p.resolveNumberEvaluation("f", 1, { targetingKey: "u" }),
        value: 42,
      },
      {
        run: (p: ToggleSlashProvider) =>
          p.resolveObjectEvaluation("f", { a: 1 }, { targetingKey: "u" }),
        value: { a: 2 },
      },
    ];
    for (const { run, value } of cases) {
      const client = new FakeClient(served(value, 3));
      const result = await run(new ToggleSlashProvider(client));
      expect(result.value).toEqual(value);
      expect(result.variant).toBe("3");
      expect(client.calls).toHaveLength(1);
      expect(client.calls[0]?.flagKey).toBe("f");
    }
    const client = new FakeClient(served("x"));
    await new ToggleSlashProvider(client).resolveStringEvaluation("flag-1", "the-default", {
      targetingKey: "u",
    });
    expect(client.calls[0]?.fallback).toBe("the-default");
  });

  test("a null variationIndex leaves variant out", async () => {
    const client = new FakeClient(served(false, null));
    const result = await new ToggleSlashProvider(client).resolveBooleanEvaluation("f", false, {
      targetingKey: "u",
    });
    expect("variant" in result).toBe(false);
  });
});

describe("context", () => {
  test("targetingKey becomes key, other entries become attributes, a Date becomes its ISO string", async () => {
    const client = new FakeClient(served(true));
    const when = new Date("2026-11-10T08:30:00.000Z");
    await new ToggleSlashProvider(client).resolveBooleanEvaluation("f", false, {
      targetingKey: "u1",
      plan: "pro",
      seats: 4,
      beta: true,
      signedUpAt: when,
      nested: { a: 1 },
    });
    expect(client.calls[0]?.context).toEqual({
      key: "u1",
      attributes: {
        plan: "pro",
        seats: 4,
        beta: true,
        signedUpAt: "2026-11-10T08:30:00.000Z",
        nested: { a: 1 },
      },
    });
  });

  test.each([
    ["missing", {}],
    ["empty", { targetingKey: "" }],
  ])(
    "a %s targetingKey answers TARGETING_KEY_MISSING without calling the client",
    async (_name, context) => {
      const client = new FakeClient(served(true));
      const result = await new ToggleSlashProvider(client).resolveBooleanEvaluation(
        "f",
        false,
        context,
      );
      expect(result).toEqual({
        value: false,
        reason: "ERROR",
        errorCode: ErrorCode.TARGETING_KEY_MISSING,
      });
      expect(client.calls).toHaveLength(0);
    },
  );
});

describe("mapping", () => {
  const reasons: [DetailReason, string][] = [
    [{ kind: "OFF" }, StandardResolutionReasons.DISABLED],
    [{ kind: "TARGET_MATCH" }, StandardResolutionReasons.TARGETING_MATCH],
    [{ kind: "RULE_MATCH", ruleIndex: 2 }, StandardResolutionReasons.TARGETING_MATCH],
    [{ kind: "FALLTHROUGH" }, StandardResolutionReasons.DEFAULT],
    [{ kind: "PREREQUISITE_FAILED", flagKey: "other" }, "PREREQUISITE_FAILED"],
    [{ kind: "ERROR", code: "FLAG_NOT_FOUND" }, StandardResolutionReasons.ERROR],
  ];

  test.each(reasons)("reason %j maps to %s", async (reason, expected) => {
    const client = new FakeClient({ value: true, variationIndex: 0, reason });
    const result = await new ToggleSlashProvider(client).resolveBooleanEvaluation("f", false, {
      targetingKey: "u",
    });
    expect(result.reason).toBe(expected);
    expect(result.errorCode === undefined).toBe(reason.kind !== "ERROR");
  });

  const codes: [SdkErrorCode, ErrorCode][] = [
    ["NOT_READY", ErrorCode.PROVIDER_NOT_READY],
    ["FLAG_NOT_FOUND", ErrorCode.FLAG_NOT_FOUND],
    ["TYPE_MISMATCH", ErrorCode.TYPE_MISMATCH],
    ["INVALID_CONTEXT", ErrorCode.INVALID_CONTEXT],
    ["MALFORMED_FLAG", ErrorCode.PARSE_ERROR],
    ["MALFORMED_SEGMENT", ErrorCode.PARSE_ERROR],
    ["SEGMENT_NOT_FOUND", ErrorCode.GENERAL],
    ["PREREQUISITE_CYCLE", ErrorCode.GENERAL],
    ["PREREQUISITE_DEPTH_EXCEEDED", ErrorCode.GENERAL],
    ["EVALUATION_FAILED", ErrorCode.GENERAL],
  ];

  test.each(codes)("error code %s maps to %s and is the errorMessage", async (code, expected) => {
    const client = new FakeClient({
      value: false,
      variationIndex: null,
      reason: { kind: "ERROR", code },
    });
    const result = await new ToggleSlashProvider(client).resolveBooleanEvaluation("f", false, {
      targetingKey: "u",
    });
    expect(result.errorCode).toBe(expected);
    expect(result.errorMessage).toBe(code);
    expect(result.value).toBe(false);
  });
});

describe("lifecycle", () => {
  test("initialize resolves when ready() resolves false", async () => {
    const client = new FakeClient(served(true));
    client.readyResult = false;
    await expect(new ToggleSlashProvider(client).initialize()).resolves.toBeUndefined();
    expect(client.readyCalls).toBe(1);
  });

  test("onClose closes the client", async () => {
    const client = new FakeClient(served(true));
    await new ToggleSlashProvider(client).onClose();
    expect(client.closeCalls).toBe(1);
  });
});

describe("through the OpenFeature API", () => {
  test("a targeting match carries its reason and variant", async () => {
    const client = new FakeClient({
      value: true,
      variationIndex: 0,
      reason: { kind: "TARGET_MATCH" },
    });
    await OpenFeature.setProviderAndWait(new ToggleSlashProvider(client));
    const details = await OpenFeature.getClient().getBooleanDetails("flag", false, {
      targetingKey: "u1",
    });
    expect(details.value).toBe(true);
    expect(details.reason).toBe(StandardResolutionReasons.TARGETING_MATCH);
    expect(details.variant).toBe("0");
    expect(client.readyCalls).toBe(1);
  });

  test("NOT_READY comes back as PROVIDER_NOT_READY with the default", async () => {
    const client = new FakeClient({
      value: false,
      variationIndex: null,
      reason: { kind: "ERROR", code: "NOT_READY" },
    });
    await OpenFeature.setProviderAndWait(new ToggleSlashProvider(client));
    const details = await OpenFeature.getClient().getBooleanDetails("flag", false, {
      targetingKey: "u1",
    });
    expect(details.value).toBe(false);
    expect(details.errorCode).toBe(ErrorCode.PROVIDER_NOT_READY);
    expect(details.reason).toBe(StandardResolutionReasons.ERROR);
  });
});
