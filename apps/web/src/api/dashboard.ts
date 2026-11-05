/**
 * The server functions the dashboard's routes call. Their handlers run in the Worker, with the admin
 * token; the browser sees only the plain data they return (`types.ts`), never the token or the URL.
 */
import { createServerFn } from "@tanstack/react-start";
import { recordOf, str, strings } from "./json.ts";
import type { Failure, Loaded, MutationResult, NewFlag } from "./types.ts";
import { ApiFailure, type AdminApi } from "./client.server.ts";
import { adminApi } from "./env.server.ts";

function failureOf(error: unknown): Failure {
  if (error instanceof ApiFailure) return { kind: error.kind, message: error.message };
  return { kind: "error", message: "The dashboard hit an unexpected error." };
}

interface Selection {
  project?: string | undefined;
  env?: string | undefined;
}

function selection(data: unknown): Selection {
  const d = recordOf(data);
  return {
    project: typeof d["project"] === "string" ? d["project"] : undefined,
    env: typeof d["env"] === "string" ? d["env"] : undefined,
  };
}

/**
 * Everything the flag list needs for the chosen project and environment (the first of each when none
 * is chosen). A down, misconfigured or unauthorised server is a value, not a throw, so the page can say so.
 */
export const loadDashboard = createServerFn({ method: "GET" })
  .validator(selection)
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
        };
      }
      const environments = await made.api.environments(project.key);
      const environment = environments.find((e) => e.key === data.env) ?? environments[0] ?? null;
      const flags = environment === null ? [] : await made.api.flags(project.key, environment.key);
      return { ok: true, projects, project, environments, environment, flags };
    } catch (error) {
      return { ok: false, failure: failureOf(error) };
    }
  });

async function mutate(run: (api: AdminApi) => Promise<void>): Promise<MutationResult> {
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
