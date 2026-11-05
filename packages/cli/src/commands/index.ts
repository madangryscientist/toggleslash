import type { Command } from "../command.ts";
import { CATALOGUE } from "./catalogue.ts";
import { CONFIG } from "./config.ts";
import { FLAGS } from "./flags.ts";
import { KEYS } from "./keys.ts";

export const COMMANDS: readonly Command[] = [...CONFIG, ...CATALOGUE, ...FLAGS, ...KEYS];
