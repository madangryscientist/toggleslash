import {
  Button,
  Callout,
  EmptyState,
  PageHeader,
  Pill,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "@hackcove/ui-react";
import { useHydrated } from "@tanstack/react-router";
import { useState } from "react";
import { revokeKey } from "#/api/operator";
import type { ApiKey, CreatedKey, Environment, Project } from "#/api/types";
import { day } from "./text";
import { NewKeyDialog, NewKeySecret, RevokeDialog } from "./KeyDialogs";

interface TableProps {
  keys: ApiKey[];
  onRevoke: (key: ApiKey) => void;
}

function KeyTable({ keys, onRevoke }: TableProps) {
  // Disabled until hydrated: before React attaches the handlers a click would do nothing, and nothing would say so.
  const hydrated = useHydrated();
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
                  disabled={!hydrated}
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
  const hydrated = useHydrated();
  const startCreating = () => setCreating(true);

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
          <Button disabled={!hydrated} onClick={startCreating} data-testid="new-key-button">
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
          actions={
            <Button disabled={!hydrated} onClick={startCreating}>
              Create the first key
            </Button>
          }
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
