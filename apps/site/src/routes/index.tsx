import { createFileRoute } from "@tanstack/react-router";
import { canonicalUrl } from "#/content/canonical";

export const Route = createFileRoute("/")({
  head: () => ({
    meta: [{ title: "FeatureSlash: feature flags you can self-host" }],
    links: [{ rel: "canonical", href: canonicalUrl("/") }],
  }),
  component: Home,
});

function Home() {
  return (
    <div className="space-y-4">
      <h1 className="text-3xl font-bold">FeatureSlash</h1>
      <p>Open source feature flags you can run yourself. MIT licensed.</p>
      <p>
        FeatureSlash is in early development: the repository is a skeleton and has no flags yet.
      </p>
    </div>
  );
}
