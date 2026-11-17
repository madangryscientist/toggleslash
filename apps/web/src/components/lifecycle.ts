import type { PillTone } from "@hackcove/ui-react";
import type { Lifecycle } from "#/api/types";

const STAGES: Record<Lifecycle, { label: string; tone: PillTone }> = {
  new: { label: "New", tone: "info" },
  active: { label: "Active", tone: "positive" },
  inactive: { label: "Inactive", tone: "neutral" },
  archived: { label: "Archived", tone: "warn" },
};

/** How a lifecycle stage reads on a pill. */
export function stageOf(stage: Lifecycle): { label: string; tone: PillTone } {
  return STAGES[stage];
}
