import type { Command } from "../command.ts";
import { CATALOGUE } from "./catalogue.ts";
import { CONFIG } from "./config.ts";
import { DIAGNOSE } from "./diagnose.ts";
import { FLAGS } from "./flags.ts";
import { KEYS } from "./keys.ts";
import { RULESETS } from "./rulesets.ts";
import { RUNTIME } from "./runtime.ts";
import { TOKENS } from "./tokens.ts";

export const COMMANDS: readonly Command[] = [
  ...CONFIG,
  ...CATALOGUE,
  ...FLAGS,
  ...KEYS,
  ...TOKENS,
  ...DIAGNOSE,
  ...RUNTIME,
  ...RULESETS,
];
