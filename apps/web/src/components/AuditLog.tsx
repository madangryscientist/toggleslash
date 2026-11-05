import {
  Button,
  Cluster,
  EmptyState,
  Field,
  Input,
  PageHeader,
  Select,
  Stack,
  Text,
  Timeline,
  TimelineItem,
} from "@hackcove/ui-react";
import { Link } from "@tanstack/react-router";
import { useState, type FormEvent } from "react";
import type { AuditFilters, AuditPage, Environment, Project } from "#/api/types";
import { describeEvent, nonEmpty, stamp } from "./text";

interface FilterProps {
  environments: Environment[];
  filters: AuditFilters;
  onApply: (filters: AuditFilters) => void;
}

/** The three narrowings the server supports. Applying them restarts the log from its newest event. */
function Filters({ environments, filters, onApply }: FilterProps) {
  const [draft, setDraft] = useState(filters);
  function submit(event: FormEvent) {
    event.preventDefault();
    onApply({ ...draft, flag: draft.flag.trim() });
  }
  const filtered = filters.flag !== "" || filters.environment !== "" || filters.since !== "";
  return (
    <form onSubmit={submit} aria-label="Filter the audit log" data-testid="audit-filters">
      <Cluster wrap align="end">
        <Field label="Flag" htmlFor="audit-flag">
          <Input
            id="audit-flag"
            placeholder="Flag key"
            value={draft.flag}
            onChange={(e) => setDraft({ ...draft, flag: e.currentTarget.value })}
          />
        </Field>
        <Field label="Environment" htmlFor="audit-environment">
          <Select
            id="audit-environment"
            value={draft.environment}
            onChange={(e) => setDraft({ ...draft, environment: e.currentTarget.value })}
          >
            <option value="">Any environment</option>
            {environments.map((env) => (
              <option key={env.key} value={env.key}>
                {env.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Since" htmlFor="audit-since">
          <Input
            id="audit-since"
            type="date"
            value={draft.since}
            onChange={(e) => setDraft({ ...draft, since: e.currentTarget.value })}
          />
        </Field>
        <Button type="submit" data-testid="apply-filters">
          Apply filters
        </Button>
        {filtered && (
          <Button
            variant="ghost"
            data-testid="clear-filters"
            onClick={() => {
              const none = { flag: "", environment: "", since: "" };
              setDraft(none);
              onApply(none);
            }}
          >
            Clear
          </Button>
        )}
      </Cluster>
    </form>
  );
}

interface Props {
  project: Project;
  env: string | undefined;
  environments: Environment[];
  filters: AuditFilters;
  cursor: string | null;
  page: AuditPage;
  onApply: (filters: AuditFilters) => void;
}

/** The project's audit events, newest first, a page at a time. */
export function AuditLog({ project, env, environments, filters, cursor, page, onApply }: Props) {
  return (
    <Stack width="full">
      <PageHeader
        title="Audit log"
        subtitle={`${project.name}: every change made through the admin API, newest first`}
      />
      <Filters
        // Applying filters (or clearing them) remounts the form on what was applied.
        key={`${filters.flag}|${filters.environment}|${filters.since}`}
        environments={environments}
        filters={filters}
        onApply={onApply}
      />
      {page.events.length === 0 ? (
        <EmptyState title="No events">
          {cursor === null &&
          filters.flag === "" &&
          filters.environment === "" &&
          filters.since === ""
            ? `Nothing has been changed in ${project.name} yet.`
            : "No event matches these filters."}
        </EmptyState>
      ) : (
        <Timeline label="Audit log" data-testid="audit-events">
          {page.events.map((event) => (
            <TimelineItem
              key={event.id}
              title={describeEvent(event)}
              time={`${stamp(event.at)}, ${event.actor}`}
              data-testid="audit-event"
            />
          ))}
        </Timeline>
      )}
      <Cluster>
        {cursor !== null && (
          <Link
            to="/audit"
            search={{ project: project.key, env, ...nonEmpty(filters) }}
            data-testid="audit-newest"
          >
            Back to the newest
          </Link>
        )}
        {page.nextCursor !== null && (
          <Link
            to="/audit"
            search={{ project: project.key, env, ...nonEmpty(filters), cursor: page.nextCursor }}
            data-testid="audit-older"
          >
            Older events
          </Link>
        )}
        <Text tone="muted" numeric>
          {page.events.length} shown
        </Text>
      </Cluster>
    </Stack>
  );
}
