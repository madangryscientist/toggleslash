import { Stack } from "@hackcove/ui-react";
import { createFileRoute, useRouter } from "@tanstack/react-router";
import { loadRuleset } from "#/api/operator";
import { RulesetNeedsScope, RulesetNotFound, RulesetView } from "#/components/RulesetView";
import { ServerDown, Shell } from "#/components/Shell";
import { param } from "#/routes/-search";

interface Search {
  project?: string | undefined;
  env?: string | undefined;
  flag?: string | undefined;
}

export const Route = createFileRoute("/rulesets/$hash")({
  validateSearch: (raw: Record<string, unknown>): Search => ({
    project: param(raw["project"]),
    env: param(raw["env"]),
    flag: param(raw["flag"]),
  }),
  loaderDeps: ({ search }) => search,
  // A hash belongs to one environment, so without both keys there is nothing to ask the server.
  loader: ({ deps, params }) =>
    deps.project === undefined || deps.env === undefined
      ? null
      : loadRuleset({
          data: { project: deps.project, env: deps.env, hash: params.hash, flag: deps.flag ?? "" },
        }),
  component: PastRuleset,
});

type Loaded = ReturnType<typeof Route.useLoaderData>;

function Answer({ loaded, hash }: { loaded: Loaded; hash: string }) {
  if (loaded === null || !loaded.ok) return <RulesetNeedsScope hash={hash} />;
  const { found, project, env } = loaded;
  if (!found.ok) {
    return <RulesetNotFound project={project} env={env} hash={hash} message={found.message} />;
  }
  return (
    <RulesetView
      project={project}
      env={env}
      hash={loaded.hash}
      flag={loaded.flag}
      ruleset={found.ruleset}
    />
  );
}

function PastRuleset() {
  const loaded = Route.useLoaderData();
  const { hash } = Route.useParams();
  const search = Route.useSearch();
  const router = useRouter();
  if (loaded !== null && !loaded.ok) {
    return <ServerDown failure={loaded.failure} onRetry={() => void router.invalidate()} />;
  }
  return (
    <Shell project={search.project} env={search.env}>
      <Stack width="full">
        <Answer loaded={loaded} hash={hash} />
      </Stack>
    </Shell>
  );
}
