/** The flags the evaluate-versus-SDK comparison runs over, with the clause each one exercises. */
export interface FlagSpec {
  key: string;
  variations: unknown[];
  /** The per-environment config; the helper fills `on`, `targets`, `prerequisites` and `rules` when absent. */
  config: Record<string, unknown>;
}

export const SEGMENT = {
  key: "beta-users",
  included: ["user-3"],
  excluded: ["user-5"],
  rules: [{ clauses: [{ attribute: "country", op: "in", values: ["NL"] }], weight: 50_000 }],
};

export const FLAGS: FlagSpec[] = [
  {
    key: "gate-open",
    variations: [true, false],
    config: { offVariation: 1, fallthrough: { variation: 0 } },
  },
  {
    key: "gate-closed",
    variations: [true, false],
    config: { offVariation: 1, fallthrough: { variation: 1 } },
  },
  {
    // Targets, a prerequisite, a segment rule, an attribute rule and a percentage rollout.
    key: "plan-gate",
    variations: ["control", "pro", "beta", "vip"],
    config: {
      offVariation: 0,
      targets: [{ variation: 3, contextKeys: ["user-7", "ñandú-é"] }],
      prerequisites: [{ flagKey: "gate-open", variation: 0 }],
      rules: [
        { clauses: [{ op: "segmentMatch", values: ["beta-users"] }], variation: 2 },
        {
          clauses: [
            { attribute: "plan", op: "in", values: ["pro", "team"] },
            { attribute: "age", op: "gte", values: [30] },
          ],
          variation: 1,
        },
      ],
      fallthrough: {
        rollout: {
          variations: [
            { variation: 0, weight: 70_000 },
            { variation: 1, weight: 30_000 },
          ],
        },
      },
    },
  },
  {
    key: "rollout-split",
    variations: ["a", "b", "c"],
    config: {
      offVariation: 0,
      fallthrough: {
        rollout: {
          variations: [
            { variation: 0, weight: 33_334 },
            { variation: 1, weight: 33_333 },
            { variation: 2, weight: 33_333 },
          ],
        },
      },
    },
  },
  {
    // A prerequisite that is not met.
    key: "gated",
    variations: ["yes", "no"],
    config: {
      offVariation: 1,
      prerequisites: [{ flagKey: "gate-closed", variation: 0 }],
      fallthrough: { variation: 0 },
    },
  },
  {
    // Off with no off variation: nothing is served.
    key: "kill-switch",
    variations: ["x", "y"],
    config: { on: false, fallthrough: { variation: 0 } },
  },
  {
    key: "ready-marker",
    variations: [true, false],
    config: { offVariation: 1, fallthrough: { variation: 0 } },
  },
];
