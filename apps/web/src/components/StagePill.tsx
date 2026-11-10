import { Pill } from "@hackcove/ui-react";
import type { Lifecycle } from "#/api/types";
import { stageOf } from "./lifecycle";

/** A flag's lifecycle stage as a pill. */
export function StagePill({ stage }: { stage: Lifecycle }) {
  const { label, tone } = stageOf(stage);
  return (
    <Pill tone={tone} data-testid="stage">
      {label}
    </Pill>
  );
}
