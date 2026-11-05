import {
  Button,
  Cluster,
  ClusterGrow,
  Dialog,
  EmptyState,
  Field,
  PageHeader,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeaderCell,
  TableRow,
  Text,
  Textarea,
} from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { saveSegment } from "#/api/operator";
import type { Environment, Project, Segment } from "#/api/types";
import { parseMembers, ruleCount } from "./text";

interface EditProps {
  project: Project;
  environment: Environment;
  segment: Segment;
  onClose: () => void;
  onSaved: () => Promise<void>;
}

interface Draft {
  included: string;
  excluded: string;
  rules: string;
}

interface FieldsProps {
  draft: Draft;
  onChange: (draft: Draft) => void;
  rulesError: string | undefined;
}

/** The three boxes, as text: contexts one per line, rules as JSON. */
function SegmentFields({ draft, onChange, rulesError }: FieldsProps) {
  return (
    <>
      <Cluster align="start">
        <ClusterGrow>
          <Field
            label="Always included"
            htmlFor="segment-included"
            hint="One context key per line."
          >
            <Textarea
              id="segment-included"
              rows={3}
              value={draft.included}
              onChange={(e) => onChange({ ...draft, included: e.currentTarget.value })}
            />
          </Field>
        </ClusterGrow>
        <ClusterGrow>
          <Field label="Always excluded" htmlFor="segment-excluded" hint="Wins over included.">
            <Textarea
              id="segment-excluded"
              rows={3}
              value={draft.excluded}
              onChange={(e) => onChange({ ...draft, excluded: e.currentTarget.value })}
            />
          </Field>
        </ClusterGrow>
      </Cluster>
      <Field
        label="Rules"
        htmlFor="segment-rules"
        hint="A JSON array, as docs/evaluation.md describes."
        error={rulesError}
      >
        <Textarea
          id="segment-rules"
          rows={5}
          value={draft.rules}
          onChange={(e) => onChange({ ...draft, rules: e.currentTarget.value })}
        />
      </Field>
    </>
  );
}

const CONFLICT =
  "Someone else changed this segment, so this was not saved. Your edits are still here: copy what you need, then reload the page to get the latest version.";

function EditSegmentDialog({ project, environment, segment, onClose, onSaved }: EditProps) {
  const [draft, setDraft] = useState<Draft>({
    included: segment.included.join("\n"),
    excluded: segment.excluded.join("\n"),
    rules: segment.rules,
  });
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const rulesError =
    ruleCount(draft.rules) === null ? "The rules must be a JSON array." : undefined;

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (rulesError !== undefined) {
      setError(rulesError);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await saveSegment({
        data: {
          project: project.key,
          env: environment.key,
          segment: segment.key,
          included: parseMembers(draft.included),
          excluded: parseMembers(draft.excluded),
          rules: draft.rules,
          version: segment.version,
        },
      });
      if (result.ok) await onSaved();
      else if (result.conflict === true) setError(CONFLICT);
      else setError(result.detail ?? result.failure.message);
    } catch {
      setError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog
      title={`Edit ${segment.key}`}
      description={`Saving replaces its members and rules in ${environment.name}.`}
      size="lg"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="segment-form"
            busy={saving}
            busyLabel="Saving"
            data-testid="save-segment"
          >
            Save segment
          </Button>
        </>
      }
    >
      <form id="segment-form" onSubmit={(e) => void submit(e)} noValidate>
        <Stack gap="sm">
          <SegmentFields draft={draft} onChange={setDraft} rulesError={rulesError} />
          {error !== null && (
            <p role="alert" data-testid="segment-error">
              {error}
            </p>
          )}
        </Stack>
      </form>
    </Dialog>
  );
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

interface Props {
  project: Project;
  environment: Environment;
  segments: Segment[];
  onChanged: () => Promise<void>;
}

/** The environment's segments, each editable. Creating and deleting one is the admin API's for now. */
export function SegmentsPanel({ project, environment, segments, onChanged }: Props) {
  const [editing, setEditing] = useState<Segment | null>(null);
  return (
    <Stack width="full">
      <PageHeader title="Segments" subtitle={`${project.name} · ${environment.name}`} />
      {segments.length === 0 ? (
        <EmptyState title="No segments yet">
          <Text>
            A segment is a reusable group of contexts that flag rules can name. None exists in{" "}
            {environment.name}; create one through the admin API.
          </Text>
        </EmptyState>
      ) : (
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
                  <Button
                    variant="outline"
                    size="sm"
                    aria-label={`Edit ${segment.key}`}
                    data-testid={`edit-${segment.key}`}
                    onClick={() => setEditing(segment)}
                  >
                    Edit
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
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
