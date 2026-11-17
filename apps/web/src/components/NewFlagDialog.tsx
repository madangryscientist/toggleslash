import { Button, Checkbox, Dialog, Field, Input, Stack } from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { KEY, parseTags } from "./text";
import { createFlag } from "#/api/dashboard";
import type { Project } from "#/api/types";

interface Props {
  project: Project;
  onClose: () => void;
  onCreated: () => Promise<void>;
}

interface Draft {
  key: string;
  name: string;
  description: string;
  temporary: boolean;
  clientVisible: boolean;
  tags: string;
}

const EMPTY: Draft = {
  key: "",
  name: "",
  description: "",
  temporary: false,
  clientVisible: false,
  tags: "",
};

interface FieldsProps {
  draft: Draft;
  onDraft: (draft: Draft) => void;
  error: string | null;
  onSubmit: (event: FormEvent) => void;
}

function Fields({ draft, onDraft, error, onSubmit }: FieldsProps) {
  const keyError =
    draft.key !== "" && !KEY.test(draft.key)
      ? "Lowercase letters, digits, - and _, up to 64."
      : undefined;
  return (
    <form id="new-flag-form" onSubmit={onSubmit} noValidate>
      <Stack gap="sm">
        <Field label="Name">
          <Input
            value={draft.name}
            onChange={(e) => onDraft({ ...draft, name: e.currentTarget.value })}
          />
        </Field>
        <Field label="Key" hint="What code asks for. It cannot change." error={keyError}>
          <Input
            value={draft.key}
            onChange={(e) => onDraft({ ...draft, key: e.currentTarget.value })}
          />
        </Field>
        <Field label="Description">
          <Input
            value={draft.description}
            onChange={(e) => onDraft({ ...draft, description: e.currentTarget.value })}
          />
        </Field>
        <Field label="Tags" hint="Separated by commas or spaces.">
          <Input
            value={draft.tags}
            onChange={(e) => onDraft({ ...draft, tags: e.currentTarget.value })}
          />
        </Field>
        <Checkbox
          label="Temporary"
          hint="A rollout or experiment to remove later, not a permanent switch."
          checked={draft.temporary}
          onChange={(checked) => onDraft({ ...draft, temporary: checked })}
        />
        <Checkbox
          label="Visible to client keys"
          checked={draft.clientVisible}
          onChange={(checked) => onDraft({ ...draft, clientVisible: checked })}
        />
        {error !== null && (
          <p role="alert" data-testid="create-error">
            {error}
          </p>
        )}
      </Stack>
    </form>
  );
}

/** Creates a boolean flag (`[true, false]`). Other variations are the CLI's for now. */
export function NewFlagDialog({ project, onClose, onCreated }: Props) {
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!KEY.test(draft.key)) {
      setError("Give the flag a key: lowercase letters, digits, - and _, up to 64.");
      return;
    }
    if (draft.name.trim() === "") {
      setError("Give the flag a name.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await createFlag({
        data: {
          project: project.key,
          flag: {
            key: draft.key,
            name: draft.name.trim(),
            description: draft.description,
            temporary: draft.temporary,
            clientVisible: draft.clientVisible,
            tags: parseTags(draft.tags),
          },
        },
      });
      if (result.ok) await onCreated();
      else setError(result.failure.message);
    } catch {
      setError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      title="New flag"
      width="lg"
      description={`A boolean flag in ${project.name}: off serves false, on serves true. It starts off in every environment.`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" form="new-flag-form" busy={saving && "Creating"}>
            Create flag
          </Button>
        </>
      }
    >
      <Fields draft={draft} onDraft={setDraft} error={error} onSubmit={(e) => void submit(e)} />
    </Dialog>
  );
}
