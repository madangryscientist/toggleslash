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
import { deleteSegment } from "#/api/operator";
import type { Environment, Project, Segment } from "#/api/types";
import { EditSegmentDialog } from "./SegmentEditDialog";
import { DeleteSegmentDialog, NewSegmentDialog } from "./SegmentDialogs";
import { ruleCount } from "./text";
import { useRunner } from "./useRunner";

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

interface TableProps {
  project: Project;
  environment: Environment;
  segments: Segment[];
  onEdit: (segment: Segment) => void;
  onDelete: (segment: Segment) => void;
}

function SegmentTable({ project, environment, segments, onEdit, onDelete }: TableProps) {
  return (
    <Table caption={`Segments in ${project.name}, ${environment.name}`} data-testid="segments">
      <TableHead>
        <TableRow>
          <TableHeaderCell>Key</TableHeaderCell>
          <TableHeaderCell>Included</TableHeaderCell>
          <TableHeaderCell>Excluded</TableHeaderCell>
          <TableHeaderCell>Rules</TableHeaderCell>
          <TableHeaderCell srLabel="Actions" />
        </TableRow>
      </TableHead>
      <TableBody>
        {segments.map((segment) => (
          <TableRow key={segment.key} data-testid={`segment-${segment.key}`}>
            <TableCell tone="strong" mono>
              {segment.key}
            </TableCell>
            <TableCell>{plural(segment.included.length, "context", "contexts")}</TableCell>
            <TableCell>{plural(segment.excluded.length, "context", "contexts")}</TableCell>
            <TableCell>{plural(ruleCount(segment.rules) ?? 0, "rule", "rules")}</TableCell>
            <TableCell align="end">
              <Cluster justify="end">
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={`Edit ${segment.key}`}
                  data-testid={`edit-${segment.key}`}
                  onClick={() => onEdit(segment)}
                >
                  Edit
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  tone="danger"
                  aria-label={`Delete ${segment.key}`}
                  data-testid={`delete-${segment.key}`}
                  onClick={() => onDelete(segment)}
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

function NoSegments({ environment, onCreate }: { environment: Environment; onCreate: () => void }) {
  return (
    <EmptyState
      title="No segments yet"
      actions={<Button onClick={onCreate}>Create the first segment</Button>}
    >
      A segment is a reusable group of contexts that flag rules can name. None exists in{" "}
      {environment.name} yet.
    </EmptyState>
  );
}

interface Props {
  project: Project;
  environment: Environment;
  segments: Segment[];
  onChanged: () => Promise<void>;
}

/** The environment's segments: create, edit and delete. */
export function SegmentsPanel({ project, environment, segments, onChanged }: Props) {
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Segment | null>(null);
  const [deleting, setDeleting] = useState<Segment | null>(null);
  const { busy, problem, run, clear } = useRunner(onChanged);

  /** A new action clears the last failure. */
  function open(action: () => void) {
    clear();
    action();
  }

  async function remove(segment: Segment) {
    await run(segment.key, () =>
      deleteSegment({ data: { project: project.key, env: environment.key, segment: segment.key } }),
    );
    setDeleting(null);
  }
  const create = () => open(() => setCreating(true));

  return (
    <Stack width="full">
      <PageHeader
        title="Segments"
        subtitle={`${project.name} · ${environment.name}`}
        actions={
          <Button onClick={create} data-testid="new-segment">
            New segment
          </Button>
        }
      />
      {problem !== null && (
        <Callout tone="danger" title="That did not work" data-testid="problem">
          {problem}
        </Callout>
      )}
      {segments.length === 0 ? (
        <NoSegments environment={environment} onCreate={create} />
      ) : (
        <SegmentTable
          project={project}
          environment={environment}
          segments={segments}
          onEdit={(segment) => open(() => setEditing(segment))}
          onDelete={(segment) => open(() => setDeleting(segment))}
        />
      )}
      {creating && (
        <NewSegmentDialog
          project={project}
          environment={environment}
          onClose={() => setCreating(false)}
          onCreated={(segment) => {
            setCreating(false);
            setEditing(segment);
            void onChanged();
          }}
        />
      )}
      {deleting !== null && (
        <DeleteSegmentDialog
          segment={deleting}
          busy={busy !== null}
          onCancel={() => setDeleting(null)}
          onConfirm={(segment) => void remove(segment)}
        />
      )}
      {editing !== null && (
        <EditSegmentDialog
          project={project}
          environment={environment}
          segment={editing}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null);
            await onChanged();
          }}
        />
      )}
    </Stack>
  );
}
