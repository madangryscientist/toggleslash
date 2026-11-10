/**
 * The server functions the dashboard's routes call. Their handlers run in the Worker, with the admin
 * token; the browser sees only the plain data they return (`types.ts`), never the token or the URL.
 */
import { createServerFn } from "@tanstack/react-start";
import { recordOf, selection, str, strings } from "./json.ts";
import type { DetailLoaded, FlagDetail, Loaded, NewFlag, SaveResult } from "./types.ts";
import type { Config } from "../targeting/model.ts";
import { wireOf, type Wire } from "../targeting/wire.ts";
import type { FlagPatch } from "./client.server.ts";
import { adminApi } from "./env.server.ts";
import { failureOf, mutate, save } from "./result.server.ts";

function dashboardInput(data: unknown) {
  return { ...selection(data), archived: recordOf(data)["archived"] === true };
}

/**
 * Everything the flag list needs for the chosen project and environment (the first of each when none
 * is chosen). A down, misconfigured or unauthorised server is a value, not a throw, so the page can say so.
 */
export const loadDashboard = createServerFn({ method: "GET" })
  .validator(dashboardInput)
  .handler(async ({ data }): Promise<Loaded> => {
    const made = adminApi();
    if (!made.ok) return { ok: false, failure: { kind: "unconfigured", message: made.message } };
    try {
      const projects = await made.api.projects();
      const project = projects.find((p) => p.key === data.project) ?? projects[0] ?? null;
      if (project === null) {
        return {
          ok: true,
          projects,
          project,
          environments: [],
          environment: null,
          flags: [],
          archived: data.archived,
        };
      }
      const environments = await made.api.environments(project.key);
      const environment = environments.find((e) => e.key === data.env) ?? environments[0] ?? null;
      const flags =
        environment === null
          ? []
          : await made.api.flags(project.key, environment.key, { archived: data.archived });
      return {
        ok: true,
        projects,
        project,
        environments,
        environment,
        flags,
        archived: data.archived,
      };
    } catch (error) {
      return { ok: false, failure: failureOf(error) };
    }
  });

interface ToggleInput {
  project: string;
  env: string;
  flag: string;
  on: boolean;
  version: number | null;
}

function toggleInput(data: unknown): ToggleInput {
  const d = recordOf(data);
  return {
    project: str(d["project"]),
    env: str(d["env"]),
    flag: str(d["flag"]),
    on: d["on"] === true,
    version: typeof d["version"] === "number" ? d["version"] : null,
  };
}

export const toggleFlag = createServerFn({ method: "POST" })
  .validator(toggleInput)
  .handler(({ data }) => mutate((api) => api.toggle(data)));

interface CreateInput {
  project: string;
  flag: NewFlag;
}

function createInput(data: unknown): CreateInput {
  const d = recordOf(data);
  const f = recordOf(d["flag"]);
  return {
    project: str(d["project"]),
    flag: {
      key: str(f["key"]),
      name: str(f["name"]),
      description: str(f["description"]),
      temporary: f["temporary"] === true,
      tags: strings(f["tags"]),
    },
  };
}

export const createFlag = createServerFn({ method: "POST" })
  .validator(createInput)
  .handler(({ data }) => mutate((api) => api.createFlag(data.project, data.flag)));

function archiveInput(data: unknown): { project: string; flag: string } {
  const d = recordOf(data);
  return { project: str(d["project"]), flag: str(d["flag"]) };
}

export const archiveFlag = createServerFn({ method: "POST" })
  .validator(archiveInput)
  .handler(({ data }) => mutate((api) => api.archiveFlag(data.project, data.flag)));

export const restoreFlag = createServerFn({ method: "POST" })
  .validator(archiveInput)
  .handler(({ data }) => mutate((api) => api.restoreFlag(data.project, data.flag)));

/** Deletes an archived flag; the server refuses one that is not archived, or that another flag needs. */
export const deleteFlag = createServerFn({ method: "POST" })
  .validator(archiveInput)
  .handler(({ data }) => mutate((api) => api.deleteFlag(data.project, data.flag)));

interface DetailInput {
  project: string;
  flag: string;
  env?: string | undefined;
}

function detailInput(data: unknown): DetailInput {
  const d = recordOf(data);
  return {
    project: str(d["project"]),
    flag: str(d["flag"]),
    env: typeof d["env"] === "string" ? d["env"] : undefined,
  };
}

/**
 * Everything the flag detail page shows: the flag, its config in the chosen environment (the first when
 * none is chosen), the other flags a prerequisite can use, its newest history and its usage.
 */
export const loadFlagDetail = createServerFn({ method: "GET" })
  .validator(detailInput)
  .handler(async ({ data }): Promise<DetailLoaded> => {
    const made = adminApi();
    if (!made.ok) return { ok: false, failure: { kind: "unconfigured", message: made.message } };
    const api = made.api;
    try {
      const projects = await api.projects();
      const project = projects.find((p) => p.key === data.project);
      if (project === undefined) {
        return { ok: false, failure: { kind: "error", message: `No project "${data.project}".` } };
      }
      const environments = await api.environments(project.key);
      const environment = environments.find((e) => e.key === data.env) ?? environments[0] ?? null;
      const [flag, config, siblings, history, usage] = await Promise.all([
        api.flag(project.key, data.flag),
        environment === null ? null : api.config(project.key, data.flag, environment.key),
        api.siblings(project.key),
        api.history(project.key, data.flag),
        api.usage(project.key, data.flag),
      ]);
      return {
        ok: true,
        project,
        environments,
        environment,
        flag,
        config,
        siblings: siblings.filter((s) => s.key !== data.flag),
        history,
        usage,
      };
    } catch (error) {
      return { ok: false, failure: failureOf(error) };
    }
  });

interface MetaInput {
  project: string;
  flag: string;
  patch: FlagPatch;
}

function metaInput(data: unknown): MetaInput {
  const d = recordOf(data);
  const p = recordOf(d["patch"]);
  return {
    project: str(d["project"]),
    flag: str(d["flag"]),
    patch: {
      name: str(p["name"]),
      description: str(p["description"]),
      owner: str(p["owner"]),
      temporary: p["temporary"] === true,
      tags: strings(p["tags"]),
    },
  };
}

export const saveFlagMeta = createServerFn({ method: "POST" })
  .validator(metaInput)
  .handler(({ data }): Promise<SaveResult<FlagDetail>> =>
    save((api) => api.updateFlag(data.project, data.flag, data.patch)),
  );

interface ConfigInput {
  project: string;
  flag: string;
  env: string;
  version: number;
  wire: Wire;
}

function configInput(data: unknown): ConfigInput {
  const d = recordOf(data);
  return {
    project: str(d["project"]),
    flag: str(d["flag"]),
    env: str(d["env"]),
    version: typeof d["version"] === "number" ? d["version"] : 0,
    wire: wireOf(d["wire"]),
  };
}

/**
 * Saves a flag's whole config in one environment. The browser sends the wire body the editor built
 * (`toWire`), so the server's own validation is the only judge of it; `version` is the one the page read.
 */
export const saveConfig = createServerFn({ method: "POST" })
  .validator(configInput)
  .handler(({ data }): Promise<SaveResult<Config>> => {
    return save((api) => api.putConfig(data, data.wire, data.version));
  });
