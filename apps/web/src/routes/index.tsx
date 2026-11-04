import { createFileRoute } from "@tanstack/react-router";
import { describeEngine } from "@hackcove/featureslash-engine";

export const Route = createFileRoute("/")({ component: Home });

function Home() {
  return (
    <main className="mx-auto max-w-2xl p-8">
      <h1 className="text-3xl font-bold">FeatureSlash</h1>
      <p>The dashboard. It has no flags yet ({describeEngine()}).</p>
    </main>
  );
}
