/**
 * The server functions of the operator pages: SDK keys, segments and the audit log.
 * Their handlers run in the Worker, with the admin token. The one answer that carries a key's
 * plaintext is `createKey`'s; no loader returns it.
 */
import { createServerFn } from "@tanstack/react-start";
import { kindMembers, recordOf, selection, str, strings, type Selection } from "./json.ts";
import type {
  AuditFilters,
  AuditLoaded,
  CreatedKey,
  Failure,
  KeyKind,
  KeysLoaded,
  KindMembers,
  MutationResult,
  ProjectLoaded,
  ProjectReady,
  RulesetLoaded,
  SaveResult,
  Segment,
  SegmentsLoaded,
} from "./types.ts";
import type { AdminApi } from "./client.server.ts";
import { failureOf, mutate, save } from "./result.server.ts";
import { adminApi } from "./env.server.ts";

/** The projects and environments every operator page starts from: the chosen ones, else the first. */
async function scope(api: AdminApi, data: Selection): Promise<ProjectReady> {
  const projects = await api.projects();
  const project = projects.find((p) => p.key === data.project) ?? projects[0] ?? null;
  if (project === null) return { ok: true, projects, project, environments: [], environment: null };
  const environments = await api.environments(project.key);
  const environment = environments.find((e) => e.key === data.env) ?? environments[0] ?? null;
  return { ok: true, projects, project, environments, environment };
}

/** Runs a loader on the admin API; a down, misconfigured or unauthorised server is a value, not a throw. */
async function load<T extends { ok: boolean }>(
  run: (api: AdminApi) => Promise<T>,
): Promise<T | { ok: false; failure: Failure }> {
  const made = adminApi();
  if (!made.ok) return { ok: false, failure: { kind: "unconfigured", message: made.message } };
  try {
    return await run(made.api);
  } catch (error) {
    return { ok: false, failure: failureOf(error) };
  }
}

export const loadEnvironments = createServerFn({ method: "GET" })
  .validator(selection)
  .handler((ctx): Promise<ProjectLoaded> => load((api) => scope(api, ctx.data)));

/** The environment's keys, revoked ones included: prefixes and dates, never a plaintext. */
export const loadKeys = createServerFn({ method: "GET" })
  .validator(selection)
  .handler((ctx): Promise<KeysLoaded> =>
    load(async (api) => {
      const base = await scope(api, ctx.data);
      if (base.project === null || base.environment === null) return { ...base, keys: [] };
      return { ...base, keys: await api.keys(base.project.key, base.environment.key) };
    }),
  );

export const loadSegments = createServerFn({ method: "GET" })
  .validator(selection)
  .handler((ctx): Promise<SegmentsLoaded> =>
    load(async (api) => {
      const base = await scope(api, ctx.data);
      if (base.project === null || base.environment === null) return { ...base, segments: [] };
      return { ...base, segments: await api.segments(base.project.key, base.environment.key) };
    }),
  );

interface AuditInput {
  project?: string | undefined;
  env?: string | undefined;
  filters: AuditFilters;
  cursor: string | null;
}

function auditInput(data: unknown): AuditInput {
  const d = recordOf(data);
  return {
    ...selection(data),
    filters: {
      flag: str(d["flag"]),
      environment: str(d["environment"]),
      since: str(d["since"]),
    },
    cursor: typeof d["cursor"] === "string" && d["cursor"] !== "" ? d["cursor"] : null,
  };
}

export const loadAudit = createServerFn({ method: "GET" })
  .validator(auditInput)
  .handler((ctx): Promise<AuditLoaded> =>
    load(async (api) => {
      const { filters, cursor, ...wanted } = ctx.data;
      const base = await scope(api, wanted);
      const empty = { events: [], nextCursor: null };
      if (base.project === null) return { ...base, filters, cursor, page: empty };
      return { ...base, filters, cursor, page: await api.audit(base.project.key, filters, cursor) };
    }),
  );

interface RulesetInput {
  project: string;
  env: string;
  hash: string;
  flag: string | null;
}

function rulesetInput(data: unknown): RulesetInput {
  const d = recordOf(data);
  return {
    project: str(d["project"]),
    env: str(d["env"]),
    hash: str(d["hash"]),
    flag: str(d["flag"]) === "" ? null : str(d["flag"]),
  };
}

