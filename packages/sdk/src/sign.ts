import { createPrivateKey, sign } from "node:crypto";
import { type Context, normaliseContext } from "@hackcove/toggleslash-engine";

/** `<byte length of the UTF-8>:<UTF-8 bytes>,` */
function netstring(text: string): Buffer {
  const bytes = Buffer.from(text, "utf8");
  return Buffer.concat([
    Buffer.from(`${bytes.length}:`, "ascii"),
    bytes,
    Buffer.from(",", "ascii"),
  ]);
}

/**
 * The bytes a secure-mode signature covers (`docs/sdk-browser.md`, "Secure mode"): the context normalised as
 * `docs/evaluation.md` does it, keys only, the kinds in byte order, each kind and then its key as a netstring.
 * Attributes are not part of it. Throws when the context is not valid.
 */
export function contextMessage(context: Context): Uint8Array {
  const contexts = normaliseContext(context);
  if (contexts === null)
    throw new TypeError("signContext: the context is not valid; see docs/evaluation.md");
  // A kind is ASCII, so code-unit order is byte order, and no two entries share a kind.
  const entries = [...contexts].toSorted(([a], [b]) => (a < b ? -1 : 1));
  return Buffer.concat(entries.flatMap(([kind, { key }]) => [netstring(kind), netstring(key)]));
}

/**
 * Signs a context for secure mode with an environment's signing key (the PKCS#8 PEM that
 * `POST .../environments/{env}/signing-key` answered once). The result goes to the browser with the context, and the
 * browser SDK sends it with every evaluate for that context. Node only: it runs on the application's server, which
 * holds the key; never in a page.
 *
 * Unlike the rest of the SDK this throws, on a context that is not valid or a key that is not an ECDSA P-256 private
 * key: both are mistakes at the call site, not conditions at run time.
 */
export function signContext(privateKeyPem: string, context: Context): string {
  const message = contextMessage(context);
  const key = createPrivateKey(privateKeyPem);
  if (key.asymmetricKeyType !== "ec" || key.asymmetricKeyDetails?.namedCurve !== "prime256v1")
    throw new TypeError("signContext: the key is not an ECDSA P-256 private key");
  // The PEM, not the KeyObject: Cloudflare Workers' `node:crypto` refuses a KeyObject as `options.key`.
  return sign("sha256", message, { key: privateKeyPem, dsaEncoding: "ieee-p1363" }).toString(
    "base64url",
  );
}
