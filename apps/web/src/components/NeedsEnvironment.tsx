import { Callout, Text } from "@hackcove/ui-react";
import { Link } from "@tanstack/react-router";
import type { Project } from "#/api/types";

/** The empty state of a page that works inside one environment, when there is no project or no environment. */
export function NeedsEnvironment({ project, what }: { project: Project | null; what: string }) {
  if (project === null) {
    return (
      <Callout tone="info" title="No projects yet">
        <Text>
          Create one with the CLI: <code>toggleslash projects create</code>. The dashboard does not
          create projects yet.
        </Text>
      </Callout>
    );
  }
  return (
    <Callout tone="info" title={`${project.name} has no environments`}>
      <Text>
        {what} belong to an environment.{" "}
        <Link to="/environments" search={{ project: project.key }}>
          Create one first
        </Link>
        .
      </Text>
    </Callout>
  );
}
