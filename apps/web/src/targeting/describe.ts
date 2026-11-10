/** A past config as plain lines for reading, not editing. Pure: no React, no fetch. */
import { SEGMENT_OP, WEIGHT_TOTAL, type Clause, type Serve } from "./model.ts";

/** `0 (true)`: a variation's index, with its value when the ruleset has one. */
export function variationLabel(index: number, variations: string[]): string {
  const value = variations[index];
  return value === undefined ? `${index} (missing)` : `${index} (${value})`;
}

function percent(weight: number): string {
  const whole = weight / (WEIGHT_TOTAL / 100);
  return `${Number.isInteger(whole) ? whole : whole.toFixed(3).replace(/0+$/, "")}%`;
}

/** What a rule or the fallthrough serves: a fixed variation, or a rollout's shares. */
export function describeServe(serve: Serve, variations: string[]): string {
  if (serve.kind === "variation") return `serve ${variationLabel(serve.variation, variations)}`;
  if (serve.entries.length === 0) return "serve a rollout with no entries";
  const shares = serve.entries.map(
    (e) => `${percent(e.weight)} ${variationLabel(e.variation, variations)}`,
  );
  const by =
    serve.contextKind === undefined || serve.contextKind === "user"
      ? ""
      : ` by ${serve.contextKind}`;
  return `roll out ${shares.join(", ")}${by}`;
}

export function describeClause(clause: Clause): string {
  const not = clause.negate ? "not " : "";
  if (clause.op === SEGMENT_OP) return `${not}in segment ${clause.values.join(", ")}`;
  const values = clause.values.map((v) => JSON.stringify(v)).join(", ");
  const kind =
    clause.contextKind === undefined || clause.contextKind === "user"
      ? ""
      : `${clause.contextKind} `;
  return `${kind}${clause.attribute} ${not}${clause.op} ${values}`;
}
