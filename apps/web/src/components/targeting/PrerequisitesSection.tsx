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
  disabled: boolean;
  onChange: (next: Prerequisite) => void;
  onRemove: () => void;
}

function PrerequisiteRow({
  index,
  prerequisite,
  siblings,
  error,
  disabled,
  onChange,
  onRemove,
}: RowProps) {
  const sibling = siblings.find((s) => s.key === prerequisite.flagKey);
  return (
    <Field label={`Prerequisite ${index + 1}`} htmlFor={`prerequisite-${index}`} error={error}>
      <Cluster gap="xs">
        <Select
          id={`prerequisite-${index}`}
          data-testid={`prerequisite-${index}-flag`}
          disabled={disabled}
          width="auto"
          value={prerequisite.flagKey}
          onChange={(e) => onChange({ flagKey: e.currentTarget.value, variation: 0 })}
          items={[
            ...(sibling === undefined
              ? [{ value: prerequisite.flagKey, label: prerequisite.flagKey || "Choose a flag" }]
              : []),
            ...siblings.map((s) => ({ value: s.key, label: `${s.name} (${s.key})` })),
          ]}
        />
        <Select
          aria-label={`Prerequisite ${index + 1} variation`}
          data-testid={`prerequisite-${index}-variation`}
          disabled={disabled}
          width="auto"
          value={String(prerequisite.variation)}
          onChange={(e) => onChange({ ...prerequisite, variation: Number(e.currentTarget.value) })}
          items={(sibling?.variations ?? []).map((v, j) => ({
            value: String(j),
            label: VARIATION_LABEL(v, j),
          }))}
        />
        <Button
          variant="ghost"
          size="sm"
          tone="danger"
          aria-label={`Remove prerequisite ${index + 1}`}
          disabled={disabled}
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
  disabled,
}: SectionProps & { siblings: Sibling[] }) {
  return (
    <Section
      title="Prerequisites"
      actions={
        <Button
          variant="outline"
          size="sm"
          data-testid="add-prerequisite"
          disabled={disabled || siblings.length === 0}
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
          disabled={disabled}
          onChange={(next) =>
            onDraft({ ...draft, prerequisites: replaceAt(draft.prerequisites, i, next) })
          }
          onRemove={() => onDraft({ ...draft, prerequisites: removeAt(draft.prerequisites, i) })}
        />
      ))}
    </Section>
  );
}
