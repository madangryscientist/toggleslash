import { Button, Dialog, Field, Input, Stack } from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { createSegment } from "#/api/operator";
import type { Environment, Project, Segment } from "#/api/types";
import { KEY } from "./text";

interface NewProps {
  project: Project;
  environment: Environment;
  onClose: () => void;
  /** The segment was made, empty. */
  onCreated: (segment: Segment) => void;
}

export function NewSegmentDialog({ project, environment, onClose, onCreated }: NewProps) {
  const [key, setKey] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const keyError =
    key !== "" && !KEY.test(key) ? "Lowercase letters, digits, - and _, up to 64." : undefined;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!KEY.test(key)) {
      setError("Give the segment a key: lowercase letters, digits, - and _, up to 64.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await createSegment({
        data: { project: project.key, env: environment.key, key },
      });
      if (result.ok) onCreated(result.saved);
      else setError(result.detail ?? result.failure.message);
    } catch {
      setError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      title="New segment"
      description={`A reusable group of contexts in ${environment.name}. It starts empty; you fill it in next. The key cannot change.`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" form="new-segment-form" busy={saving} busyLabel="Creating">
            Create segment
          </Button>
        </>
      }
    >
      <form id="new-segment-form" onSubmit={(e) => void submit(e)} noValidate>
        <Stack gap="sm">
          <Field label="Key" htmlFor="segment-key" hint="What flag rules name." error={keyError}>
            <Input id="segment-key" value={key} onChange={(e) => setKey(e.currentTarget.value)} />
          </Field>
          {error !== null && (
            <p role="alert" data-testid="create-error">
              {error}
            </p>
          )}
        </Stack>
      </form>
    </Dialog>
  );
}

interface DeleteProps {
  segment: Segment;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (segment: Segment) => void;
}

export function DeleteSegmentDialog({ segment, busy, onCancel, onConfirm }: DeleteProps) {
  return (
    <Dialog
      title={`Delete ${segment.key}?`}
      description="Flag rules can no longer name it. A segment a flag still uses cannot be deleted."
      size="sm"
      onClose={onCancel}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            tone="danger"
            busy={busy}
            busyLabel="Deleting"
            data-testid="confirm-delete-segment"
            onClick={() => onConfirm(segment)}
          >
            Delete
          </Button>
        </>
      }
    />
  );
}
