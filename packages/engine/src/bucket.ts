import { sha256 } from "./sha256.ts";

/** Buckets run 0..99999; a rollout weight of 1000 is 1%. */
export const BUCKET_COUNT = 100_000;

const encoder = new TextEncoder();

/**
 * Where a context lands for a flag or segment: SHA-256 of the UTF-8 bytes of
 * `<key>.<salt>.<contextKey>`, the first 4 bytes as a big-endian uint32, modulo 100000.
 */
export function bucket(key: string, salt: string, contextKey: string): number {
  const digest = sha256(encoder.encode(`${key}.${salt}.${contextKey}`));
  return new DataView(digest.buffer).getUint32(0) % BUCKET_COUNT;
}
