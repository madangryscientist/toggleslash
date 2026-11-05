import {
  Button,
  Callout,
  Cluster,
  ClusterGrow,
  Dialog,
  EmptyState,
  PageHeader,
  Input,
  Pill,
  Stack,
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
import { archiveFlag, toggleFlag } from "#/api/dashboard";
import type { Environment, FlagRow, MutationResult, Project } from "#/api/types";
import { NewFlagDialog } from "./NewFlagDialog";
import { matches } from "./text";

interface Props {
  project: Project;
  environment: Environment;
  flags: FlagRow[];
  onChanged: () => Promise<void>;
}

type Run = (flag: string, action: () => Promise<MutationResult>) => Promise<void>;

interface RowProps {
  project: Project;
  environment: Environment;
  flag: FlagRow;
  busy: string | null;
  run: Run;
  onArchive: (flag: FlagRow) => void;
}

function ToggleCell({ project, environment, flag, busy, run }: Omit<RowProps, "onArchive">) {
  if (flag.on === null) return <Text tone="muted">unknown</Text>;
  return (
    <Button
      variant="outline"
      size="sm"
      tone={flag.on ? "positive" : undefined}
      pressed={flag.on}
      busy={busy === flag.key}
      busyLabel="Saving"
      aria-label={`${flag.name} in ${environment.name}`}
      data-testid={`toggle-${flag.key}`}
      onClick={() =>
        void run(flag.key, () =>
          toggleFlag({
            data: {
              project: project.key,
              env: environment.key,
              flag: flag.key,
              on: !flag.on,
              version: flag.version,
            },
          }),
        )
      }
    >
      {flag.on ? "On" : "Off"}
    </Button>
  );
}

function FlagRowView(props: RowProps) {
  const { flag, busy, onArchive } = props;
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
              search={{ project: props.project.key, env: props.environment.key }}
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
      <TableCell>
        <Cluster wrap>
          {flag.temporary && <Pill tone="amber">temporary</Pill>}
          {flag.tags.map((tag) => (
            <Pill key={tag} tone="slate">
              {tag}
            </Pill>
          ))}
        </Cluster>
      </TableCell>
      <TableCell>
        <ToggleCell {...props} />
      </TableCell>
      <TableCell tone="muted">{flag.lastEvaluated ?? "never"}</TableCell>
      <TableCell align="end">
        <Button
          variant="ghost"
          size="sm"
          tone="danger"
          aria-label={`Archive ${flag.name}`}
          disabled={busy !== null}
          onClick={() => onArchive(flag)}
        >
          Archive
        </Button>
      </TableCell>
    </TableRow>
  );
}

interface TableProps extends Omit<RowProps, "flag"> {
  flags: FlagRow[];
}

function FlagTable({ flags, ...row }: TableProps) {
  const { project, environment } = row;
  return (
    <Table caption={`Flags in ${project.name}, ${environment.name}`}>
      <TableHead>
        <TableRow>
          <TableHeaderCell>Flag</TableHeaderCell>
          <TableHeaderCell>Tags</TableHeaderCell>
          <TableHeaderCell>{environment.name}</TableHeaderCell>
          <TableHeaderCell>Last evaluated</TableHeaderCell>
          <TableHeaderCell srLabel="Actions" />
        </TableRow>
      </TableHead>
      <TableBody>
        {flags.map((flag) => (
          <FlagRowView key={flag.key} flag={flag} {...row} />
        ))}
      </TableBody>
    </Table>
  );
}

function count(shown: number, total: number): string {
  if (shown === total) return `${total} ${total === 1 ? "flag" : "flags"}`;
  return `${shown} of ${total}`;
}

interface ListProps extends TableProps {
  total: number;
  query: string;
  onCreate: () => void;
}

/** The empty states, or the table. */
function Results({ total, query, onCreate, flags, ...row }: ListProps) {
  const { project, environment } = row;
  if (total === 0) {
    return (
      <EmptyState
        title="No flags yet"
        actions={<Button onClick={onCreate}>Create the first flag</Button>}
      >
        Flags you create in {project.name} appear here, with their state in {environment.name}.
      </EmptyState>
    );
  }
  if (flags.length === 0) {
    return (
      <EmptyState title="No flags match">
        Nothing in {project.name} matches “{query}”.
      </EmptyState>
    );
  }
  return <FlagTable flags={flags} {...row} />;
}

interface ArchiveProps {
  flag: FlagRow;
  onCancel: () => void;
  onConfirm: (flag: FlagRow) => void;
}

function ArchiveDialog({ flag, onCancel, onConfirm }: ArchiveProps) {
  return (
    <Dialog
      title={`Archive ${flag.name}?`}
      description="An archived flag is hidden from this list and from the SDK ruleset. Its key stays reserved."
      size="sm"
      onClose={onCancel}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button tone="danger" data-testid="confirm-archive" onClick={() => onConfirm(flag)}>
            Archive
          </Button>
        </>
      }
    />
  );
}

/** The busy flag, the last problem, and a `run` that reports a failed or refused call as that problem. */
function useRunner(onChanged: () => Promise<void>) {
  const [busy, setBusy] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const run: Run = async (flag, action) => {
    setBusy(flag);
    setProblem(null);
    try {
      const result = await action();
      if (!result.ok) setProblem(result.failure.message);
      // Refresh on success, and on a conflict so the row shows what the server now has.
      if (result.ok || result.conflict) await onChanged();
    } catch {
      setProblem("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setBusy(null);
    }
  };
  return { busy, problem, run };
}

export function FlagList({ project, environment, flags, onChanged }: Props) {
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);
  const [archiving, setArchiving] = useState<FlagRow | null>(null);
  const { busy, problem, run } = useRunner(onChanged);

  const shown = flags.filter((flag) => matches(flag, query));

  return (
    <Stack width="full">
      <PageHeader
        title="Flags"
        subtitle={`${project.name} · ${environment.name}`}
        actions={
          <Button onClick={() => setCreating(true)} data-testid="new-flag">
            New flag
          </Button>
        }
      />
      {problem !== null && (
        <Callout tone="danger" title="That did not work" data-testid="problem">
          {problem}
        </Callout>
      )}
      <Cluster>
        <ClusterGrow>
          <Input
            type="search"
            aria-label="Search flags"
            placeholder="Search by key, name or tag"
            value={query}
            onChange={(event) => setQuery(event.currentTarget.value)}
          />
        </ClusterGrow>
        <Text tone="muted" numeric>
          {count(shown.length, flags.length)}
        </Text>
      </Cluster>
      <Results
        project={project}
        environment={environment}
        flags={shown}
        total={flags.length}
        query={query}
        busy={busy}
        run={run}
        onArchive={setArchiving}
        onCreate={() => setCreating(true)}
      />
      {creating && (
        <NewFlagDialog
          project={project}
          onClose={() => setCreating(false)}
          onCreated={async () => {
            setCreating(false);
            await onChanged();
          }}
        />
      )}
      {archiving !== null && (
        <ArchiveDialog
          flag={archiving}
          onCancel={() => setArchiving(null)}
          onConfirm={(flag) => {
            setArchiving(null);
            void run(flag.key, () =>
              archiveFlag({ data: { project: project.key, flag: flag.key } }),
            );
          }}
        />
      )}
    </Stack>
  );
}
