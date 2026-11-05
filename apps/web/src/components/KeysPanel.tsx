import {
  Button,
  Callout,
  CopyLine,
  Dialog,
  EmptyState,
  Field,
  Input,
  PageHeader,
  Pill,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeaderCell,
  TableRow,
  Text,
} from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { createKey, revokeKey } from "#/api/operator";
import type { ApiKey, CreatedKey, Environment, KeyKind, Project } from "#/api/types";
import { day } from "./text";

interface NewKeyProps {
  project: Project;
  environment: Environment;
  onClose: () => void;
  /** The key was made; this is the only time its plaintext is in the page. */
  onCreated: (key: CreatedKey) => void;
}

function NewKeyDialog({ project, environment, onClose, onCreated }: NewKeyProps) {
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
          <Button type="submit" form="new-key-form" busy={saving} busyLabel="Creating">
            Create key
          </Button>
        </>
      }
    >
      <form id="new-key-form" onSubmit={(e) => void submit(e)} noValidate>
        <Stack gap="sm">
          <Field label="Name" htmlFor="key-name">
            <Input id="key-name" value={name} onChange={(e) => setName(e.currentTarget.value)} />
          </Field>
          <Field
            label="Kind"
            htmlFor="key-kind"
            hint="A server key reads the ruleset and evaluates locally. A client key can only ask the server to evaluate."
          >
            <Select
              id="key-kind"
              value={kind}
              onChange={(e) => setKind(e.currentTarget.value === "client" ? "client" : "server")}
            >
              <option value="server">Server</option>
              <option value="client">Client</option>
            </Select>
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
function NewKeySecret({ created, onDismiss }: { created: CreatedKey; onDismiss: () => void }) {
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

function RevokeDialog({
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
            busyLabel="Revoking"
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

interface TableProps {
  keys: ApiKey[];
  onRevoke: (key: ApiKey) => void;
}

function KeyTable({ keys, onRevoke }: TableProps) {
  return (
    <Table caption="SDK keys" data-testid="keys">
      <TableHead>
        <TableRow>
          <TableHeaderCell>Name</TableHeaderCell>
          <TableHeaderCell>Kind</TableHeaderCell>
          <TableHeaderCell>Prefix</TableHeaderCell>
          <TableHeaderCell>Created</TableHeaderCell>
          <TableHeaderCell>Status</TableHeaderCell>
          <TableHeaderCell srLabel="Actions" />
        </TableRow>
      </TableHead>
      <TableBody>
        {keys.map((key) => (
          <TableRow key={key.id} data-testid={`key-${key.id}`}>
            <TableCell tone="strong">{key.name}</TableCell>
            <TableCell>
              <Pill tone={key.kind === "server" ? "slate" : "blue"}>{key.kind}</Pill>
            </TableCell>
            <TableCell mono>{key.prefix}</TableCell>
            <TableCell tone="muted">{day(key.createdAt)}</TableCell>
            <TableCell>
              {key.revokedAt === null ? (
                <Pill tone="emerald">active</Pill>
              ) : (
                <Pill tone="amber">revoked {day(key.revokedAt)}</Pill>
              )}
            </TableCell>
            <TableCell align="end">
              {key.revokedAt === null && (
                <Button
                  variant="ghost"
                  size="sm"
                  tone="danger"
                  aria-label={`Revoke ${key.name}`}
                  data-testid={`revoke-${key.id}`}
                  onClick={() => onRevoke(key)}
                >
                  Revoke
                </Button>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

interface Props {
  project: Project;
  environment: Environment;
  keys: ApiKey[];
  onChanged: () => Promise<void>;
}

export function KeysPanel({ project, environment, keys, onChanged }: Props) {
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedKey | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  async function revoke(key: ApiKey) {
    setBusy(true);
    setProblem(null);
    try {
      const result = await revokeKey({
        data: { project: project.key, env: environment.key, id: key.id },
      });
      if (!result.ok) setProblem(result.failure.message);
      await onChanged();
    } catch {
      setProblem("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setBusy(false);
      setRevoking(null);
    }
  }

  return (
    <Stack width="full">
      <PageHeader
        title="SDK keys"
        subtitle={`${project.name} · ${environment.name}`}
        actions={
          <Button onClick={() => setCreating(true)} data-testid="new-key-button">
            New key
          </Button>
        }
      />
      {created !== null && <NewKeySecret created={created} onDismiss={() => setCreated(null)} />}
      {problem !== null && (
        <Callout tone="danger" title="That did not work" data-testid="problem">
          {problem}
        </Callout>
      )}
      {keys.length === 0 ? (
        <EmptyState
          title="No keys yet"
          actions={<Button onClick={() => setCreating(true)}>Create the first key</Button>}
        >
          An SDK needs a key to read {environment.name}. Create a server key for your backend.
        </EmptyState>
      ) : (
        <KeyTable keys={keys} onRevoke={setRevoking} />
      )}
      {creating && (
        <NewKeyDialog
          project={project}
          environment={environment}
          onClose={() => setCreating(false)}
          onCreated={(key) => {
            setCreating(false);
            setCreated(key);
            void onChanged();
          }}
        />
      )}
      {revoking !== null && (
        <RevokeDialog
          keyRow={revoking}
          busy={busy}
          onCancel={() => setRevoking(null)}
          onConfirm={(key) => void revoke(key)}
        />
      )}
    </Stack>
  );
}
