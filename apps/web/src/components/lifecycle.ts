import type { PillTone } from "@hackcove/ui-react";
import type { Lifecycle } from "#/api/types";

const STAGES: Record<Lifecycle, { label: string; tone: PillTone }> = {
  new: { label: "New", tone: "blue" },
  active: { label: "Active", tone: "emerald" },
  inactive: { label: "Inactive", tone: "slate" },
  archived: { label: "Archived", tone: "amber" },
};

/** How a lifecycle stage reads on a pill. */
export function stageOf(stage: Lifecycle): { label: string; tone: PillTone } {
  return STAGES[stage];
}
