import {
  Section,
  StatTile,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeaderCell,
  TableRow,
  Text,
  Timeline,
  TimelineItem,
} from "@hackcove/ui-react";
import type { FlagDetail, HistoryEvent, Usage } from "#/api/types";

function describe(event: HistoryEvent): string {
  const where = event.environment === null ? "" : ` in ${event.environment}`;
  const version = event.version === null ? "" : ` (version ${event.version})`;
  return `${event.action} ${event.entityType}${where}${version}`;
}

export function History({ events }: { events: HistoryEvent[] }) {
  return (
    <Section title="History">
      <Timeline
        label="History of this flag"
        data-testid="history"
        empty={<Text tone="muted">Nothing has been recorded for this flag yet.</Text>}
      >
        {events.map((event) => (
          <TimelineItem
            key={event.id}
            title={describe(event)}
            time={`${event.at.replace("T", " ").replace(/\..*$|Z$/, "")} UTC, ${event.actor}`}
            data-testid="history-event"
          />
        ))}
      </Timeline>
    </Section>
  );
}

/** Evaluation counts per day, newest first, for the chosen environment (or every one when none is chosen). */
export function UsagePanel({
  usage,
  flag,
  environment,
}: {
  usage: Usage;
  flag: FlagDetail;
  environment: string | null;
}) {
  const shown = usage.environments.filter(
    (e) => environment === null || e.environment === environment,
  );
  return (
    <Section title="Usage">
      {shown.length === 0 && <Text tone="muted">No evaluations have been counted yet.</Text>}
      {shown.map((env) => (
        <div key={env.environment} data-testid={`usage-${env.environment}`}>
          <StatTile
            label={`Evaluations in ${env.environment}`}
            value={env.total}
            hint="Counts only: no context or end-user data is stored."
            data-testid={`usage-total-${env.environment}`}
          />
          <Table caption={`Daily evaluations in ${env.environment}`}>
            <TableHead>
              <tr>
                <TableHeaderCell>Day</TableHeaderCell>
                <TableHeaderCell align="end">Evaluations</TableHeaderCell>
                <TableHeaderCell>By variation</TableHeaderCell>
              </tr>
            </TableHead>
            <TableBody>
              {env.days.map((day) => (
                <TableRow key={day.day}>
                  <TableCell mono>{day.day}</TableCell>
                  <TableCell numeric align="end">
                    {day.count}
                  </TableCell>
                  <TableCell>
                    {day.variations
                      .map(
                        (v) => `${flag.variations[v.variation] ?? `#${v.variation}`}: ${v.count}`,
                      )
                      .join(", ")}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ))}
    </Section>
  );
}
