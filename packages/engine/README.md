# @hackcove/toggleslash-engine

The ToggleSlash flag evaluator for TypeScript: pure functions over a ruleset, with no network, no clock and no
dependencies. It implements [`docs/evaluation.md`](https://github.com/slashsuite/toggleslash/blob/main/docs/evaluation.md), and every shared vector in
`fixtures/evaluation/` runs against it. Most applications want the
[server SDK](https://github.com/slashsuite/toggleslash/blob/main/docs/sdk.md) (`@hackcove/toggleslash-sdk`), which keeps a ruleset current and calls this for you.
Published on npm by `release.yml`.

```sh
npm install @hackcove/toggleslash-engine
```

```ts
import { createRuleset, evaluate } from "@hackcove/toggleslash-engine";

const ruleset = createRuleset({
  flags: [
    { key: "new-checkout", variations: [false, true], on: true, offVariation: 0, targets: [], prerequisites: [],
      rules: [{ clauses: [{ attribute: "plan", op: "in", values: ["pro"] }], variation: 1 }],
      fallthrough: { variation: 0 }, salt: "s", version: 1 },
  ],
  segments: [],
});

evaluate(ruleset, "new-checkout", { key: "user-1", attributes: { plan: "pro" } });
// { value: true, variationIndex: 1, reason: { kind: "RULE_MATCH", ruleIndex: 0 } }
```
