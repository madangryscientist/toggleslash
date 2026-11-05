/** Which field of the config or the flag a message is about. Pure. */

/**
 * A problem with a field: `path` is the server's own notation (`rules[0].clauses[1].values`). The one
 * exception is a percentage that is not a number, found before anything is sent: `rules[0].rollout.entries[1]`,
 * by position in the editor (the server's index skips zero-weight entries, so it cannot be used).
 */
export interface FieldProblem {
  path: string;
  message: string;
}

const CONFIG_PATH =
  /^((?:targets|prerequisites|rules|fallthrough|offVariation)(?:\[\d+\]|\.[A-Za-z]+)*)/;

/**
 * The field a server message is about. The server's messages open with the path to the field
 * (`rules[0].clauses[1].values[2] must be a string...`), and that opening is the contract here.
 * One that names none (a cycle, a stale version) is returned as `null`: the page shows it on its own.
 */
export function configFieldOf(message: string): FieldProblem | null {
  const found = CONFIG_PATH.exec(message);
  // A bare `prerequisites` (a cycle) has no row to mark: the page shows it above the form.
  if (found?.[1] !== undefined && found[1] !== "prerequisites") {
    return { path: found[1], message };
  }
  return null;
}

/** The part of a flag's metadata a message is about: `name`, `description`, `owner` or `tags`. */
export function metaFieldOf(message: string): FieldProblem | null {
  const found = /^(name|description|owner)\b/.exec(message);
  if (found?.[1] !== undefined) return { path: found[1], message };
  if (/^(every tag|a flag takes at most \d+ tags|tag)\b/.test(message))
    return { path: "tags", message };
  return null;
}

/** The problems at `path` or below it (`rules[0]` holds `rules[0].rollout`), for a section that shows them together. */
export function under(problems: FieldProblem[], path: string): FieldProblem[] {
  return problems.filter(
    (p) => p.path === path || p.path.startsWith(`${path}.`) || p.path.startsWith(`${path}[`),
  );
}

/** The problems at exactly `path`. */
export function at(problems: FieldProblem[], path: string): FieldProblem | undefined {
  return problems.find((p) => p.path === path);
}
