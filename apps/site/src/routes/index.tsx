import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  head: () => ({
    meta: [{ title: "ToggleSlash: feature flags you can self-host" }],
  }),
  component: Home,
});

function Home() {
  return (
    <div className="space-y-4">
      <h1 className="text-3xl font-bold">ToggleSlash</h1>
      <p>Feature flags you can run yourself. Source available under the Elastic License 2.0.</p>
      <p>
        ToggleSlash is in early development and nothing is published yet. The server, the CLI and
        the TypeScript server SDK work; the code is not yet open to the public.
      </p>
    </div>
  );
}
