import { Breadcrumbs, Callout, PageHeader, Pill, Stack, Text } from "@hackcove/ui-react";
import { Link, createFileRoute, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import { loadFlagDetail } from "#/api/dashboard";
import type { DetailLoaded } from "#/api/types";
import { History, UsagePanel } from "#/components/Activity";
import { MetaForm } from "#/components/MetaForm";
import { ServerDown, Shell, Switchers } from "#/components/Shell";
import { TargetingEditor } from "#/components/TargetingEditor";

interface Search {
  project?: string | undefined;
  env?: string | undefined;
}

function param(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

export const Route = createFileRoute("/flags/$flag")({
  validateSearch: (raw: Record<string, unknown>): Search => ({
    project: param(raw["project"]),
    env: param(raw["env"]),
  }),
  loaderDeps: ({ search }) => ({ project: search.project, env: search.env }),
  loader: ({ deps, params }) =>
    loadFlagDetail({ data: { project: deps.project ?? "", flag: params.flag, env: deps.env } }),
  component: FlagDetailPage,
});

type Ready = Extract<DetailLoaded, { ok: true }>;

interface Notice {
  tone: "info" | "warn";
  title: string;
  text: string;
}

function Header({ loaded, search }: { loaded: Ready; search: Search }) {
  const { flag, project } = loaded;
  return (
    <PageHeader
      breadcrumbs={
        <Breadcrumbs
          crumbs={[
            {
              label: project.name,
              render: ({ className, children }) => (
                <Link
                  to="/"
                  search={{ project: project.key, env: search.env }}
                  className={className}
                  data-testid="back-to-flags"
                >
                  {children}
                </Link>
              ),
            },
            flag.name,
          ]}
        />
      }
      title={flag.name}
      subtitle={flag.description === "" ? flag.key : `${flag.key}: ${flag.description}`}
      actions={flag.archived ? <Pill tone="amber">Archived</Pill> : undefined}
    />
  );
}

type Reload = (next: Notice | null) => Promise<void>;

function Targeting({ loaded, reload }: { loaded: Ready; reload: Reload }) {
  const { project, environment, flag, config, siblings } = loaded;
  if (environment === null) {
    return (
      <Callout tone="info" title={`${project.name} has no environments`}>
        <Text>
          Create one with the CLI: <code>featureslash envs create</code>. Targeting is set per
          environment.
        </Text>
      </Callout>
    );
  }
  if (config === null) {
    return (
      <Callout tone="info" title={`No targeting in ${environment.name}`}>
        <Text>This flag has no config in {environment.name}.</Text>
      </Callout>
    );
  }
  return (
    <TargetingEditor
      // A new version (saved here or by someone else) or environment starts a fresh draft.
      key={`${environment.key}/${config.version}`}
      project={project}
      environment={environment}
      flag={flag}
      config={config}
      siblings={siblings}
      onSaved={() =>
        reload({ tone: "info", title: "Targeting saved", text: `Saved in ${environment.name}.` })
      }
      onConflict={() =>
        reload({
          tone: "warn",
          title: "Someone else changed this flag",
          text: "Your edits were not saved; the latest version is loaded.",
        })
      }
    />
  );
}

function Body({ loaded, search }: { loaded: Ready; search: Search }) {
  const router = useRouter();
  const [notice, setNotice] = useState<Notice | null>(null);
  const { project, environment, flag, history, usage } = loaded;

  async function reload(next: Notice | null) {
    setNotice(next);
    await router.invalidate();
  }

  return (
    <Stack gap="lg">
      <Header loaded={loaded} search={search} />
      {notice !== null && (
        <Callout tone={notice.tone} title={notice.title} data-testid="notice" role="status">
          {notice.text}
        </Callout>
      )}
      <MetaForm
        // A saved or reloaded flag starts a fresh form.
        key={JSON.stringify(flag)}
        project={project}
        flag={flag}
        onSaved={() =>
          reload({ tone: "info", title: "Details saved", text: "The flag's details were saved." })
        }
      />
      <Targeting loaded={loaded} reload={reload} />
      <History events={history} />
      <UsagePanel usage={usage} flag={flag} environment={environment?.key ?? null} />
    </Stack>
  );
}

function FlagDetailPage() {
  const loaded = Route.useLoaderData();
  const search = Route.useSearch();
  const router = useRouter();
  const navigate = Route.useNavigate();

  if (!loaded.ok) {
    return <ServerDown failure={loaded.failure} onRetry={() => void router.invalidate()} />;
  }
  return (
    <Shell
      active="flags"
      project={loaded.project.key}
      env={loaded.environment?.key}
      controls={
        <Switchers
          projects={[loaded.project]}
          project={loaded.project}
          environments={loaded.environments}
          environment={loaded.environment}
          projectLocked
          onProject={() => undefined}
          onEnvironment={(env) => void navigate({ search: { project: loaded.project.key, env } })}
        />
      }
    >
      <Body key={loaded.environment?.key ?? ""} loaded={loaded} search={search} />
    </Shell>
  );
}
