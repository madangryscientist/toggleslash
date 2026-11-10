import { Callout, Text } from "@hackcove/ui-react";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { loadDashboard } from "#/api/dashboard";
import type { Loaded } from "#/api/types";
import { FlagList } from "#/components/FlagList";
import { ServerDown, Shell, Switchers } from "#/components/Shell";
import { param, type ProjectSearch } from "#/routes/-search";

interface DashboardSearch extends ProjectSearch {
  /** `true` for the archived view; absent for the active one. */
  archived?: true | undefined;
}

export const Route = createFileRoute("/")({
  validateSearch: (raw: Record<string, unknown>): DashboardSearch => ({
    project: param(raw["project"]),
    env: param(raw["env"]),
    archived: raw["archived"] === true || raw["archived"] === "true" ? true : undefined,
  }),
  loaderDeps: ({ search }) => ({
    project: search.project,
    env: search.env,
    archived: search.archived === true,
  }),
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
      onProject={(key) =>
        void navigate({ search: { project: key, env: undefined, archived: search.archived } })
      }
      onEnvironment={(key) =>
        void navigate({
          search: { project: search.project ?? project?.key, env: key, archived: search.archived },
        })
      }
    />
  );
}

function Body({ loaded }: { loaded: Ready }) {
  const router = useRouter();
  const navigate = Route.useNavigate();
  const { project, environment, flags, archived } = loaded;
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
  if (environment === null) {
    return (
      <Callout tone="info" title={`${project.name} has no environments`}>
        <Text>
          Create one with the CLI: <code>toggleslash envs create</code>. Flags are switched on and
          off per environment.
        </Text>
      </Callout>
    );
  }
  return (
    <FlagList
      // A new project or environment starts with a fresh search box and no open dialog.
      key={`${project.key}/${environment.key}/${archived}`}
      project={project}
      environment={environment}
      flags={flags}
      archived={archived}
      onView={(next) =>
        void navigate({ search: (prev) => ({ ...prev, archived: next ? true : undefined }) })
      }
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
    <Shell
      active="flags"
      project={loaded.project?.key}
      env={loaded.environment?.key}
      controls={<DashboardSwitchers loaded={loaded} />}
    >
      <Body loaded={loaded} />
    </Shell>
  );
}
