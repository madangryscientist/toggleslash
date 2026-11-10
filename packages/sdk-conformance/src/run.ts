/** One whole run: every scenario, then every vector, against one service. */
import { type Outcome, loadScenarios, runScenario, type Scenario } from "./scenarios.ts";
import { Service } from "./service.ts";
import { runVectors, type VectorOutcome } from "./vectors.ts";

/** Scenarios run this many at a time; their windows are wide enough for it. */
const CONCURRENCY = 6;

export interface Summary {
  service: string;
  outcomes: Outcome[];
  vectors: VectorOutcome;
}

interface RunOptions {
  serviceUrl: string;
  /** Only scenarios and vectors whose name contains this. */
  only?: string | undefined;
  /** Each line of the report, as it happens. */
  print: (line: string) => void;
}

function line(outcome: Outcome): string {
  const name = `${outcome.scenario.file.replace(/\.json$/, "")}: ${outcome.scenario.name}`;
  if (outcome.result === "pass") return `pass  ${name}`;
  if (outcome.result === "skip") return `skip  ${name} (${outcome.detail})`;
  return `FAIL  ${name}\n  ${outcome.detail}`;
}

async function runScenarios(
  scenarios: Scenario[],
  service: Service,
  capabilities: string[],
  print: RunOptions["print"],
): Promise<Outcome[]> {
  const outcomes: Outcome[] = [];
  let next = 0;
  const worker = async (): Promise<void> => {
    while (next < scenarios.length) {
      const index = next++;
      const scenario = scenarios[index];
      if (scenario === undefined) return;
      const outcome = await runScenario(scenario, index, service, capabilities);
      print(line(outcome));
      outcomes.push(outcome);
    }
  };
  await Promise.all(Array.from({ length: CONCURRENCY }, worker));
  return outcomes;
}

export async function runAll(options: RunOptions): Promise<Summary> {
  const service = new Service(options.serviceUrl);
  const described = await service.describe();
  options.print(
    `service: ${described.name}; capabilities: ${described.capabilities.join(", ") || "none"}`,
  );
  const { only } = options;
  const scenarios = loadScenarios().filter(
    (scenario) => only === undefined || scenario.name.includes(only),
  );
  const outcomes = await runScenarios(scenarios, service, described.capabilities, options.print);
  const vectors = await runVectors(service, only);
  for (const failure of vectors.failures) options.print(`FAIL  vector ${failure}`);
  return { service: described.name, outcomes, vectors };
}

export function summaryLine(summary: Summary): string {
  const count = (result: Outcome["result"]): number =>
    summary.outcomes.filter((outcome) => outcome.result === result).length;
  const { vectors } = summary;
  return (
    `scenarios: ${count("pass")} passed, ${count("fail")} failed, ${count("skip")} skipped; ` +
    `vectors: ${vectors.passed} passed, ${vectors.failures.length} failed, ${vectors.skipped} skipped`
  );
}

export function failed(summary: Summary): boolean {
  return (
    summary.outcomes.some((outcome) => outcome.result === "fail") ||
    summary.vectors.failures.length > 0
  );
}
