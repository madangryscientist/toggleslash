import { Button, Callout, Cluster, Stack, Text } from "@hackcove/ui-react";
import { useState } from "react";
import { saveConfig } from "#/api/dashboard";
import type { Environment, FlagDetail, Project, Sibling } from "#/api/types";
import { isDirty, toDraft, type Draft } from "#/targeting/draft";
import type { Config } from "#/targeting/model";
import { configFieldOf, type FieldProblem } from "#/targeting/problems";
import { toWire } from "#/targeting/wire";
import { PrerequisitesSection } from "./targeting/PrerequisitesSection";
import { FallthroughSection, RulesSection } from "./targeting/RulesSection";
import { StateSection, TargetsSection } from "./targeting/StateSections";

interface Props {
  project: Project;
  environment: Environment;
  flag: FlagDetail;
  config: Config;
  siblings: Sibling[];
  /** The config was saved; the page reloads it. */
  onSaved: () => Promise<void>;
  /** The version was stale; the page says so and reloads, which discards this editor's edits. */
  onConflict: (message: string) => Promise<void>;
}

/** What a save came to, for the editor to show or hand to the page. */
type Outcome =
  | { kind: "saved" }
  | { kind: "conflict"; message: string }
  | { kind: "rejected"; problems: FieldProblem[]; message: string };

async function submit(props: Props, draft: Draft): Promise<Outcome> {
  const built = toWire(draft);
  if (!built.ok) {
    return {
      kind: "rejected",
      problems: built.problems,
      message: "Fix the marked fields, then save.",
    };
  }
  const result = await saveConfig({
    data: {
      project: props.project.key,
      flag: props.flag.key,
      env: props.environment.key,
      version: props.config.version,
      wire: built.wire,
    },
  });
  if (result.ok) return { kind: "saved" };
  const message = result.detail ?? result.failure.message;
  if (result.conflict === true) return { kind: "conflict", message };
  const field = result.detail === undefined ? null : configFieldOf(result.detail);
  return field === null
    ? { kind: "rejected", problems: [], message }
    : {
        kind: "rejected",
        problems: [field],
        message: "The server rejected the config. Fix the marked field, then save.",
      };
}

export function TargetingEditor(props: Props) {
  const { flag, config, siblings, onSaved, onConflict } = props;
  const initial = toDraft(config, flag.variations.length);
  const [draft, setDraft] = useState<Draft>(initial);
  const [problems, setProblems] = useState<FieldProblem[]>([]);
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const dirty = isDirty(draft, initial);

  function edit(next: Draft) {
    setDraft(next);
    // What the server said was about the draft it was sent; an edit makes it stale.
    setProblems([]);
    setFormError(null);
  }

  async function save() {
    setSaving(true);
    setProblems([]);
    setFormError(null);
    try {
      const outcome = await submit(props, draft);
      if (outcome.kind === "saved") await onSaved();
      else if (outcome.kind === "conflict") await onConflict(outcome.message);
      else {
        setProblems(outcome.problems);
        setFormError(outcome.message);
      }
    } catch {
      setFormError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  const shared = { draft, onDraft: edit, problems, flag };
  return (
    <Stack gap="lg">
      <StateSection {...shared} />
      <TargetsSection {...shared} />
      <PrerequisitesSection {...shared} siblings={siblings} />
      <RulesSection {...shared} />
      <FallthroughSection {...shared} />
      {formError !== null && (
        <Callout tone="danger" title="Not saved" data-testid="config-error" role="alert">
          {formError}
        </Callout>
      )}
      <Cluster>
        <Button
          data-testid="save-targeting"
          disabled={!dirty}
          busy={saving}
          busyLabel="Saving"
          onClick={() => void save()}
        >
          Save targeting
        </Button>
        <Button variant="ghost" disabled={!dirty || saving} onClick={() => edit(initial)}>
          Discard changes
        </Button>
        <Text tone="muted">Version {config.version}</Text>
      </Cluster>
    </Stack>
  );
}
