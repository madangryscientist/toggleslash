/**
 * Secure-mode signing (contract 14) through the service's `POST /sign`, for a service that lists `sign-context`.
 * ECDSA signatures are randomised, so `fixtures/signing/messages.json` holds the bytes each context must sign, not a
 * signature: the harness makes a P-256 key pair for the run (never written anywhere), asks the service to sign each
 * context and verifies the answer over the fixture's bytes with the public half. One more case sends a P-384 key,
 * which the SDK must refuse.
 */
import { generateKeyPairSync, type KeyObject, verify } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { isRecord, list, record, show, str } from "./json.ts";
import { REPO_ROOT, type Service } from "./service.ts";

export const SIGN_CONTEXT = "sign-context";

const FILE = join(REPO_ROOT, "fixtures/signing/messages.json");
/** 64 bytes of P1363 `r || s`, base64url with no padding. */
const SIGNATURE = /^[A-Za-z0-9_-]{86}$/;

interface Message {
  name: string;
  context: unknown;
  message: string;
}

export interface SigningOutcome {
  passed: number;
  skipped: number;
  failures: string[];
}

function loadMessages(): Message[] {
  return list(record(JSON.parse(readFileSync(FILE, "utf8")), FILE)["messages"], FILE).map((raw) => {
    const entry = record(raw, "a message in fixtures/signing");
    return { name: str(entry, "name"), context: entry["context"], message: str(entry, "message") };
  });
}

function keyPair(namedCurve: string): { privateKeyPem: string; publicKey: KeyObject } {
  const { privateKey, publicKey } = generateKeyPairSync("ec", { namedCurve });
  const privateKeyPem = privateKey.export({ type: "pkcs8", format: "pem" });
  return { privateKeyPem, publicKey };
}

async function signsMessage(
  service: Service,
  privateKeyPem: string,
  publicKey: KeyObject,
  vector: Message,
): Promise<string | null> {
  const answer = await service.call("POST", "/sign", { privateKeyPem, context: vector.context });
  const signature = answer["signature"];
  if (typeof signature !== "string" || !SIGNATURE.test(signature))
    return `the signature is not 86 base64url characters: ${show(signature)}`;
  const verified = verify(
    "sha256",
    Buffer.from(vector.message, "utf8"),
    { key: publicKey, dsaEncoding: "ieee-p1363" },
    Buffer.from(signature, "base64url"),
  );
  return verified ? null : `the signature does not verify over ${show(vector.message)}`;
}

async function refusesP384(service: Service): Promise<string | null> {
  const { privateKeyPem } = keyPair("secp384r1");
  const { status, body } = await service.callWithStatus("POST", "/sign", {
    privateKeyPem,
    context: { key: "user-1" },
  });
  if (status !== 400) return `answered ${status}, not 400: ${show(body)}`;
  const parsed: unknown = JSON.parse(body);
  const error = isRecord(parsed) ? parsed["error"] : undefined;
  return typeof error === "string" && error !== "" ? null : `the 400 has no error string: ${body}`;
}

export async function runSigning(
  service: Service,
  capabilities: string[],
  only?: string,
): Promise<SigningOutcome> {
  const refusal = "a P-384 key is refused";
  const picked = (name: string): boolean => only === undefined || name.includes(only);
  const vectors = loadMessages().filter((vector) => picked(vector.name));
  const total = vectors.length + (picked(refusal) ? 1 : 0);
  if (!capabilities.includes(SIGN_CONTEXT)) return { passed: 0, skipped: total, failures: [] };
  const { privateKeyPem, publicKey } = keyPair("prime256v1");
  const failures: string[] = [];
  const check = async (name: string, problem: () => Promise<string | null>): Promise<void> => {
    let found: string | null;
    try {
      found = await problem();
    } catch (error) {
      found = error instanceof Error ? error.message : String(error);
    }
    if (found !== null) failures.push(`${name}: ${found}`);
  };
  for (const vector of vectors)
    await check(vector.name, () => signsMessage(service, privateKeyPem, publicKey, vector));
  if (picked(refusal)) await check(refusal, () => refusesP384(service));
  return { passed: total - failures.length, skipped: 0, failures };
}
