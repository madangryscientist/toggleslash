import { useState } from "react";
import type { MutationResult } from "#/api/types";

export type Run = (flag: string, action: () => Promise<MutationResult>) => Promise<void>;

/** The busy flag, the last problem, and a `run` that reports a failed or refused call as that problem. */
export function useRunner(onChanged: () => Promise<void>) {
  const [busy, setBusy] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const run: Run = async (flag, action) => {
    setBusy(flag);
    setProblem(null);
    try {
      const result = await action();
      if (!result.ok) setProblem(result.failure.message);
      // Refresh on success, and on a conflict so the row shows what the server now has.
      if (result.ok || result.conflict) await onChanged();
    } catch {
      setProblem("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setBusy(null);
    }
  };
  return { busy, problem, run };
}
