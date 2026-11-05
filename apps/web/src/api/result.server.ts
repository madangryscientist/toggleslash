/**
 * What a server function answers when a call to the admin API goes wrong: a failure as plain data, never a
 * throw. Runs only in the Worker (the `.server.ts` name keeps it out of the client bundle).
 */
import { ApiFailure, type AdminApi } from "./client.server.ts";
import { adminApi } from "./env.server.ts";
import type { Failure, MutationResult, SaveResult } from "./types.ts";

export function failureOf(error: unknown): Failure {
  if (error instanceof ApiFailure) return { kind: error.kind, message: error.message };
  return { kind: "error", message: "The dashboard hit an unexpected error." };
}

export async function mutate(run: (api: AdminApi) => Promise<void>): Promise<MutationResult> {
  const made = adminApi();
  if (!made.ok) return { ok: false, failure: { kind: "unconfigured", message: made.message } };
  try {
    await run(made.api);
    return { ok: true };
  } catch (error) {
    const failure = failureOf(error);
    const conflict = error instanceof ApiFailure && error.status === 409;
    return conflict ? { ok: false, failure, conflict } : { ok: false, failure };
  }
}

/** Runs a write and keeps what the server answered; a refusal carries the server's own message as `detail`. */
export async function save<T>(run: (api: AdminApi) => Promise<T>): Promise<SaveResult<T>> {
  const made = adminApi();
  if (!made.ok) return { ok: false, failure: { kind: "unconfigured", message: made.message } };
  try {
    return { ok: true, saved: await run(made.api) };
  } catch (error) {
    const failure = failureOf(error);
    if (!(error instanceof ApiFailure)) return { ok: false, failure };
    return { ok: false, failure, detail: error.detail, conflict: error.status === 409 };
  }
}
