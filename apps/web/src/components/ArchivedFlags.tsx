import {
  Button,
  Cluster,
  Dialog,
  EmptyState,
  Pill,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeaderCell,
  TableRow,
  Text,
  TextLink,
} from "@hackcove/ui-react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { deleteFlag, restoreFlag } from "#/api/dashboard";
import type { Environment, FlagRow, Project } from "#/api/types";
import { StagePill } from "./StagePill";
import type { Run } from "./useRunner";

interface DeleteProps {
  flag: FlagRow;
  onCancel: () => void;
  onConfirm: (flag: FlagRow) => void;
}

function DeleteDialog({ flag, onCancel, onConfirm }: DeleteProps) {
  return (
    <Dialog
      title={`Delete ${flag.name}?`}
      description="This removes the flag and its targeting in every environment. Its audit history stays, and the key can be used again."
      size="sm"
      onClose={onCancel}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button tone="danger" data-testid="confirm-delete" onClick={() => onConfirm(flag)}>
            Delete
          </Button>
        </>
      }
    />
  );
}

interface RowProps {
  project: Project;
  environment: Environment;
  flag: FlagRow;
  busy: string | null;
  run: Run;
  onDelete: (flag: FlagRow) => void;
}

function ArchivedRow({ project, environment, flag, busy, run, onDelete }: RowProps) {
  return (
    <TableRow data-testid={`flag-${flag.key}`}>
      <TableCell
        tone="strong"
        secondary={
          <Text mono tone="muted">
            {flag.key}
          </Text>
        }
      >
        <TextLink
          render={({ className, children }) => (
            <Link
              to="/flags/$flag"
              params={{ flag: flag.key }}
              search={{ project: project.key, env: environment.key }}
              className={className}
              data-testid={`open-${flag.key}`}
            >
              {children}
            </Link>
          )}
        >
          {flag.name}
        </TextLink>
      </TableCell>
      <TableCell>{flag.lifecycle !== null && <StagePill stage={flag.lifecycle} />}</TableCell>
      <TableCell>
        <Cluster wrap>
          {flag.tags.map((tag) => (
            <Pill key={tag} tone="slate">
              {tag}
            </Pill>
          ))}
        </Cluster>
      </TableCell>
      <TableCell align="end">
        <Cluster justify="end">
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Restore ${flag.name}`}
            data-testid="restore-flag"
            disabled={busy !== null}
            onClick={() =>
              void run(flag.key, () =>
                restoreFlag({ data: { project: project.key, flag: flag.key } }),
              )
            }
          >
            Restore
          </Button>
          <Button
            variant="ghost"
            size="sm"
            tone="danger"
            aria-label={`Delete ${flag.name}`}
            data-testid="delete-flag"
            disabled={busy !== null}
            onClick={() => onDelete(flag)}
          >
            Delete
          </Button>
        </Cluster>
      </TableCell>
    </TableRow>
  );
}

interface Props {
  project: Project;
  environment: Environment;
  /** The archived flags the search leaves. */
  flags: FlagRow[];
  total: number;
  query: string;
  busy: string | null;
  run: Run;
}

/** The archived view: its table with restore and delete, the delete confirmation, and the empty states. */
export function ArchivedFlags({ project, environment, flags, total, query, busy, run }: Props) {
  const [deleting, setDeleting] = useState<FlagRow | null>(null);
  if (total === 0) {
    return <EmptyState title="No archived flags">No archived flags in {project.name}.</EmptyState>;
  }
  if (flags.length === 0) {
    return (
      <EmptyState title="No flags match">
        Nothing archived in {project.name} matches “{query}”.
      </EmptyState>
    );
  }
  return (
    <>
      <Table caption={`Archived flags in ${project.name}`}>
        <TableHead>
          <TableRow>
            <TableHeaderCell>Flag</TableHeaderCell>
            <TableHeaderCell>Stage</TableHeaderCell>
            <TableHeaderCell>Tags</TableHeaderCell>
            <TableHeaderCell srLabel="Actions" />
          </TableRow>
        </TableHead>
        <TableBody>
          {flags.map((flag) => (
            <ArchivedRow
              key={flag.key}
              project={project}
              environment={environment}
              flag={flag}
              busy={busy}
              run={run}
              onDelete={setDeleting}
            />
          ))}
        </TableBody>
      </Table>
      {deleting !== null && (
        <DeleteDialog
          flag={deleting}
          onCancel={() => setDeleting(null)}
          onConfirm={(flag) => {
            setDeleting(null);
            void run(flag.key, () =>
              deleteFlag({ data: { project: project.key, flag: flag.key } }),
            );
          }}
        />
      )}
    </>
  );
}
