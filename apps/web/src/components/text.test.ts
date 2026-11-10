import { describe, expect, test } from "vitest";
import type { AuditEvent, FlagRow } from "#/api/types";
import { day, describeEvent, matches, parseMembers, parseTags, ruleCount, stamp } from "./text";

const flag: FlagRow = {
  key: "checkout-v2",
  name: "Checkout v2",
  description: "",
  tags: ["payments", "ui"],
  temporary: false,
  lifecycle: null,
  lastEvaluated: null,
  on: true,
  version: 1,
};

describe("matches", () => {
  test("an empty search matches everything", () => {
    expect(matches(flag, "")).toBe(true);
    expect(matches(flag, "   ")).toBe(true);
  });
  test("looks in key, name and tags, ignoring case", () => {
    expect(matches(flag, "CHECKOUT")).toBe(true);
    expect(matches(flag, "pay")).toBe(true);
    expect(matches(flag, "v2")).toBe(true);
  });
  test("every word must match", () => {
    expect(matches(flag, "checkout ui")).toBe(true);
    expect(matches(flag, "checkout search")).toBe(false);
  });
});

describe("parseTags", () => {
  test("splits on commas and spaces and drops repeats and blanks", () => {
    expect(parseTags("a, B  c,,A")).toEqual(["a", "b", "c"]);
    expect(parseTags("")).toEqual([]);
  });
});

describe("day, stamp and parseMembers", () => {
  test("day keeps the date of an ISO time and is empty for none", () => {
    expect(day("2026-11-05T08:00:00Z")).toBe("2026-11-05");
    expect(day("")).toBe("");
  });

  test("stamp drops fractions and the zone letter, and says UTC", () => {
    expect(stamp("2026-11-05T08:01:02Z")).toBe("2026-11-05 08:01:02 UTC");
    expect(stamp("2026-11-05T08:01:02.123456Z")).toBe("2026-11-05 08:01:02 UTC");
  });

  test("parseMembers splits on lines only, trims, and drops blanks and repeats", () => {
    expect(parseMembers("alice\n bob \n\nalice\na,b")).toEqual(["alice", "bob", "a,b"]);
    expect(parseMembers("")).toEqual([]);
  });
});

const event: AuditEvent = {
  id: 1,
  at: "2026-11-05T08:00:00Z",
  actor: "admin",
  entityType: "api_key",
  entityId: "3",
  action: "revoked",
  environment: "production",
  flag: null,
};

describe("describeEvent", () => {
  test("names the action, the entity and the environment", () => {
    expect(describeEvent(event)).toBe("revoked api_key 3 in production");
  });

  test("leaves out the environment when the event has none", () => {
    expect(describeEvent({ ...event, environment: null })).toBe("revoked api_key 3");
  });
});

describe("ruleCount", () => {
  test("counts a JSON array and refuses anything else", () => {
    expect(ruleCount("[]")).toBe(0);
    expect(ruleCount('[{"clauses": []}, {"clauses": []}]')).toBe(2);
    expect(ruleCount("{}")).toBeNull();
    expect(ruleCount("not json")).toBeNull();
  });
});
