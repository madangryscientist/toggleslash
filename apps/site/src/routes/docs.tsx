import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/docs")({
  head: () => ({
    meta: [{ title: "Docs: ToggleSlash" }],
  }),
  component: Docs,
});

function Docs() {
  return (
    <div className="space-y-4">
      <h1 className="text-3xl font-bold">Docs</h1>
      <p>
        The documentation is not published yet. It lives in the repository, which is not public yet.
      </p>
    </div>
  );
}
