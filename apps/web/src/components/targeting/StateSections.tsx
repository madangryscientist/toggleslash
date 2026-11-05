import { Checkbox, Field, Section, Select, Text, Textarea } from "@hackcove/ui-react";
import { at, under } from "#/targeting/problems";
import { removeAt, replaceAt } from "#/targeting/list";
import { VARIATION_LABEL, type SectionProps } from "./shared";

export function StateSection({ draft, onDraft, problems, flag }: SectionProps) {
  return (
    <Section title="State">
      <Checkbox
        label="Targeting is on"
        description="When off, the flag serves its off variation to everyone."
        data-testid="targeting-on"
        checked={draft.on}
        onChange={(e) => onDraft({ ...draft, on: e.currentTarget.checked })}
      />
      <Field
        label="Off variation"
        htmlFor="off-variation"
        error={at(problems, "offVariation")?.message}
      >
        <Select
          id="off-variation"
          data-testid="off-variation"
          width="auto"
          value={draft.offVariation === null ? "" : String(draft.offVariation)}
          onChange={(e) =>
            onDraft({
              ...draft,
              offVariation: e.currentTarget.value === "" ? null : Number(e.currentTarget.value),
            })
          }
        >
          <option value="">None (serves null)</option>
          {flag.variations.map((v, i) => (
            <option key={i} value={String(i)}>
              {VARIATION_LABEL(v, i)}
            </option>
          ))}
        </Select>
      </Field>
    </Section>
  );
}

export function TargetsSection({ draft, onDraft, problems, flag }: SectionProps) {
  return (
    <Section title="Individual targets">
      <Text tone="muted">
        Context keys served a variation whatever the rules say. One per line.
      </Text>
      {flag.variations.map((v, variation) => {
        const index = draft.targets.findIndex((t) => t.variation === variation);
        const error = index < 0 ? undefined : under(problems, `targets[${index}]`)[0]?.message;
        return (
          <Field
            key={variation}
            label={`Serve ${VARIATION_LABEL(v, variation)}`}
            htmlFor={`targets-${variation}`}
            error={error}
          >
            <Textarea
              id={`targets-${variation}`}
              data-testid={`targets-${variation}`}
              rows={3}
              value={index < 0 ? "" : (draft.targets[index]?.keys ?? "")}
              onChange={(e) => {
                const keys = e.currentTarget.value;
                if (index >= 0) {
                  onDraft({
                    ...draft,
                    targets:
                      keys === ""
                        ? removeAt(draft.targets, index)
                        : replaceAt(draft.targets, index, { variation, keys }),
                  });
                } else if (keys !== "") {
                  onDraft({ ...draft, targets: [...draft.targets, { variation, keys }] });
                }
              }}
            />
          </Field>
        );
      })}
    </Section>
  );
}
