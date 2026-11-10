import type { SingleContext } from "@hackcove/toggleslash-sdk";

/** The shoppers the page can be viewed as. `plan` is what the `pro-customers` segment looks at. */
export const USERS: readonly SingleContext[] = [
  { key: "free-user", attributes: { plan: "free", country: "US" } },
  { key: "pro-user", attributes: { plan: "pro", country: "NL" } },
  { key: "beta-user", attributes: { plan: "free", country: "ZA", beta: true } },
];

const DEFAULT_USER = "free-user";

/** The user for a `?user=` value: absent means the default one, an unknown key is `null`. */
export function userFor(key: string | null): SingleContext | null {
  return USERS.find((user) => user.key === (key ?? DEFAULT_USER)) ?? null;
}
