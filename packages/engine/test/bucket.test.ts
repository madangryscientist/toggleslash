import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { bucket } from "../src/bucket.ts";
import { createRuleset, evaluate } from "../src/index.ts";
import { sha256 } from "../src/sha256.ts";

interface Pin {
  flagKey: string;
  salt: string;
  contextKey: string;
  uint32: number;
  bucket: number;
}

const hex = (bytes: Uint8Array) => Buffer.from(bytes).toString("hex");
const utf8 = (text: string) => new TextEncoder().encode(text);

function isPinFile(value: unknown): value is { buckets: Pin[] } {
  return typeof value === "object" && value !== null && "buckets" in value;
}

function loadPins(): Pin[] {
  const file = join(import.meta.dirname, "../../../fixtures/evaluation/buckets.json");
  const parsed: unknown = JSON.parse(readFileSync(file, "utf8"));
  if (!isPinFile(parsed)) throw new Error("buckets.json has no buckets");
  return parsed.buckets;
}

describe("sha256", () => {
  it("matches the FIPS 180-4 examples", () => {
    expect(hex(sha256(utf8("")))).toBe(
      "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    );
    expect(hex(sha256(utf8("abc")))).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
    expect(hex(sha256(utf8("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq")))).toBe(
      "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1",
    );
  });

  it("agrees with the platform across every padding boundary", () => {
    for (let length = 0; length <= 300; length++) {
      const message = Uint8Array.from({ length }, (_, i) => (i * 31 + length) % 256);
      expect(hex(sha256(message))).toBe(createHash("sha256").update(message).digest("hex"));
    }
  });
});

describe("bucket", () => {
  const buckets = loadPins();

  it.each(buckets)(
    "pins $flagKey / $salt / $contextKey",
    ({ flagKey, salt, contextKey, uint32, bucket: expected }) => {
      expect(uint32 % 100_000).toBe(expected);
      expect(bucket(flagKey, salt, contextKey)).toBe(expected);
    },
  );

  it("encodes a lone surrogate as U+FFFD, as .NET's UTF-8 encoder does", () => {
    expect(bucket("f", "s", "\ud800")).toBe(bucket("f", "s", "�"));
  });
});

describe("rollout distribution", () => {
  it("serves a 25/25/50 rollout in its shares over a fixed key set, within half a point", () => {
    const flag = {
      key: "distribution",
      variations: ["a", "b", "c"],
      on: true,
      targets: [],
      prerequisites: [],
      rules: [],
      fallthrough: {
        rollout: {
          variations: [
            { variation: 0, weight: 25_000 },
            { variation: 1, weight: 25_000 },
            { variation: 2, weight: 50_000 },
          ],
        },
      },
      salt: "fixed-salt",
      version: 1,
    };
    const ruleset = createRuleset({ flags: [flag], segments: [] });
    const KEYS = 100_000;
    const counts = [0, 0, 0];
    for (let i = 0; i < KEYS; i++) {
      const { variationIndex } = evaluate(ruleset, "distribution", { key: `user-${i}` });
      counts[variationIndex ?? 0]! += 1;
    }
    expect(Math.abs(counts[0]! / KEYS - 0.25)).toBeLessThan(0.005);
    expect(Math.abs(counts[1]! / KEYS - 0.25)).toBeLessThan(0.005);
    expect(Math.abs(counts[2]! / KEYS - 0.5)).toBeLessThan(0.005);
  });
});
