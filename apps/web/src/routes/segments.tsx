import { createFileRoute, useRouter } from "@tanstack/react-router";
import { loadSegments } from "#/api/operator";
import { SegmentsPanel } from "#/components/SegmentsPanel";
import { NeedsEnvironment } from "#/components/NeedsEnvironment";
import { OperatorSwitchers } from "#/components/OperatorSwitchers";
import { ServerDown, Shell } from "#/components/Shell";
import { param, type ProjectSearch } from "#/routes/-search";

export const Route = createFileRoute("/segments")({
  validateSearch: (raw: Record<string, unknown>): ProjectSearch => ({
    project: param(raw["project"]),
    env: param(raw["env"]),
  }),
  loaderDeps: ({ search }) => ({ project: search.project, env: search.env }),
  loader: ({ deps }) => loadSegments({ data: deps }),
  component: Segments,
});

function Segments() {
  const loaded = Route.useLoaderData();
  const router = useRouter();
  const navigate = Route.useNavigate();
  if (!loaded.ok) {
    return <ServerDown failure={loaded.failure} onRetry={() => void router.invalidate()} />;
  }
  const { project, environment } = loaded;
  return (
    <Shell
      active="segments"
      project={project?.key}
      env={environment?.key}
      controls={
        <OperatorSwitchers loaded={loaded} onChoose={(search) => void navigate({ search })} />
      }
    >
      {project === null || environment === null ? (
        <NeedsEnvironment project={project} what="Segments" />
      ) : (
        <SegmentsPanel
          // A new project or environment starts with no segment open and no open dialog.
          key={`${project.key}/${environment.key}`}
          project={project}
          environment={environment}
          segments={loaded.segments}
          onChanged={() => router.invalidate()}
        />
      )}
    </Shell>
  );
}
