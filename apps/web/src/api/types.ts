/** What the dashboard knows about the server's admin API. Plain data, safe to send to the browser. */
import type { Config } from "../targeting/model.ts";

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

/** One flag, as the detail page edits it. */
export interface FlagDetail {
  key: string;
  name: string;
  description: string;
  owner: string;
  tags: string[];
  temporary: boolean;
  archived: boolean;
  /** Each variation's value as JSON text (`true`, `"blue"`, `42`); the index is what targeting refers to. */
  variations: string[];
  lastEvaluated: string | null;
}

/** Another flag a prerequisite can point at. */
export interface Sibling {
  key: string;
  name: string;
  variations: string[];
}

export interface HistoryEvent {
  id: number;
  /** ISO 8601, UTC. */
  at: string;
  actor: string;
  entityType: string;
  action: string;
  environment: string | null;
  /** The config version the write produced; `null` for an event that is not a config write. */
  version: number | null;
}

export interface UsageDay {
  day: string;
  count: number;
  variations: { variation: number; count: number }[];
}

export interface Usage {
  environments: { environment: string; total: number; days: UsageDay[] }[];
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

export type DetailLoaded =
  | { ok: false; failure: Failure }
  | {
      ok: true;
      project: Project;
      environments: Environment[];
      environment: Environment | null;
      flag: FlagDetail;
      /** The chosen environment's config, or `null` when the flag has none there. */
      config: Config | null;
      siblings: Sibling[];
      history: HistoryEvent[];
      usage: Usage;
    };

/** What a save of the metadata or the targeting answered: the saved state, or why not. */
export type SaveResult<T> =
  | { ok: true; saved: T }
  | { ok: false; failure: Failure; conflict?: boolean | undefined; detail?: string | undefined };

export type MutationResult = { ok: true } | { ok: false; failure: Failure; conflict?: boolean };

export interface NewFlag {
  key: string;
  name: string;
  description: string;
  temporary: boolean;
  tags: string[];
}
