import {
  Button,
  Callout,
  Cluster,
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
import { useState } from "react";
import { deleteEnvironment } from "#/api/environments";
import type { Environment, ProjectReady } from "#/api/types";
import {
  DeleteEnvironmentDialog,
  NewEnvironmentDialog,
  RenameEnvironmentDialog,
} from "./EnvironmentDialogs";
import { NeedsEnvironment } from "./NeedsEnvironment";
import { day } from "./text";
import { useRunner } from "./useRunner";

interface TableProps {
  projectName: string;
  environments: Environment[];
  onRename: (environment: Environment) => void;
  onDelete: (environment: Environment) => void;
}

function EnvironmentTable({ projectName, environments, onRename, onDelete }: TableProps) {
  return (
    <Table caption={`Environments in ${projectName}`} data-testid="environments">
      <TableHead>
        <TableRow>
          <TableHeaderCell>Name</TableHeaderCell>
          <TableHeaderCell>Key</TableHeaderCell>
          <TableHeaderCell>Created</TableHeaderCell>
          <TableHeaderCell srLabel="Actions" />
        </TableRow>
      </TableHead>
      <TableBody>
        {environments.map((env) => (
          <TableRow key={env.key} data-testid={`environment-${env.key}`}>
            <TableCell tone="strong">{env.name}</TableCell>
            <TableCell mono>{env.key}</TableCell>
            <TableCell tone="muted">{day(env.createdAt)}</TableCell>
            <TableCell align="end">
              <Cluster justify="end">
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Rename ${env.key}`}
                  data-testid={`rename-${env.key}`}
                  onClick={() => onRename(env)}
                >
                  Rename
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  tone="danger"
                  aria-label={`Delete ${env.key}`}
                  data-testid={`delete-${env.key}`}
                  onClick={() => onDelete(env)}
                >
                  Delete
                </Button>
              </Cluster>
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
  | { kind: "delete"; environment: Environment };

interface DialogsProps {
  project: string;
  dialog: Dialog | null;
  busy: boolean;
  onClose: () => void;
  /** A create or rename went through: close and refresh. */
  onChanged: () => Promise<void>;
  onConfirmDelete: (environment: Environment) => void;
}

/** The one dialog that is open, if any. */
function Dialogs({ project, dialog, busy, onClose, onChanged, onConfirmDelete }: DialogsProps) {
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

/** The project's environments: create, rename and delete. The server refuses a delete while a key is not revoked. */
export function EnvironmentsPanel({ loaded, onChanged }: Props) {
  const { project, environments } = loaded;
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const { busy, problem, run, clear } = useRunner(onChanged);
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

  return (
    <Stack width="full">
      <PageHeader
        title="Environments"
        subtitle={project.name}
        actions={
          <Button onClick={create} data-testid="new-environment">
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
          actions={<Button onClick={create}>Create the first environment</Button>}
        >
          Flags are switched on and off per environment, so a project needs at least one.
        </EmptyState>
      ) : (
        <EnvironmentTable
          projectName={project.name}
          environments={environments}
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
        onConfirmDelete={(env) => void remove(env, project.key)}
      />
    </Stack>
  );
}
