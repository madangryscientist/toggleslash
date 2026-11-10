import {
  Button,
  Checkbox,
  Cluster,
  Field,
  Input,
  Section,
  Select,
  Stack,
  SubsectionHeading,
  Text,
  Textarea,
} from "@hackcove/ui-react";
import { at, under } from "#/targeting/problems";
import type { TargetDraft } from "#/targeting/draft";
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
        const index = draft.targets.findIndex(
          (t) => t.variation === variation && t.contextKind === undefined,
        );
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
      <OtherKindTargets draft={draft} onDraft={onDraft} problems={problems} flag={flag} />
    </Section>
  );
}

function AddOtherTarget({ draft, onDraft }: Pick<SectionProps, "draft" | "onDraft">) {
  const add = () =>
    onDraft({ ...draft, targets: [...draft.targets, { variation: 0, contextKind: "", keys: "" }] });
  return (
    <div>
      <Button variant="outline" size="sm" data-testid="add-other-target" onClick={add}>
        Add a target for another kind
      </Button>
    </div>
  );
}

/** Targets for a context kind other than user: one row each, in the draft's order (the first match wins). */
function OtherKindTargets({ draft, onDraft, problems, flag }: SectionProps) {
  return (
    <Stack gap="sm">
      <SubsectionHeading>Other context kinds</SubsectionHeading>
      {draft.targets.map((target, index) => {
        if (target.contextKind === undefined) return null;
        const id = `other-target-${index}`;
        const change = (next: Partial<TargetDraft>) =>
          onDraft({ ...draft, targets: replaceAt(draft.targets, index, { ...target, ...next }) });
        return (
          <Stack key={index} gap="xs">
            <Cluster wrap align="start">
              <Field label="Serve" htmlFor={`${id}-variation`}>
                <Select
                  id={`${id}-variation`}
                  data-testid={`${id}-variation`}
                  width="auto"
                  value={String(target.variation)}
                  onChange={(e) => change({ variation: Number(e.currentTarget.value) })}
                >
                  {flag.variations.map((v, i) => (
                    <option key={i} value={String(i)}>
                      {VARIATION_LABEL(v, i)}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="Context kind" htmlFor={`${id}-kind`}>
                <Input
                  id={`${id}-kind`}
                  data-testid={`${id}-kind`}
                  width="auto"
                  value={target.contextKind}
                  onChange={(e) => change({ contextKind: e.currentTarget.value })}
                />
              </Field>
              <Field label="Context keys" htmlFor={`${id}-keys`} hint="One per line.">
                <Textarea
                  id={`${id}-keys`}
                  data-testid={`${id}-keys`}
                  rows={3}
                  value={target.keys}
                  onChange={(e) => change({ keys: e.currentTarget.value })}
                />
              </Field>
              <Button
                variant="ghost"
                size="sm"
                tone="danger"
                data-testid={`${id}-remove`}
                onClick={() => onDraft({ ...draft, targets: removeAt(draft.targets, index) })}
              >
                Remove
              </Button>
            </Cluster>
            {under(problems, `targets[${index}]`).map((p) => (
              <Text key={p.path} tone="danger">
                {p.message}
              </Text>
            ))}
          </Stack>
        );
      })}
      <AddOtherTarget draft={draft} onDraft={onDraft} />
    </Stack>
  );
}
