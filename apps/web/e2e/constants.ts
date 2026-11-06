/** Shared by `playwright.config.ts`, the fake API and the specs. */
export const PORT = 4420;
export const API_PORT = 4421;
/** The same build, started with neither sign-in secret. */
export const UNCONFIGURED_PORT = 4422;
/** Throwaway: it only means something to the fake API, and is not a credential for anything. */
export const E2E_ADMIN_TOKEN = "e2e-admin-token-9f3c1a";
/** Throwaway, like the token: they sign in to this run's fake data and nothing else. */
export const E2E_OPERATOR_PASSWORD = "e2e-operator-password-7d2b";
export const E2E_SESSION_SECRET = "e2e-session-secret-4c81a6f09be3d275";
/** Under .wrangler/e2e, which is git-ignored and cleared by the next run. */
export const SIGNED_IN_STATE = ".wrangler/e2e/signed-in.json";
/** A ruleset hash the fake API says shop/production served: `GET .../rulesets/<hash>` answers it, any other hash is a 404. */
export const PAST_RULESET_HASH = "5e7a1c0b9d3f4a68b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9";
