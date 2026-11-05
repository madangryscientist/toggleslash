import { createFileRoute, useRouter } from "@tanstack/react-router";
import { loadAudit } from "#/api/operator";
import { AuditLog } from "#/components/AuditLog";
import { nonEmpty } from "#/components/text";
import { OperatorSwitchers } from "#/components/OperatorSwitchers";
import { ServerDown, Shell } from "#/components/Shell";
import { NeedsEnvironment } from "#/components/NeedsEnvironment";
import { param } from "#/routes/-search";

interface Search {
  project?: string | undefined;
  /** Not shown on this page, but carried so the nav still opens the other pages on it. */
  env?: string | undefined;
  flag?: string | undefined;
  environment?: string | undefined;
  since?: string | undefined;
  cursor?: string | undefined;
}

const DAY = /^\d{4}-\d{2}-\d{2}$/;

export const Route = createFileRoute("/audit")({
  validateSearch: (raw: Record<string, unknown>): Search => {
    const since = param(raw["since"]);
    return {
      project: param(raw["project"]),
      env: param(raw["env"]),
      flag: param(raw["flag"]),
      environment: param(raw["environment"]),
      // A hand-edited date the server would refuse is dropped rather than shown as an error.
      since: since !== undefined && DAY.test(since) ? since : undefined,
      cursor: param(raw["cursor"]),
    };
  },
  loaderDeps: ({ search }) => search,
  loader: ({ deps }) => loadAudit({ data: deps }),
  component: Audit,
});

function Audit() {
  const loaded = Route.useLoaderData();
  const router = useRouter();
  const navigate = Route.useNavigate();
  if (!loaded.ok) {
    return <ServerDown failure={loaded.failure} onRetry={() => void router.invalidate()} />;
  }
  const { project } = loaded;
  return (
    <Shell
      active="audit"
      project={project?.key}
      env={loaded.environment?.key}
      controls={
        <OperatorSwitchers
          loaded={loaded}
          hideEnvironment
          onChoose={({ project: next }) => void navigate({ search: { project: next } })}
        />
      }
    >
      {project === null ? (
        <NeedsEnvironment project={null} what="The audit log" />
      ) : (
        <AuditLog
          project={project}
          environments={loaded.environments}
          filters={loaded.filters}
          env={loaded.environment?.key}
          cursor={loaded.cursor}
          page={loaded.page}
          onApply={(filters) =>
            void navigate({
              search: { project: project.key, env: loaded.environment?.key, ...nonEmpty(filters) },
            })
          }
        />
      )}
    </Shell>
  );
}
