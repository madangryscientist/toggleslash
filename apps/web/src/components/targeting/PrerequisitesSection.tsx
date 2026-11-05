import { Button, Cluster, Field, Section, Select, Text } from "@hackcove/ui-react";
import { under } from "#/targeting/problems";
import { removeAt, replaceAt } from "#/targeting/list";
import type { Sibling } from "#/api/types";
import type { Prerequisite } from "#/targeting/model";
import { VARIATION_LABEL, type SectionProps } from "./shared";

interface RowProps {
  index: number;
  prerequisite: Prerequisite;
  siblings: Sibling[];
  error: string | undefined;
  onChange: (next: Prerequisite) => void;
  onRemove: () => void;
}

function PrerequisiteRow({ index, prerequisite, siblings, error, onChange, onRemove }: RowProps) {
  const sibling = siblings.find((s) => s.key === prerequisite.flagKey);
  return (
    <Field label={`Prerequisite ${index + 1}`} htmlFor={`prerequisite-${index}`} error={error}>
      <Cluster wrap>
        <Select
          id={`prerequisite-${index}`}
          data-testid={`prerequisite-${index}-flag`}
          width="auto"
          value={prerequisite.flagKey}
          onChange={(e) => onChange({ flagKey: e.currentTarget.value, variation: 0 })}
        >
          {sibling === undefined && (
            <option value={prerequisite.flagKey}>{prerequisite.flagKey || "Choose a flag"}</option>
          )}
          {siblings.map((s) => (
            <option key={s.key} value={s.key}>
              {s.name} ({s.key})
            </option>
          ))}
        </Select>
        <Select
          aria-label={`Prerequisite ${index + 1} variation`}
          data-testid={`prerequisite-${index}-variation`}
          width="auto"
          value={String(prerequisite.variation)}
          onChange={(e) => onChange({ ...prerequisite, variation: Number(e.currentTarget.value) })}
        >
          {(sibling?.variations ?? []).map((v, j) => (
            <option key={j} value={String(j)}>
              {VARIATION_LABEL(v, j)}
            </option>
          ))}
        </Select>
        <Button
          variant="ghost"
          size="sm"
          tone="danger"
          aria-label={`Remove prerequisite ${index + 1}`}
          onClick={onRemove}
        >
          Remove
        </Button>
      </Cluster>
    </Field>
  );
}

export function PrerequisitesSection({
  draft,
  onDraft,
  problems,
  siblings,
}: SectionProps & { siblings: Sibling[] }) {
  return (
    <Section
      title="Prerequisites"
      actions={
        <Button
          variant="outline"
          size="sm"
          data-testid="add-prerequisite"
          disabled={siblings.length === 0}
          onClick={() =>
            onDraft({
              ...draft,
              prerequisites: [
                ...draft.prerequisites,
                { flagKey: siblings[0]?.key ?? "", variation: 0 },
              ],
            })
          }
        >
          Add prerequisite
        </Button>
      }
    >
      <Text tone="muted">
        This flag is only on for a context when each of these flags serves the variation named.
      </Text>
      {draft.prerequisites.map((p, i) => (
        <PrerequisiteRow
          key={i}
          index={i}
          prerequisite={p}
          siblings={siblings}
          error={under(problems, `prerequisites[${i}]`)[0]?.message}
          onChange={(next) =>
            onDraft({ ...draft, prerequisites: replaceAt(draft.prerequisites, i, next) })
          }
          onRemove={() => onDraft({ ...draft, prerequisites: removeAt(draft.prerequisites, i) })}
        />
      ))}
    </Section>
  );
}
