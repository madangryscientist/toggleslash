import {
  Button,
  Callout,
  CopyLine,
  Dialog,
  Field,
  Input,
  Select,
  Stack,
  Text,
} from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { createKey } from "#/api/operator";
import type { ApiKey, CreatedKey, Environment, KeyKind, Project } from "#/api/types";

interface NewKeyProps {
  project: Project;
  environment: Environment;
  onClose: () => void;
  /** The key was made; this is the only time its plaintext is in the page. */
  onCreated: (key: CreatedKey) => void;
}

export function NewKeyDialog({ project, environment, onClose, onCreated }: NewKeyProps) {
  const [name, setName] = useState("");
  const [kind, setKind] = useState<KeyKind>("server");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (name.trim() === "") {
      setError("Give the key a name, so you can tell it from the others later.");
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await createKey({
        data: { project: project.key, env: environment.key, kind, name: name.trim() },
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
      title="New SDK key"
      description={`A key for ${project.name}, ${environment.name}. It is shown once, right after it is created.`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" form="new-key-form" busy={saving && "Creating"}>
            Create key
          </Button>
        </>
      }
    >
      <form id="new-key-form" onSubmit={(e) => void submit(e)} noValidate>
        <Stack gap="sm">
          <Field label="Name">
            <Input value={name} onChange={(e) => setName(e.currentTarget.value)} />
          </Field>
          <Field
            label="Kind"
            hint="A server key reads the ruleset and evaluates locally. A client key can only ask the server to evaluate."
          >
            <Select
              value={kind}
              onChange={(e) => setKind(e.currentTarget.value === "client" ? "client" : "server")}
              items={[
                { value: "server", label: "Server" },
                { value: "client", label: "Client" },
              ]}
            />
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

/** The new key's plaintext. It lives in this page's state only: leave, or dismiss it, and it is gone. */
export function NewKeySecret({
  created,
  onDismiss,
}: {
  created: CreatedKey;
  onDismiss: () => void;
}) {
  return (
    <Callout tone="warn" title={`Copy “${created.name}” now`} data-testid="new-key">
      <Stack gap="sm">
        <Text>
          This is the only time the key is shown. The server keeps a hash and the prefix{" "}
          <code>{created.prefix}</code>, so a lost key cannot be recovered: revoke it and create
          another.
        </Text>
        <CopyLine text={created.plaintext} data-testid="new-key-plaintext" />
        <div>
          <Button variant="outline" onClick={onDismiss} data-testid="dismiss-key">
            I have copied it
          </Button>
        </div>
      </Stack>
    </Callout>
  );
}

export function RevokeDialog({
  keyRow,
  busy,
  onCancel,
  onConfirm,
}: {
  keyRow: ApiKey;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (key: ApiKey) => void;
}) {
  return (
    <Dialog
      title={`Revoke ${keyRow.name}?`}
      description={`Anything using ${keyRow.prefix}… stops working at once. A revoked key cannot be restored.`}
      width="sm"
      onClose={onCancel}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            tone="danger"
            busy={busy && "Revoking"}
            data-testid="confirm-revoke"
            onClick={() => onConfirm(keyRow)}
          >
            Revoke
          </Button>
        </>
      }
    />
  );
}
