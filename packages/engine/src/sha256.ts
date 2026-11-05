/**
 * SHA-256 (FIPS 180-4) in plain TypeScript. Web Crypto's `digest` is async, and this is what keeps
 * `evaluate` synchronous; `docs/evaluation.md` records the choice.
 */
const ROUND_CONSTANTS = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

const INITIAL_STATE = [
  0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
];

const BLOCK_BYTES = 64;
const LENGTH_FIELD_BYTES = 8;
const TWO_POW_29 = 0x20000000;

function rotr(x: number, n: number): number {
  return (x >>> n) | (x << (32 - n));
}

function word(words: Uint32Array, i: number): number {
  return words[i] ?? 0;
}

function fillSchedule(schedule: Uint32Array, data: DataView, offset: number): void {
  for (let t = 0; t < 16; t++) schedule[t] = data.getUint32(offset + t * 4);
  for (let t = 16; t < 64; t++) {
    const w15 = word(schedule, t - 15);
    const w2 = word(schedule, t - 2);
    const s0 = rotr(w15, 7) ^ rotr(w15, 18) ^ (w15 >>> 3);
    const s1 = rotr(w2, 17) ^ rotr(w2, 19) ^ (w2 >>> 10);
    schedule[t] = word(schedule, t - 16) + s0 + word(schedule, t - 7) + s1;
  }
}

function compress(state: Uint32Array, schedule: Uint32Array): void {
  let a = word(state, 0);
  let b = word(state, 1);
  let c = word(state, 2);
  let d = word(state, 3);
  let e = word(state, 4);
  let f = word(state, 5);
  let g = word(state, 6);
  let h = word(state, 7);
  for (let t = 0; t < 64; t++) {
    const s1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
    const choose = (e & f) ^ (~e & g);
    const t1 = (h + s1 + choose + word(ROUND_CONSTANTS, t) + word(schedule, t)) | 0;
    const s0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
    const majority = (a & b) ^ (a & c) ^ (b & c);
    h = g;
    g = f;
    f = e;
    e = (d + t1) | 0;
    d = c;
    c = b;
    b = a;
    a = (t1 + s0 + majority) | 0;
  }
  [a, b, c, d, e, f, g, h].forEach((value, i) => {
    state[i] = word(state, i) + value;
  });
}

/** The padded message: the bytes, a 0x80, zeros, then the bit length as a big-endian uint64. */
function pad(message: Uint8Array): DataView {
  const minimum = message.length + 1 + LENGTH_FIELD_BYTES;
  const length = Math.ceil(minimum / BLOCK_BYTES) * BLOCK_BYTES;
  const padded = new Uint8Array(length);
  padded.set(message);
  padded[message.length] = 0x80;
  const view = new DataView(padded.buffer);
  view.setUint32(length - 8, Math.floor(message.length / TWO_POW_29));
  view.setUint32(length - 4, (message.length * 8) % 2 ** 32);
  return view;
}

export function sha256(message: Uint8Array): Uint8Array {
  const data = pad(message);
  const state = new Uint32Array(INITIAL_STATE);
  const schedule = new Uint32Array(64);
  for (let offset = 0; offset < data.byteLength; offset += BLOCK_BYTES) {
    fillSchedule(schedule, data, offset);
    compress(state, schedule);
  }
  const digest = new DataView(new ArrayBuffer(32));
  state.forEach((value, i) => {
    digest.setUint32(i * 4, value);
  });
  return new Uint8Array(digest.buffer);
}
