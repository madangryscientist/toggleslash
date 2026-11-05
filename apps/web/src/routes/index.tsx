import {
  AppShell,
  AppShellBrand,
  Avatar,
  Callout,
  ErrorPage,
  Select,
  Text,
} from "@hackcove/ui-react";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { loadDashboard } from "#/api/dashboard";
import type { Failure, Loaded } from "#/api/types";
import { FlagList } from "#/components/FlagList";

interface Search {
  project?: string | undefined;
  env?: string | undefined;
}

function key(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

export const Route = createFileRoute("/")({
  validateSearch: (raw: Record<string, unknown>): Search => ({
    project: key(raw["project"]),
    env: key(raw["env"]),
  }),
  loaderDeps: ({ search }) => ({ project: search.project, env: search.env }),
  loader: ({ deps }) => loadDashboard({ data: deps }),
  component: Dashboard,
});

const FAILURE_TITLES: Record<Failure["kind"], string> = {
  unreachable: "The FeatureSlash server is not reachable",
  unconfigured: "The dashboard is not configured",
  unauthorized: "The server rejected the admin token",
  error: "The server could not answer",
};

const FAILURE_HELP: Record<Failure["kind"], string> = {
  unreachable:
    "The dashboard could not get an answer from the server. Check that it is running and that FEATURESLASH_API_URL points at it, then try again.",
  unconfigured:
    "The dashboard reads the server's address and the admin token from its environment. Set both, then try again.",
  unauthorized:
    "The server answered, but not to this token. Check FEATURESLASH_ADMIN_TOKEN against the token the server was started with.",
  error: "The server answered with an error. Try again; if it persists, look at the server's log.",
};

type Ready = Extract<Loaded, { ok: true }>;

function Switchers({ loaded }: { loaded: Ready }) {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const { projects, project, environments, environment } = loaded;
  return (
    <>
      <Select
        aria-label="Project"
        width="auto"
        value={project?.key ?? ""}
        disabled={projects.length === 0}
        onChange={(event) =>
          void navigate({ search: { project: event.currentTarget.value, env: undefined } })
        }
      >
        {projects.length === 0 && <option value="">No projects</option>}
        {projects.map((p) => (
          <option key={p.key} value={p.key}>
            {p.name}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Environment"
        width="auto"
        value={environment?.key ?? ""}
        disabled={environments.length === 0}
        onChange={(event) =>
          void navigate({
            search: { project: search.project ?? project?.key, env: event.currentTarget.value },
          })
        }
      >
        {environments.length === 0 && <option value="">No environments</option>}
        {environments.map((e) => (
          <option key={e.key} value={e.key}>
            {e.name}
          </option>
        ))}
      </Select>
    </>
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
    const { failure } = loaded;
    return (
      <ErrorPage
        data-testid="server-down"
        title={FAILURE_TITLES[failure.kind]}
        detail={failure.message}
        onRetry={() => void router.invalidate()}
      >
        {FAILURE_HELP[failure.kind]}
      </ErrorPage>
    );
  }

  return (
    <AppShell
      brand={
        <AppShellBrand
          logo={<Avatar name="FeatureSlash" size="sm" />}
          label="FeatureSlash"
          href="/"
        />
      }
      controls={<Switchers loaded={loaded} />}
    >
      <Body loaded={loaded} />
    </AppShell>
  );
}
