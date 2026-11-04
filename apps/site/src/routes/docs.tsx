import { createFileRoute } from "@tanstack/react-router";
import { canonicalUrl } from "#/content/canonical";

export const Route = createFileRoute("/docs")({
  head: () => ({
    meta: [{ title: "Docs: FeatureSlash" }],
    links: [{ rel: "canonical", href: canonicalUrl("/docs") }],
  }),
  component: Docs,
});

function Docs() {
  return (
    <div className="space-y-4">
      <h1 className="text-3xl font-bold">Docs</h1>
      <p>The documentation arrives with the first feature. Nothing to configure yet.</p>
    </div>
  );
}
