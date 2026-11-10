/**
 * The server functions of the environments page: create, rename and delete. Their handlers run in the
 * Worker, with the admin token.
 */
import { createServerFn } from "@tanstack/react-start";
import { recordOf, str } from "./json.ts";
import type { Environment, MutationResult, SaveResult } from "./types.ts";
import { mutate, save } from "./result.server.ts";

interface NewEnvironmentInput {
  project: string;
  key: string;
  name: string;
}

function newEnvironmentInput(data: unknown): NewEnvironmentInput {
  const d = recordOf(data);
  return { project: str(d["project"]), key: str(d["key"]), name: str(d["name"]) };
}

export const createEnvironment = createServerFn({ method: "POST" })
  .validator(newEnvironmentInput)
  .handler(({ data }): Promise<SaveResult<Environment>> =>
    save((api) => api.createEnvironment(data.project, data.key, data.name)),
  );

interface RenameEnvironmentInput {
  project: string;
  env: string;
  name: string;
}

function renameEnvironmentInput(data: unknown): RenameEnvironmentInput {
  const d = recordOf(data);
  return { project: str(d["project"]), env: str(d["env"]), name: str(d["name"]) };
}

/** Changes an environment's name. Its key is what code and URLs use, and never changes. */
export const renameEnvironment = createServerFn({ method: "POST" })
  .validator(renameEnvironmentInput)
  .handler(({ data }): Promise<SaveResult<Environment>> =>
    save((api) => api.renameEnvironment(data.project, data.env, data.name)),
  );

interface DeleteEnvironmentInput {
  project: string;
  env: string;
}

function deleteEnvironmentInput(data: unknown): DeleteEnvironmentInput {
  const d = recordOf(data);
  return { project: str(d["project"]), env: str(d["env"]) };
}

/** Deletes an environment with its targeting, segments and keys; the server refuses while a key is not revoked. */
export const deleteEnvironment = createServerFn({ method: "POST" })
  .validator(deleteEnvironmentInput)
  .handler(({ data }): Promise<MutationResult> =>
    mutate((api) => api.deleteEnvironment(data.project, data.env)),
  );
