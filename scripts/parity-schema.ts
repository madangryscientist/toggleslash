/** The data model and validator for docs/feature-parity.json. */

export type Status = "yes" | "partial" | "no";
export type FeatureSlashStatus = Status | "planned";

export interface Competitor {
  id: string;
  name: string;
  short: string;
  url: string;
}

export interface FeatureSlashCell {
  status: FeatureSlashStatus;
  evidence: string[];
  note: string | undefined;
}

export interface Feature {
  id: string;
  category: string;
  name: string;
  description: string;
  featureslash: FeatureSlashCell;
  competitors: Record<string, Status>;
  note: string | undefined;
}

export interface Source {
  name: string;
  url: string;
}

export interface ParityData {
  updated: string;
  note: string;
  competitors: Competitor[];
  features: Feature[];
  sources: Source[];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function text(record: Record<string, unknown>, key: string, where: string): string {
  const value = record[key];
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${where}: "${key}" must be a non-empty string.`);
  }
  return value;
}

function optionalText(
  record: Record<string, unknown>,
  key: string,
  where: string,
): string | undefined {
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new Error(`${where}: "${key}" must be a string.`);
  return value;
}

function parseStatus(value: unknown, where: string): Status {
  if (value === "yes" || value === "partial" || value === "no") return value;
  throw new Error(`${where}: expected yes, partial or no.`);
}

function parseFeatureStatus(value: unknown, where: string): FeatureSlashStatus {
  if (value === "planned") return value;
  return parseStatus(value, where);
}

function parseCompetitors(raw: unknown): Competitor[] {
  if (!Array.isArray(raw) || raw.length === 0) {
    throw new Error('root: "competitors" must be a non-empty array.');
  }
  const seen = new Set<string>();
  const competitors: Competitor[] = [];
  for (const [index, item] of raw.entries()) {
    const where = `competitors[${index}]`;
    if (!isRecord(item)) throw new Error(`${where}: must be an object.`);
    const id = text(item, "id", where);
    if (seen.has(id)) throw new Error(`${where}: duplicate competitor id "${id}".`);
    seen.add(id);
    competitors.push({
      id,
      name: text(item, "name", where),
      short: text(item, "short", where),
      url: text(item, "url", where),
    });
  }
  return competitors;
}

function parseEvidence(raw: unknown, where: string, status: FeatureSlashStatus): string[] {
  if (!Array.isArray(raw)) throw new Error(`${where}: "evidence" must be an array.`);
  const evidence: string[] = [];
  for (const item of raw) {
    if (typeof item !== "string" || item.trim() === "") {
      throw new Error(`${where}: every evidence entry must be a non-empty string.`);
    }
    evidence.push(item);
  }
  if ((status === "yes" || status === "partial") && evidence.length === 0) {
    throw new Error(`${where}: a "${status}" feature needs at least one evidence entry.`);
  }
  return evidence;
}

function parseFeature(raw: unknown, index: number, competitorIds: Set<string>): Feature {
  const where = `features[${index}]`;
  if (!isRecord(raw)) throw new Error(`${where}: must be an object.`);
  const id = text(raw, "id", where);
  const cell = raw["featureslash"];
  if (!isRecord(cell)) throw new Error(`${where}: "featureslash" must be an object.`);
  const status = parseFeatureStatus(cell["status"], `${where}.featureslash`);
  const evidence = parseEvidence(cell["evidence"], `${where}.featureslash`, status);
  const competitorsRaw = raw["competitors"];
  if (!isRecord(competitorsRaw)) {
    throw new Error(`${where}: "competitors" must be an object.`);
  }
  const competitors: Record<string, Status> = {};
  for (const key of Object.keys(competitorsRaw)) {
    if (!competitorIds.has(key)) throw new Error(`${where}: unknown competitor "${key}".`);
    competitors[key] = parseStatus(competitorsRaw[key], `${where}.competitors.${key}`);
  }
  for (const competitorId of competitorIds)
    competitors[competitorId] = competitors[competitorId] ?? "no";
  return {
    id,
    category: text(raw, "category", where),
    name: text(raw, "name", where),
    description: text(raw, "description", where),
    featureslash: {
      status,
      evidence,
      note: optionalText(cell, "note", `${where}.featureslash`),
    },
    competitors,
    note: optionalText(raw, "note", where),
  };
}

function parseFeatures(raw: unknown, competitors: Competitor[]): Feature[] {
  if (!Array.isArray(raw) || raw.length === 0) {
    throw new Error('root: "features" must be a non-empty array.');
  }
  const ids = new Set<string>();
  const competitorIds = new Set(competitors.map((competitor) => competitor.id));
  const features: Feature[] = [];
  for (const [index, item] of raw.entries()) {
    const feature = parseFeature(item, index, competitorIds);
    if (ids.has(feature.id))
      throw new Error(`features[${index}]: duplicate feature id "${feature.id}".`);
    ids.add(feature.id);
    features.push(feature);
  }
  return features;
}

function parseSources(raw: unknown): Source[] {
  if (!Array.isArray(raw) || raw.length === 0) {
    throw new Error('root: "sources" must be a non-empty array.');
  }
  return raw.map((item, index) => {
    const where = `sources[${index}]`;
    if (!isRecord(item)) throw new Error(`${where}: must be an object.`);
    return { name: text(item, "name", where), url: text(item, "url", where) };
  });
}

export function parseParity(raw: unknown): ParityData {
  if (!isRecord(raw)) throw new Error("root: the file must contain an object.");
  const updated = text(raw, "updated", "root");
  if (!/^\d{4}-\d{2}-\d{2}$/.test(updated)) {
    throw new Error('root: "updated" must be a date written as yyyy-mm-dd.');
  }
  const competitors = parseCompetitors(raw["competitors"]);
  return {
    updated,
    note: text(raw, "note", "root"),
    competitors,
    features: parseFeatures(raw["features"], competitors),
    sources: parseSources(raw["sources"]),
  };
}
