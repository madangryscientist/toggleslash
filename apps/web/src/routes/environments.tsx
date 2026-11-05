import { createFileRoute, useRouter } from "@tanstack/react-router";
import { loadEnvironments } from "#/api/operator";
import { EnvironmentsPanel } from "#/components/EnvironmentsPanel";
import { ServerDown, Shell } from "#/components/Shell";
import { OperatorSwitchers } from "#/components/OperatorSwitchers";
import { param, type ProjectSearch } from "#/routes/-search";

export const Route = createFileRoute("/environments")({
  validateSearch: (raw: Record<string, unknown>): ProjectSearch => ({
    project: param(raw["project"]),
    env: param(raw["env"]),
  }),
  loaderDeps: ({ search }) => ({ project: search.project, env: search.env }),
  loader: ({ deps }) => loadEnvironments({ data: deps }),
  component: Environments,
});

function Environments() {
  const loaded = Route.useLoaderData();
  const router = useRouter();
  const navigate = Route.useNavigate();
  if (!loaded.ok) {
    return <ServerDown failure={loaded.failure} onRetry={() => void router.invalidate()} />;
  }
  return (
    <Shell
      active="environments"
      project={loaded.project?.key}
      env={loaded.environment?.key}
      controls={
        <OperatorSwitchers
          loaded={loaded}
          onChoose={(search) => void navigate({ search })}
          hideEnvironment
        />
      }
    >
      <EnvironmentsPanel loaded={loaded} onChanged={() => router.invalidate()} />
    </Shell>
  );
}