/** What the flags were under a ruleset hash: the page a trace links to. Needs the project and environment the hash belongs to. */
export const loadRuleset = createServerFn({ method: "GET" })
  .validator(rulesetInput)
  .handler(({ data }): Promise<RulesetLoaded> =>
    load(async (api) => ({
      ok: true,
      project: data.project,
      env: data.env,
      hash: data.hash,
      flag: data.flag,
      found: await api.ruleset(data.project, data.env, data.hash, data.flag),
    })),
  );

interface NewKeyInput {
  project: string;
  env: string;
  kind: KeyKind;
  name: string;
}

function newKeyInput(data: unknown): NewKeyInput {
  const d = recordOf(data);
  return {
    project: str(d["project"]),
    env: str(d["env"]),
    kind: d["kind"] === "client" ? "client" : "server",
    name: str(d["name"]),
  };
}

/** Creates a key. The answer holds the plaintext, the only time anything does: the page must not store it. */
export const createKey = createServerFn({ method: "POST" })
  .validator(newKeyInput)
  .handler(({ data }): Promise<SaveResult<CreatedKey>> =>
    save((api) => api.createKey(data.project, data.env, data.kind, data.name)),
  );

interface RevokeInput {
  project: string;
  env: string;
  id: number;
}

function revokeInput(data: unknown): RevokeInput {
  const d = recordOf(data);
  return {
    project: str(d["project"]),
    env: str(d["env"]),
    id: typeof d["id"] === "number" ? d["id"] : 0,
  };
}

export const revokeKey = createServerFn({ method: "POST" })
  .validator(revokeInput)
  .handler(({ data }): Promise<MutationResult> =>
    mutate((api) => api.revokeKey(data.project, data.env, data.id)),
  );

interface SegmentInput {
  project: string;
  env: string;
  segment: string;
  included: string[];
  excluded: string[];
  includedContexts: KindMembers[];
  excludedContexts: KindMembers[];
  rules: string;
  version: number;
}

function segmentInput(data: unknown): SegmentInput {
  const d = recordOf(data);
  return {
    project: str(d["project"]),
    env: str(d["env"]),
    segment: str(d["segment"]),
    included: strings(d["included"]),
    excluded: strings(d["excluded"]),
    includedContexts: kindMembers(d["includedContexts"]),
    excludedContexts: kindMembers(d["excludedContexts"]),
    rules: str(d["rules"]),
    version: typeof d["version"] === "number" ? d["version"] : 0,
  };
}

/** Replaces a segment's members and rules; the server validates the rules and says what is wrong. */
export const saveSegment = createServerFn({ method: "POST" })
  .validator(segmentInput)
  .handler(({ data }): Promise<SaveResult<Segment>> => {
    let rules: unknown;
    try {
      rules = JSON.parse(data.rules);
    } catch {
      return Promise.resolve({
        ok: false,
        failure: { kind: "error", message: "The rules are not valid JSON." },
        detail: "The rules are not valid JSON.",
      });
    }
    return save((api) =>
      api.putSegment(data.project, data.env, data.segment, {
        included: data.included,
        excluded: data.excluded,
        includedContexts: data.includedContexts,
        excludedContexts: data.excludedContexts,
        rules,
        version: data.version,
      }),
    );
  });

interface NewSegmentInput {
  project: string;
  env: string;
  key: string;
}

function newSegmentInput(data: unknown): NewSegmentInput {
  const d = recordOf(data);
  return { project: str(d["project"]), env: str(d["env"]), key: str(d["key"]) };
}

/** Creates an empty segment; the dashboard opens its edit dialog next. */
export const createSegment = createServerFn({ method: "POST" })
  .validator(newSegmentInput)
  .handler(({ data }): Promise<SaveResult<Segment>> =>
    save((api) => api.createSegment(data.project, data.env, data.key)),
  );

interface DeleteSegmentInput {
  project: string;
  env: string;
  segment: string;
}

function deleteSegmentInput(data: unknown): DeleteSegmentInput {
  const d = recordOf(data);
  return { project: str(d["project"]), env: str(d["env"]), segment: str(d["segment"]) };
}

export const deleteSegment = createServerFn({ method: "POST" })
  .validator(deleteSegmentInput)
  .handler(({ data }): Promise<MutationResult> =>
    mutate((api) => api.deleteSegment(data.project, data.env, data.segment)),
  );
