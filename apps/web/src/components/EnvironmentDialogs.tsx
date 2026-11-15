import { Button, Callout, CodeBlock, Dialog, Field, Input, Stack, Text } from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { createEnvironment, createSigningKey, renameEnvironment } from "#/api/environments";
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

interface SigningKeyProps {
  project: string;
  environment: Environment;
  onClose: () => void;
  /** A key was made: refresh the row, and keep this dialog open on the key. */
  onMade: () => Promise<void>;
}

/**
 * Makes a signing key after a confirm, then shows its private half. The key lives in this dialog's state
 * only: close the dialog and it is gone, and the server cannot show it again (it keeps the public half).
 */
export function SigningKeyDialog({ project, environment, onClose, onMade }: SigningKeyProps) {
  const [privateKey, setPrivateKey] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  async function make() {
    setSaving(true);
    setError(null);
    try {
      const result = await createSigningKey({ data: { project, env: environment.key } });
      if (result.ok) {
        setPrivateKey(result.saved.privateKey);
        await onMade();
      } else setError(result.detail ?? result.failure.message);
    } catch {
      setError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  if (privateKey !== null) {
    return (
      <Dialog
        title={`Signing key for ${environment.name}`}
        description="Your application's server signs each context with this key. Keep it in that server's environment, never in a browser or in your code."
        onClose={onClose}
        footer={
          <Button onClick={onClose} data-testid="close-signing-key">
            I have copied it
          </Button>
        }
      >
        <Stack gap="sm">
          <Callout tone="warn" title="Copy it now">
            <Text>
              This is the only time the private key is shown. The server keeps only the public half,
              so a lost key cannot be recovered: make another.
            </Text>
          </Callout>
          <CodeBlock label="PEM" copy data-testid="signing-key-pem">
            {privateKey}
          </CodeBlock>
        </Stack>
      </Dialog>
    );
  }

  // Once the request is out the server may already have replaced the key, so the dialog stays until the
  // answer is here: closing it then would drop the only copy of the new private key.
  const closeUnlessSaving = () => {
    if (!saving) onClose();
  };
  return (
    <Dialog
      title={`Make a signing key for ${environment.name}?`}
      description="A new key replaces any old one at once, so every signature made with the old key stops working."
      size="sm"
      onClose={closeUnlessSaving}
      footer={
        <>
          <Button variant="ghost" disabled={saving} onClick={closeUnlessSaving}>
            Cancel
          </Button>
          <Button
            busy={saving}
            busyLabel="Making"
            data-testid="confirm-signing-key"
            onClick={() => void make()}
          >
            Make key
          </Button>
        </>
      }
    >
      {error !== null && (
        <p role="alert" data-testid="signing-key-error">
          {error}
        </p>
      )}
    </Dialog>
  );
}
