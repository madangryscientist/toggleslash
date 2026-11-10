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

/** Where a flag is in its life, as the server computes it. */
export type Lifecycle = "new" | "active" | "inactive" | "archived";

export interface FlagRow {
  key: string;
  name: string;
  description: string;
  tags: string[];
  temporary: boolean;
  /** `null` when the answer carries none or a word this dashboard does not know. */
  lifecycle: Lifecycle | null;
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
  lifecycle: Lifecycle | null;
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
      /** Whether `flags` are the archived ones. */
      archived: boolean;
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

/** Segment members of a context kind other than user. */
export interface KindMembers {
  contextKind: string;
  values: string[];
}

/** A segment of one environment. `rules` is the JSON text the editor works on. */
export interface Segment {
  key: string;
  included: string[];
  excluded: string[];
  includedContexts: KindMembers[];
  excludedContexts: KindMembers[];
  rules: string;
  /** What a save sends back, so a stale one is refused. */
  version: number;
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

/** One flag as it was under a past ruleset hash. There is no salt: the server never sends one on this route. */
export interface PastFlag {
  key: string;
  /** Each variation's value as JSON text; the index is what the rules refer to. */
  variations: string[];
  config: Config;
}

/** A segment as it was under a past ruleset hash. `rules` is JSON text. */
export interface PastSegment {
  key: string;
  included: string[];
  excluded: string[];
  includedContexts: KindMembers[];
  excludedContexts: KindMembers[];
  rules: string;
}

/** What the server kept for one served ruleset hash. */
export interface PastRuleset {
  /** The hash: lowercase hex, no quotes. */
  hash: string;
  /** ISO 8601, UTC. */
  firstServedAt: string;
  flags: PastFlag[];
  segments: PastSegment[];
}

/** The answer to asking what the flags were under a hash: the ruleset, or that the environment never served it. */
export type RulesetLoaded =
  | { ok: false; failure: Failure }
  | {
      ok: true;
      project: string;
      env: string;
      hash: string;
      /** The one flag the page was narrowed to, if it was. */
      flag: string | null;
      found: { ok: true; ruleset: PastRuleset } | { ok: false; message: string };
    };
