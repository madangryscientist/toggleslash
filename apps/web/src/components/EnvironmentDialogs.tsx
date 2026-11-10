import { Button, Dialog, Field, Input, Stack } from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { createEnvironment, renameEnvironment } from "#/api/environments";
import type { Environment } from "#/api/types";
import { KEY } from "./text";

interface DialogProps {
  project: string;
  onClose: () => void;
  onCreated: () => Promise<void>;
}

export function NewEnvironmentDialog({ project, onClose, onCreated }: DialogProps) {
  const [key, setKey] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const keyError =
    key !== "" && !KEY.test(key) ? "Lowercase letters, digits, - and _, up to 64." : undefined;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!KEY.test(key)) {
      setError("Give the environment a key: lowercase letters, digits, - and _, up to 64.");
      return;
    }
    if (name.trim() === "") {
      setError("Give the environment a name.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await createEnvironment({ data: { project, key, name: name.trim() } });
      if (result.ok) await onCreated();
      else setError(result.detail ?? result.failure.message);
    } catch {
      setError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      title="New environment"
      description="Flags get their own targeting here, and SDK keys belong to one environment. The key cannot change."
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" form="new-env-form" busy={saving} busyLabel="Creating">
            Create environment
          </Button>
        </>
      }
    >
      <form id="new-env-form" onSubmit={(e) => void submit(e)} noValidate>
        <Stack gap="sm">
          <Field label="Name" htmlFor="env-name">
            <Input id="env-name" value={name} onChange={(e) => setName(e.currentTarget.value)} />
          </Field>
          <Field label="Key" htmlFor="env-key" hint="What code and URLs use." error={keyError}>
            <Input id="env-key" value={key} onChange={(e) => setKey(e.currentTarget.value)} />
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

interface RenameProps {
  project: string;
  environment: Environment;
  onClose: () => void;
  onChanged: () => Promise<void>;
}

export function RenameEnvironmentDialog({ project, environment, onClose, onChanged }: RenameProps) {
  const [name, setName] = useState(environment.name);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (name.trim() === "") {
      setError("Give the environment a name.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await renameEnvironment({
        data: { project, env: environment.key, name: name.trim() },
      });
      if (result.ok) await onChanged();
      else setError(result.detail ?? result.failure.message);
    } catch {
      setError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      title={`Rename ${environment.name}`}
      description="Only the name changes. The key is what code and URLs use, and stays the same."
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" form="rename-env-form" busy={saving} busyLabel="Saving">
            Rename
          </Button>
        </>
      }
    >
      <form id="rename-env-form" onSubmit={(e) => void submit(e)} noValidate>
        <Stack gap="sm">
          <Field label="Name" htmlFor="env-rename">
            <Input id="env-rename" value={name} onChange={(e) => setName(e.currentTarget.value)} />
          </Field>
          {error !== null && (
            <p role="alert" data-testid="rename-error">
              {error}
            </p>
          )}
        </Stack>
      </form>
    </Dialog>
  );
}

interface DeleteProps {
  environment: Environment;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (environment: Environment) => void;
}

export function DeleteEnvironmentDialog({ environment, busy, onCancel, onConfirm }: DeleteProps) {
  return (
    <Dialog
      title={`Delete ${environment.key}?`}
      description="Its targeting, segments and SDK keys go with it. An environment with a key that is not revoked cannot be deleted."
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
            data-testid="confirm-delete-environment"
            onClick={() => onConfirm(environment)}
          >
            Delete
          </Button>
        </>
      }
    />
  );
}
