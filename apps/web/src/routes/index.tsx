import { Callout, Text } from "@hackcove/ui-react";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { loadDashboard } from "#/api/dashboard";
import type { Loaded } from "#/api/types";
import { FlagList } from "#/components/FlagList";
import { ServerDown, Shell, Switchers } from "#/components/Shell";

interface Search {
  project?: string | undefined;
  env?: string | undefined;
}

function param(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

export const Route = createFileRoute("/")({
  validateSearch: (raw: Record<string, unknown>): Search => ({
    project: param(raw["project"]),
    env: param(raw["env"]),
  }),
  loaderDeps: ({ search }) => ({ project: search.project, env: search.env }),
  loader: ({ deps }) => loadDashboard({ data: deps }),
  component: Dashboard,
});

type Ready = Extract<Loaded, { ok: true }>;

function DashboardSwitchers({ loaded }: { loaded: Ready }) {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const { project } = loaded;
  return (
    <Switchers
      {...loaded}
      onProject={(key) => void navigate({ search: { project: key, env: undefined } })}
      onEnvironment={(key) =>
        void navigate({ search: { project: search.project ?? project?.key, env: key } })
      }
    />
  );
}

function Body({ loaded }: { loaded: Ready }) {
  const router = useRouter();
  const { project, environment, flags } = loaded;
  if (project === null) {
    return (
      <Callout tone="info" title="No projects yet">
        <Text>
          Create one with the CLI: <code>featureslash projects create</code>. The dashboard does not
          create projects yet.
        </Text>
      </Callout>
    );
  }
  if (environment === null) {
    return (
      <Callout tone="info" title={`${project.name} has no environments`}>
        <Text>
          Create one with the CLI: <code>featureslash envs create</code>. Flags are switched on and
          off per environment.
        </Text>
      </Callout>
    );
  }
  return (
    <FlagList
      // A new project or environment starts with a fresh search box and no open dialog.
      key={`${project.key}/${environment.key}`}
      project={project}
      environment={environment}
      flags={flags}
      onChanged={() => router.invalidate()}
    />
  );
}

function Dashboard() {
  const loaded = Route.useLoaderData();
  const router = useRouter();

  if (!loaded.ok) {
    return <ServerDown failure={loaded.failure} onRetry={() => void router.invalidate()} />;
  }

  return (
    <Shell controls={<DashboardSwitchers loaded={loaded} />}>
      <Body loaded={loaded} />
    </Shell>
  );
}
