import {
  Button,
  Callout,
  EmptyState,
  PageHeader,
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
import { deleteEnvironment, setSecureMode } from "#/api/environments";
import type { Environment, ProjectReady } from "#/api/types";
import { RowActions, SecureModeSwitch } from "./EnvironmentRow";
import {
  DeleteEnvironmentDialog,
  NewEnvironmentDialog,
  RenameEnvironmentDialog,
  SigningKeyDialog,
} from "./EnvironmentDialogs";
import { NeedsEnvironment } from "./NeedsEnvironment";
import { day } from "./text";
import { useRunner } from "./useRunner";

interface TableProps {
  projectName: string;
  environments: Environment[];
  /** The key of the environment whose secure mode is being saved, if any. */
  saving: string | null;
  onSecureMode: (environment: Environment, on: boolean) => void;
  onSigningKey: (environment: Environment) => void;
  onRename: (environment: Environment) => void;
  onDelete: (environment: Environment) => void;
}

function EnvironmentTable({
  projectName,
  environments,
  saving,
  onSecureMode,
  onSigningKey,
  onRename,
  onDelete,
}: TableProps) {
  return (
    <Table label={`Environments in ${projectName}`} data-testid="environments">
      <TableHead>
        <TableRow>
          <TableHeaderCell>Name</TableHeaderCell>
          <TableHeaderCell>Key</TableHeaderCell>
          <TableHeaderCell>Created</TableHeaderCell>
          <TableHeaderCell>Secure mode</TableHeaderCell>
          <TableHeaderCell>Signing key</TableHeaderCell>
          <TableHeaderCell label="Actions" />
        </TableRow>
      </TableHead>
      <TableBody>
        {environments.map((env) => (
          <TableRow key={env.key} data-testid={`environment-${env.key}`}>
            <TableCell tone="strong">{env.name}</TableCell>
            <TableCell mono>{env.key}</TableCell>
            <TableCell tone="muted">{day(env.createdAt)}</TableCell>
            <TableCell>
              <SecureModeSwitch
                env={env}
                saving={saving === env.key}
                onChange={(on) => onSecureMode(env, on)}
              />
            </TableCell>
            <TableCell tone="muted" data-testid={`signing-key-date-${env.key}`}>
              {env.signingKeyCreatedAt === null
                ? "No signing key"
                : `Made ${day(env.signingKeyCreatedAt)}`}
            </TableCell>
            <TableCell align="end">
              <RowActions
                env={env}
                onSigningKey={onSigningKey}
                onRename={onRename}
                onDelete={onDelete}
              />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

type Dialog =
  | { kind: "new" }
  | { kind: "rename"; environment: Environment }
  | { kind: "signing-key"; environment: Environment }
  | { kind: "delete"; environment: Environment };

interface DialogsProps {
  project: string;
  dialog: Dialog | null;
  busy: boolean;
  onClose: () => void;
  /** A create or rename went through: close and refresh. */
  onChanged: () => Promise<void>;
  /** Refresh and keep the dialog open: a signing key was made and is still on show. */
  onRefresh: () => Promise<void>;
  onConfirmDelete: (environment: Environment) => void;
}

/** The one dialog that is open, if any. */
function Dialogs({
  project,
  dialog,
  busy,
  onClose,
  onChanged,
  onRefresh,
  onConfirmDelete,
}: DialogsProps) {
  if (dialog === null) return null;
  if (dialog.kind === "new") {
    return <NewEnvironmentDialog project={project} onClose={onClose} onCreated={onChanged} />;
  }
  if (dialog.kind === "rename") {
    return (
      <RenameEnvironmentDialog
        project={project}
        environment={dialog.environment}
        onClose={onClose}
        onChanged={onChanged}
      />
    );
  }
  if (dialog.kind === "signing-key") {
    return (
      <SigningKeyDialog
        project={project}
        environment={dialog.environment}
        onClose={onClose}
        onMade={onRefresh}
      />
    );
  }
  return (
    <DeleteEnvironmentDialog
      environment={dialog.environment}
      busy={busy}
      onCancel={onClose}
      onConfirm={onConfirmDelete}
    />
  );
}

interface Props {
  loaded: ProjectReady;
  onChanged: () => Promise<void>;
}

/**
 * The project's environments: create, rename, delete, a signing key and secure mode. The server refuses a
 * delete while a key is not revoked, and secure mode on while there is no signing key.
 */
export function EnvironmentsPanel({ loaded, onChanged }: Props) {
  const { project, environments } = loaded;
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const { busy, problem, run, clear } = useRunner(onChanged);
  // Disabled until hydrated: before React attaches the handlers a click would do nothing, and nothing would say so.
  const hydrated = useHydrated();
  if (project === null) {
    return <NeedsEnvironment project={null} what="Environments" />;
  }

  /** A new action clears the last failure. */
  function open(action: () => void) {
    clear();
    action();
  }

  async function remove(environment: Environment, projectKey: string) {
    await run(environment.key, () =>
      deleteEnvironment({ data: { project: projectKey, env: environment.key } }),
    );
    setDialog(null);
  }
  const create = () => open(() => setDialog({ kind: "new" }));
  function secureMode(environment: Environment, on: boolean, projectKey: string) {
    clear();
    void run(environment.key, () =>
      setSecureMode({ data: { project: projectKey, env: environment.key, secureMode: on } }),
    );
  }

  return (
    <Stack width="full">
      <PageHeader
        title="Environments"
        description={project.name}
        actions={
          <Button disabled={!hydrated} onClick={create} data-testid="new-environment">
            New environment
          </Button>
        }
      />
      {problem !== null && (
        <Callout tone="danger" title="That did not work" data-testid="problem">
          {problem}
        </Callout>
      )}
      {environments.length === 0 ? (
        <EmptyState
          title="No environments yet"
          actions={
            <Button disabled={!hydrated} onClick={create}>
              Create the first environment
            </Button>
          }
        >
          Flags are switched on and off per environment, so a project needs at least one.
        </EmptyState>
      ) : (
        <EnvironmentTable
          projectName={project.name}
          environments={environments}
          saving={busy}
          onSecureMode={(env, on) => secureMode(env, on, project.key)}
          onSigningKey={(env) => open(() => setDialog({ kind: "signing-key", environment: env }))}
          onRename={(env) => open(() => setDialog({ kind: "rename", environment: env }))}
          onDelete={(env) => open(() => setDialog({ kind: "delete", environment: env }))}
        />
      )}
      <Dialogs
        project={project.key}
        dialog={dialog}
        busy={busy !== null}
        onClose={() => setDialog(null)}
        onChanged={async () => {
          setDialog(null);
          await onChanged();
        }}
        onRefresh={onChanged}
        onConfirmDelete={(env) => void remove(env, project.key)}
      />
    </Stack>
  );
}
