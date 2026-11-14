import type { FlagDetail } from "#/api/types";
import type { Draft } from "#/targeting/draft";
import type { FieldProblem } from "#/targeting/problems";

export const VARIATION_LABEL = (v: string, i: number) => `${i}: ${v}`;

export interface SectionProps {
  draft: Draft;
  onDraft: (draft: Draft) => void;
  problems: FieldProblem[];
  flag: FlagDetail;
  /** True until React has hydrated the page: the controls are not wired yet, so they stay uneditable. */
  disabled: boolean;
}
