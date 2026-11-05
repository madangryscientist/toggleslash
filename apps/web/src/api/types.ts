/** What the dashboard knows about the server's admin API. Plain data, safe to send to the browser. */
import type { Config } from "../targeting/model.ts";

export interface Project {
  key: string;
  name: string;
}

export interface Environment {
  key: string;
  name: string;
  /** ISO 8601, UTC; empty when the server did not say. */
  createdAt: string;
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

export type KeyKind = "server" | "client";

/** An SDK key as the server lists it: the prefix identifies it, nothing here can authenticate. */
export interface ApiKey {
  id: number;
  kind: KeyKind;
  name: string;
  prefix: string;
  createdAt: string;
  /** ISO 8601, or `null` while the key still works. */
  revokedAt: string | null;
}

/** The answer to creating a key: the only place the plaintext exists. The page shows it once and drops it. */
export interface CreatedKey extends ApiKey {
  plaintext: string;
}

/** A segment of one environment. `rules` is the JSON text the editor works on. */
export interface Segment {
  key: string;
  included: string[];
  excluded: string[];
  rules: string;
}

/** One project audit event, as the log lists it. */
export interface AuditEvent {
  id: number;
  at: string;
  actor: string;
  entityType: string;
  entityId: string;
  action: string;
  environment: string | null;
  flag: string | null;
}

export interface AuditPage {
  events: AuditEvent[];
  /** Send back for the next page; `null` on the last. */
  nextCursor: string | null;
}

/** What the audit log is narrowed to: an empty string is no filter. `since` is `yyyy-mm-dd`. */
export interface AuditFilters {
  flag: string;
  environment: string;
  since: string;
}

/** A page that works inside one project, with the environment switcher's state. */
export type ProjectLoaded =
  | { ok: false; failure: Failure }
  | {
      ok: true;
      projects: Project[];
      project: Project | null;
      environments: Environment[];
      environment: Environment | null;
    };

/** A project page whose server call worked. */
export type ProjectReady = Extract<ProjectLoaded, { ok: true }>;

export type KeysLoaded =
  | { ok: false; failure: Failure }
  | (Extract<ProjectLoaded, { ok: true }> & { keys: ApiKey[] });

export type SegmentsLoaded =
  | { ok: false; failure: Failure }
  | (Extract<ProjectLoaded, { ok: true }> & { segments: Segment[] });

export type AuditLoaded =
  | { ok: false; failure: Failure }
  | (Extract<ProjectLoaded, { ok: true }> & {
      filters: AuditFilters;
      cursor: string | null;
      page: AuditPage;
    });
