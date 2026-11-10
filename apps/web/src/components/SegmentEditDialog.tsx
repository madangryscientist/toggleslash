import { Button, Cluster, ClusterGrow, Dialog, Field, Stack, Textarea } from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { saveSegment } from "#/api/operator";
import type { Environment, Project, Segment } from "#/api/types";
import { parseMembers, ruleCount } from "./text";

interface EditProps {
  project: Project;
  environment: Environment;
  segment: Segment;
  onClose: () => void;
  onSaved: () => Promise<void>;
}

interface Draft {
  included: string;
  excluded: string;
  rules: string;
}

interface FieldsProps {
  draft: Draft;
  onChange: (draft: Draft) => void;
  rulesError: string | undefined;
}

/** The three boxes, as text: contexts one per line, rules as JSON. */
function SegmentFields({ draft, onChange, rulesError }: FieldsProps) {
  return (
    <>
      <Cluster align="start">
        <ClusterGrow>
          <Field
            label="Always included"
            htmlFor="segment-included"
            hint="One context key per line."
          >
            <Textarea
              id="segment-included"
              rows={3}
              value={draft.included}
              onChange={(e) => onChange({ ...draft, included: e.currentTarget.value })}
            />
          </Field>
        </ClusterGrow>
        <ClusterGrow>
          <Field label="Always excluded" htmlFor="segment-excluded" hint="Wins over included.">
            <Textarea
              id="segment-excluded"
              rows={3}
              value={draft.excluded}
              onChange={(e) => onChange({ ...draft, excluded: e.currentTarget.value })}
            />
          </Field>
        </ClusterGrow>
      </Cluster>
      <Field
        label="Rules"
        htmlFor="segment-rules"
        hint="A JSON array, as docs/evaluation.md describes."
        error={rulesError}
      >
        <Textarea
          id="segment-rules"
          rows={5}
          value={draft.rules}
          onChange={(e) => onChange({ ...draft, rules: e.currentTarget.value })}
        />
      </Field>
    </>
  );
}

const CONFLICT =
  "Someone else changed this segment, so this was not saved. Your edits are still here: copy what you need, then reload the page to get the latest version.";

export function EditSegmentDialog({ project, environment, segment, onClose, onSaved }: EditProps) {
  const [draft, setDraft] = useState<Draft>({
    included: segment.included.join("\n"),
    excluded: segment.excluded.join("\n"),
    rules: segment.rules,
  });
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const rulesError =
    ruleCount(draft.rules) === null ? "The rules must be a JSON array." : undefined;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (rulesError !== undefined) {
      setError(rulesError);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await saveSegment({
        data: {
          project: project.key,
          env: environment.key,
          segment: segment.key,
          included: parseMembers(draft.included),
          excluded: parseMembers(draft.excluded),
          rules: draft.rules,
          version: segment.version,
        },
      });
      if (result.ok) await onSaved();
      else if (result.conflict === true) setError(CONFLICT);
      else setError(result.detail ?? result.failure.message);
    } catch {
      setError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      title={`Edit ${segment.key}`}
      description={`Saving replaces its members and rules in ${environment.name}.`}
      size="lg"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="segment-form"
            busy={saving}
            busyLabel="Saving"
            data-testid="save-segment"
          >
            Save segment
          </Button>
        </>
      }
    >
      <form id="segment-form" onSubmit={(e) => void submit(e)} noValidate>
        <Stack gap="sm">
          <SegmentFields draft={draft} onChange={setDraft} rulesError={rulesError} />
          {error !== null && (
            <p role="alert" data-testid="segment-error">
              {error}
            </p>
          )}
        </Stack>
      </form>
    </Dialog>
  );
}
