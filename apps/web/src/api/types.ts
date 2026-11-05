/** What the dashboard knows about the server's admin API. Plain data, safe to send to the browser. */

export interface Project {
  key: string;
  name: string;
}

export interface Environment {
  key: string;
  name: string;
}

export interface FlagRow {
  key: string;
  name: string;
  description: string;
  tags: string[];
  temporary: boolean;
  /** `yyyy-mm-dd`, or `null` when the flag has never been evaluated. */
  lastEvaluated: string | null;
  /** Whether targeting is on in the chosen environment; `null` when its config could not be read. */
  on: boolean | null;
  /** The config's version, sent back with a toggle so a change made elsewhere is not overwritten. */
  version: number | null;
}

/** Why the server could not be used: each has its own page copy. */
export type FailureKind = "unconfigured" | "unreachable" | "unauthorized" | "error";

export interface Failure {
  kind: FailureKind;
  message: string;
}

export type Loaded =
  | { ok: false; failure: Failure }
  | {
      ok: true;
      projects: Project[];
      project: Project | null;
      environments: Environment[];
      environment: Environment | null;
      flags: FlagRow[];
    };

export type MutationResult = { ok: true } | { ok: false; failure: Failure; conflict?: boolean };

export interface NewFlag {
  key: string;
  name: string;
  description: string;
  temporary: boolean;
  tags: string[];
}
