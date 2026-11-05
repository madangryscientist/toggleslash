/**
 * The flags the shop reads, and what each serves when the SDK has nothing (not loaded yet, or the
 * flag is missing). They are the flags `packages/e2e/src/seed.ts` creates. A `null` fallback takes
 * any type, which suits the JSON flag.
 */
export const FLAGS: readonly { key: string; fallback: unknown }[] = [
  { key: "new-checkout", fallback: false },
  { key: "banner-text", fallback: "" },
  { key: "discount-percent", fallback: 0 },
  { key: "checkout-config", fallback: null },
  { key: "legacy-cart", fallback: false },
];
