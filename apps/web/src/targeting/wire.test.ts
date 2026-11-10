import { describe, expect, test } from "vitest";
import { toDraft } from "./draft.ts";
import { parseConfig } from "./model.ts";
import { toWire } from "./wire.ts";

/** A config body as the server stores it, every kind field set: an org target between two user targets. */
const withKinds = {
  on: true,
  offVariation: 1,
  targets: [
    { variation: 0, contextKeys: ["alice"] },
    { variation: 0, contextKeys: ["acme", "globex"], contextKind: "org" },
    { variation: 1, contextKeys: ["bob"] },
  ],
  prerequisites: [],
  rules: [
    {
      clauses: [
        { op: "in", attribute: "plan", values: ["pro"], contextKind: "org" },
        { op: "in", attribute: "country", values: ["NL"] },
        { op: "segmentMatch", values: ["beta"] },
      ],
      rollout: {
        variations: [
          { variation: 0, weight: 50_000 },
          { variation: 1, weight: 50_000 },
        ],
        contextKind: "org",
      },
    },
  ],
  fallthrough: { variation: 1 },
};

function roundTrip(body: Record<string, unknown>): unknown {
  const config = parseConfig({ ...body, version: 1 });
  if (config === null) throw new Error("not a config");
  const result = toWire(toDraft(config, 2));
  if (!result.ok) throw new Error(JSON.stringify(result.problems));
  return result.wire;
}

describe("a config read and saved with no edit", () => {
  test("sends back every context kind it read, in the same order", () => {
    expect(roundTrip(withKinds)).toEqual(withKinds);
  });

  test("a config with no kinds sends exactly today's body", () => {
    const plain = {
      ...withKinds,
      targets: [{ variation: 0, contextKeys: ["alice"] }],
      rules: [
        {
          clauses: [{ op: "in", attribute: "country", values: ["NL"] }],
          rollout: {
            variations: [
              { variation: 0, weight: 50_000 },
              { variation: 1, weight: 50_000 },
            ],
          },
        },
      ],
    };
    const wire = roundTrip(plain);
    expect(wire).toEqual(plain);
    expect(JSON.stringify(wire)).not.toContain("contextKind");
  });

  test("a kind that is not a string is dropped on the way in", () => {
    const config = parseConfig({
      ...withKinds,
      targets: [{ variation: 0, contextKeys: [], contextKind: 7 }],
      version: 1,
    });
    expect(config?.targets[0]).toEqual({ variation: 0, contextKeys: [] });
  });
});

describe("a context kind that is blank or user", () => {
  test("sends no field on a clause or a rollout", () => {
    const config = parseConfig({ ...withKinds, version: 1 });
    if (config === null) throw new Error("not a config");
    const draft = toDraft(config, 2);
    const clause = draft.rules[0]?.clauses[0];
    const serve = draft.rules[0]?.serve;
    if (clause === undefined || serve?.kind !== "rollout") throw new Error("fixture");
    clause.contextKind = " user ";
    serve.contextKind = "";
    const result = toWire(draft);
    expect(result.ok).toBe(true);
    expect(JSON.stringify(result.ok ? result.wire.rules : null)).not.toContain('"contextKind"');
  });
});

function problemsOf(edit: (draft: ReturnType<typeof toDraft>) => void) {
  const config = parseConfig({ ...withKinds, version: 1 });
  if (config === null) throw new Error("not a config");
  const draft = toDraft(config, 2);
  edit(draft);
  const result = toWire(draft);
  return result.ok ? [] : result.problems.map((p) => p.path);
}

describe("an invalid context kind", () => {
  test("on an other-kind target is raised at its index", () => {
    expect(
      problemsOf((d) => {
        const t = d.targets[1];
        if (t !== undefined) t.contextKind = "bad kind";
      }),
    ).toEqual(["targets[1].contextKind"]);
  });

  test("on an other-kind target, blank and user are refused", () => {
    for (const kind of ["", "user"]) {
      expect(
        problemsOf((d) => {
          const t = d.targets[1];
          if (t !== undefined) t.contextKind = kind;
        }),
      ).toEqual(["targets[1].contextKind"]);
    }
  });

  test("kind and multi are reserved", () => {
    for (const kind of ["kind", "multi"]) {
      expect(
        problemsOf((d) => {
          const t = d.targets[1];
          if (t !== undefined) t.contextKind = kind;
        }),
      ).toEqual(["targets[1].contextKind"]);
    }
  });

  test("on a clause is raised at its rule and clause", () => {
    expect(
      problemsOf((d) => {
        const c = d.rules[0]?.clauses[1];
        if (c !== undefined) c.contextKind = "a/b";
      }),
    ).toEqual(["rules[0].clauses[1].contextKind"]);
  });

  test("on a rollout is raised under the serve's path", () => {
    expect(
      problemsOf((d) => {
        const s = d.rules[0]?.serve;
        if (s?.kind === "rollout") s.contextKind = "a b";
      }),
    ).toEqual(["rules[0].rollout.contextKind"]);
    expect(
      problemsOf((d) => {
        d.fallthrough = {
          kind: "rollout",
          entries: [{ variation: 0, percent: "100" }],
          contextKind: "multi",
        };
      }),
    ).toEqual(["fallthrough.rollout.contextKind"]);
  });
});
