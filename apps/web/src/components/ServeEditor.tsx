import { Field, Input, Select, Stack, Text } from "@hackcove/ui-react";
import { rolloutTotal, switchServe, type ServeDraft } from "#/targeting/draft";
import { at, type FieldProblem } from "#/targeting/problems";

interface Props {
  /** Prefix for ids and test ids, unique on the page: `rule-0`, `fallthrough`. */
  id: string;
  label: string;
  serve: ServeDraft;
  variations: string[];
  onChange: (serve: ServeDraft) => void;
  /** The problems that concern this serve as a whole; the rollout entries' own are found by path. */
  problems: FieldProblem[];
  /** The server's path to this serve (`rules[0]`, `fallthrough`), which its rollout problems start with. */
  path: string;
  /** The editor is not live yet: every control is disabled. */
  disabled: boolean;
}

const KIND_LABELS = { variation: "A variation", rollout: "A percentage rollout" } as const;

function VariationPicker({
  id,
  serve,
  variations,
  disabled,
  onChange,
}: {
  id: string;
  disabled: boolean;
  serve: Extract<ServeDraft, { kind: "variation" }>;
  variations: string[];
  onChange: (next: ServeDraft) => void;
}) {
  return (
    <Field label="Variation" htmlFor={`${id}-variation`}>
      <Select
        id={`${id}-variation`}
        data-testid={`${id}-variation`}
        disabled={disabled}
        width="auto"
        value={String(serve.variation)}
        onChange={(e) => onChange({ kind: "variation", variation: Number(e.currentTarget.value) })}
      >
        {variations.map((v, i) => (
          <option key={i} value={String(i)}>
            {i}: {v}
          </option>
        ))}
      </Select>
    </Field>
  );
}

function RolloutKind({
  id,
  serve,
  error,
  disabled,
  onChange,
}: {
  id: string;
  disabled: boolean;
  serve: RolloutServe;
  error: string | undefined;
  onChange: (next: ServeDraft) => void;
}) {
  return (
    <Field
      label="Bucket by context kind"
      htmlFor={`${id}-rollout-kind`}
      error={error}
      hint="Blank is user."
    >
      <Input
        id={`${id}-rollout-kind`}
        data-testid={`${id}-rollout-kind`}
        disabled={disabled}
        width="auto"
        placeholder="user"
        value={serve.contextKind}
        onChange={(e) => onChange({ ...serve, contextKind: e.currentTarget.value })}
      />
    </Field>
  );
}

type RolloutServe = Extract<ServeDraft, { kind: "rollout" }>;

function RolloutEditor({
  id,
  serve,
  variations,
  problems,
  path,
  disabled,
  onChange,
}: Omit<Props, "label" | "serve"> & { serve: RolloutServe }) {
  return (
    <Stack gap="xs">
      {serve.entries.map((entry, i) => (
        <Field
          key={entry.variation}
          label={`${variations[entry.variation] ?? entry.variation} (%)`}
          htmlFor={`${id}-percent-${entry.variation}`}
          error={at(problems, `${path}.rollout.entries[${i}]`)?.message}
        >
          <Input
            id={`${id}-percent-${entry.variation}`}
            data-testid={`${id}-percent-${entry.variation}`}
            disabled={disabled}
            inputMode="decimal"
            width="auto"
            value={entry.percent}
            onChange={(e) =>
              onChange({
                ...serve,
                entries: serve.entries.map((x, j) =>
                  j === i ? { ...x, percent: e.currentTarget.value } : x,
                ),
              })
            }
          />
        </Field>
      ))}
      <RolloutKind
        id={id}
        serve={serve}
        error={at(problems, `${path}.rollout.contextKind`)?.message}
        disabled={disabled}
        onChange={onChange}
      />
      <Text tone="muted" data-testid={`${id}-total`}>
        Total {rolloutTotal(serve.entries) / 1000}% (it must be 100%)
      </Text>
    </Stack>
  );
}

/** What a rule or the fallthrough serves: one variation, or a split between them by percentage. */
export function ServeEditor({
  id,
  label,
  serve,
  variations,
  onChange,
  problems,
  path,
  disabled,
}: Props) {
  const kindPath = `${path}.rollout.contextKind`;
  const wholeProblem = problems.find(
    (p) => !p.path.includes(".rollout.entries[") && p.path !== kindPath,
  );
  return (
    <Stack gap="sm">
      <Field label={label} htmlFor={`${id}-kind`} error={wholeProblem?.message}>
        <Select
          id={`${id}-kind`}
          data-testid={`${id}-kind`}
          disabled={disabled}
          width="auto"
          value={serve.kind}
          onChange={(e) =>
            onChange(
              switchServe(
                serve,
                e.currentTarget.value === "rollout" ? "rollout" : "variation",
                variations.length,
              ),
            )
          }
        >
          {(["variation", "rollout"] as const).map((kind) => (
            <option key={kind} value={kind}>
              {KIND_LABELS[kind]}
            </option>
          ))}
        </Select>
      </Field>
      {serve.kind === "variation" ? (
        <VariationPicker
          id={id}
          serve={serve}
          variations={variations}
          disabled={disabled}
          onChange={onChange}
        />
      ) : (
        <RolloutEditor
          id={id}
          serve={serve}
          variations={variations}
          problems={problems}
          path={path}
          disabled={disabled}
          onChange={onChange}
        />
      )}
    </Stack>
  );
}
