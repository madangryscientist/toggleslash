import { createFileRoute, useRouter } from "@tanstack/react-router";
import { loadKeys } from "#/api/operator";
import { KeysPanel } from "#/components/KeysPanel";
import { NeedsEnvironment } from "#/components/NeedsEnvironment";
import { OperatorSwitchers } from "#/components/OperatorSwitchers";
import { ServerDown, Shell } from "#/components/Shell";
import { param, type ProjectSearch } from "#/routes/-search";

export const Route = createFileRoute("/keys")({
  validateSearch: (raw: Record<string, unknown>): ProjectSearch => ({
    project: param(raw["project"]),
    env: param(raw["env"]),
  }),
  loaderDeps: ({ search }) => ({ project: search.project, env: search.env }),
  loader: ({ deps }) => loadKeys({ data: deps }),
  component: Keys,
});

function Keys() {
  const loaded = Route.useLoaderData();
  const router = useRouter();
  const navigate = Route.useNavigate();
  if (!loaded.ok) {
    return <ServerDown failure={loaded.failure} onRetry={() => void router.invalidate()} />;
  }
  const { project, environment } = loaded;
  return (
    <Shell
      active="keys"
      project={project?.key}
      env={environment?.key}
      controls={
        <OperatorSwitchers loaded={loaded} onChoose={(search) => void navigate({ search })} />
      }
    >
      {project === null || environment === null ? (
        <NeedsEnvironment project={project} what="SDK keys" />
      ) : (
        <KeysPanel
          // A new project or environment starts with no key on show and no open dialog.
          key={`${project.key}/${environment.key}`}
          project={project}
          environment={environment}
          keys={loaded.keys}
          onChanged={() => router.invalidate()}
        />
      )}
    </Shell>
  );
}
