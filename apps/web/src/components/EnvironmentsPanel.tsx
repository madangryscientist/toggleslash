import {
  Button,
  Dialog,
  EmptyState,
  Field,
  Input,
  PageHeader,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { createEnvironment } from "#/api/operator";
import type { ProjectReady } from "#/api/types";
import { NeedsEnvironment } from "./NeedsEnvironment";
import { day, KEY } from "./text";

interface DialogProps {
  project: string;
  onClose: () => void;
  onCreated: () => Promise<void>;
}

function NewEnvironmentDialog({ project, onClose, onCreated }: DialogProps) {
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

interface Props {
  loaded: ProjectReady;
  onChanged: () => Promise<void>;
}

/** The project's environments, and a way to add one. The server has no rename or delete yet. */
export function EnvironmentsPanel({ loaded, onChanged }: Props) {
  const { project, environments } = loaded;
  const [creating, setCreating] = useState(false);
  if (project === null) {
    return <NeedsEnvironment project={null} what="Environments" />;
  }
  return (
    <Stack width="full">
      <PageHeader
        title="Environments"
        subtitle={project.name}
        actions={
          <Button onClick={() => setCreating(true)} data-testid="new-environment">
            New environment
          </Button>
        }
      />
      {environments.length === 0 ? (
        <EmptyState
          title="No environments yet"
          actions={<Button onClick={() => setCreating(true)}>Create the first environment</Button>}
        >
          Flags are switched on and off per environment, so a project needs at least one.
        </EmptyState>
      ) : (
        <Table caption={`Environments in ${project.name}`} data-testid="environments">
          <TableHead>
            <TableRow>
              <TableHeaderCell>Name</TableHeaderCell>
              <TableHeaderCell>Key</TableHeaderCell>
              <TableHeaderCell>Created</TableHeaderCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {environments.map((env) => (
              <TableRow key={env.key} data-testid={`environment-${env.key}`}>
                <TableCell tone="strong">{env.name}</TableCell>
                <TableCell mono>{env.key}</TableCell>
                <TableCell tone="muted">{day(env.createdAt)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {creating && (
        <NewEnvironmentDialog
          project={project.key}
          onClose={() => setCreating(false)}
          onCreated={async () => {
            setCreating(false);
            await onChanged();
          }}
        />
      )}
    </Stack>
  );
}
