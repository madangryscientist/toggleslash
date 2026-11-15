import { createPublicKey, generateKeyPairSync, verify } from "node:crypto";
import { describe, expect, it } from "vitest";
import { signContext } from "../src/index.ts";
import { contextMessage } from "../src/sign.ts";

// The two vectors of docs/sdk-browser.md, "Secure mode".
const single = { key: "user-1" };
const multi = {
  kind: "multi" as const,
  contexts: { user: { key: "u", attributes: { plan: "pro" } }, org: { key: "acme" } },
};

function pair(namedCurve = "prime256v1") {
  const { privateKey } = generateKeyPairSync("ec", { namedCurve });
  return {
    pem: privateKey.export({ type: "pkcs8", format: "pem" }),
    publicKey: createPublicKey(privateKey),
  };
}

describe("contextMessage", () => {
  it("writes the two vectors' bytes", () => {
    expect(contextMessage(single).toString("utf8")).toBe("4:user,6:user-1,");
    expect(contextMessage(multi).toString("utf8")).toBe("3:org,4:acme,4:user,1:u,");
  });

  it("counts bytes, not characters, and leaves attributes out", () => {
    expect(contextMessage({ key: "é", attributes: { a: 1 } }).toString("utf8")).toBe("4:user,2:é,");
    expect(contextMessage({ kind: "org", key: "acme" }).toString("utf8")).toBe("3:org,4:acme,");
  });
});

describe("signContext", () => {
  it("makes a 64-byte P1363 signature, base64url with no padding, that the public key verifies", () => {
    const { pem, publicKey } = pair();
    for (const context of [single, multi]) {
      const signature = signContext(pem, context);
      expect(signature).toMatch(/^[A-Za-z0-9_-]{86}$/);
      const bytes = Buffer.from(signature, "base64url");
      expect(bytes).toHaveLength(64);
      expect(
        verify(
          "sha256",
          contextMessage(context),
          { key: publicKey, dsaEncoding: "ieee-p1363" },
          bytes,
        ),
      ).toBe(true);
    }
  });

  it("throws on a context that is not valid", () => {
    const { pem } = pair();
    expect(() => signContext(pem, { key: "" })).toThrow(TypeError);
    expect(() => signContext(pem, { kind: "multi", contexts: {} })).toThrow(TypeError);
  });

  it("throws on a key that is not an ECDSA P-256 private key", () => {
    expect(() => signContext("not a key", single)).toThrow(Error);
    expect(() => signContext(pair("secp384r1").pem, single)).toThrow(TypeError);
  });
});
