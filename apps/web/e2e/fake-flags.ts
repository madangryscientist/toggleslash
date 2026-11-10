/** The fake API's flag lifecycle: the stage on every answer, the archived list, restore and delete. */
import { asBody, failure, type Reply } from "./fake-config.ts";
import { db, type Flag } from "./fake-data.ts";

const ARCHIVED_AT = "2026-11-05T08:00:00Z";

/** A flag as the server answers it: the stored stage, or `archived` once `archivedAt` is set. */
export function answer(found: Flag): unknown {
  return found.archivedAt === null ? found : { ...found, lifecycle: "archived" };
}

/** `GET .../flags` (active only) or `GET .../flags?archived=true` (archived only). */
export function listed(project: string, archived: boolean): unknown[] {
  return (db.data.flags[project] ?? [])
    .filter((f) => (f.archivedAt !== null) === archived)
    .map(answer);
}

function archive(found: Flag): Reply {
  found.archivedAt = ARCHIVED_AT;
  return [200, answer(found)];
}

function restore(found: Flag): Reply {
  found.archivedAt = null;
  return [200, answer(found)];
}

/** The flag that names `key` as a prerequisite in any environment's config, if one does. */
function dependent(project: string, key: string): string | null {
  for (const [path, config] of Object.entries(db.data.configs)) {
    const [p, , owner = ""] = path.split("/");
    if (p !== project || owner === key) continue;
    if (config.prerequisites.some((pre) => asBody(pre)["flagKey"] === key)) return owner;
  }
  return null;
}

/** `DELETE .../flags/<flag>`: archived flags only, and not one another flag needs. */
function remove(project: string, found: Flag): Reply {
  if (found.archivedAt === null) {
    return failure(409, "conflict", `"${found.key}" is not archived; archive it first.`);
  }
  const owner = dependent(project, found.key);
  if (owner !== null) {
    return failure(
      409,
      "conflict",
      `"${found.key}" is a prerequisite of "${owner}"; remove it from them first.`,
    );
  }
  const flags = db.data.flags[project] ?? [];
  db.data.flags[project] = flags.filter((f) => f.key !== found.key);
  for (const path of Object.keys(db.data.configs)) {
    if (path.startsWith(`${project}/`) && path.endsWith(`/${found.key}`)) {
      delete db.data.configs[path];
    }
  }
  return [204, undefined];
}

/** The lifecycle verbs on one flag: archive, restore and delete. `null` when the request is none of them. */
export function lifecycleRoute(project: string, found: Flag, method: string, action?: string) {
  if (action === "archive" && method === "POST") return archive(found);
  if (action === "restore" && method === "POST") return restore(found);
  if (action === undefined && method === "DELETE") return remove(project, found);
  return null;
}
