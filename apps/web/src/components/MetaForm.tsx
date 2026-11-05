import {
  Button,
  Callout,
  Checkbox,
  Cluster,
  Field,
  Input,
  Section,
  Stack,
  Text,
  Textarea,
} from "@hackcove/ui-react";
import { useState, type FormEvent } from "react";
import { parseTags } from "./text";
import { saveFlagMeta } from "#/api/dashboard";
import type { FlagDetail, Project } from "#/api/types";
import { at, metaFieldOf, type FieldProblem } from "#/targeting/problems";

interface Props {
  project: Project;
  flag: FlagDetail;
  /** The flag was saved; the page reloads it. */
  onSaved: () => Promise<void>;
}

interface Draft {
  name: string;
  description: string;
  owner: string;
  tags: string;
  temporary: boolean;
}

function draftOf(flag: FlagDetail): Draft {
  return {
    name: flag.name,
    description: flag.description,
    owner: flag.owner,
    tags: flag.tags.join(", "),
    temporary: flag.temporary,
  };
}

interface FieldsProps {
  flag: FlagDetail;
  draft: Draft;
  problems: FieldProblem[];
  onDraft: (next: Draft) => void;
}

function MetaFields({ flag, draft, problems, onDraft: edit }: FieldsProps) {
  return (
    <>
      <Field label="Key" htmlFor="meta-key" hint="What code asks for. It cannot change.">
        <Input id="meta-key" value={flag.key} readOnly />
      </Field>
      <Field label="Name" htmlFor="meta-name" error={at(problems, "name")?.message}>
        <Input
          id="meta-name"
          data-testid="meta-name"
          value={draft.name}
          onChange={(e) => edit({ ...draft, name: e.currentTarget.value })}
        />
      </Field>
      <Field
        label="Description"
        htmlFor="meta-description"
        error={at(problems, "description")?.message}
      >
        <Textarea
          id="meta-description"
          data-testid="meta-description"
          rows={2}
          value={draft.description}
          onChange={(e) => edit({ ...draft, description: e.currentTarget.value })}
        />
      </Field>
      <Field label="Owner" htmlFor="meta-owner" error={at(problems, "owner")?.message}>
        <Input
          id="meta-owner"
          data-testid="meta-owner"
          value={draft.owner}
          onChange={(e) => edit({ ...draft, owner: e.currentTarget.value })}
        />
      </Field>
      <Field
        label="Tags"
        htmlFor="meta-tags"
        hint="Separated by commas or spaces."
        error={at(problems, "tags")?.message}
      >
        <Input
          id="meta-tags"
          data-testid="meta-tags"
          value={draft.tags}
          onChange={(e) => edit({ ...draft, tags: e.currentTarget.value })}
        />
      </Field>
      <Checkbox
        label="Temporary"
        description="A rollout or experiment to remove later, not a permanent switch."
        checked={draft.temporary}
        onChange={(e) => edit({ ...draft, temporary: e.currentTarget.checked })}
      />
      <Text tone="muted">Variations: {flag.variations.map((v, i) => `${i}: ${v}`).join(", ")}</Text>
    </>
  );
}

/** Edits what describes the flag (not its key or variations, which do not change). */
export function MetaForm({ project, flag, onSaved }: Props) {
  const initial = draftOf(flag);
  const [draft, setDraft] = useState<Draft>(initial);
  const [problems, setProblems] = useState<FieldProblem[]>([]);
  const [formError, setFormError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const dirty = JSON.stringify(draft) !== JSON.stringify(initial);

  function edit(next: Draft) {
    setDraft(next);
    setProblems([]);
    setFormError(null);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setProblems([]);
    setFormError(null);
    try {
      const result = await saveFlagMeta({
        data: {
          project: project.key,
          flag: flag.key,
          patch: {
            name: draft.name,
            description: draft.description,
            owner: draft.owner,
            temporary: draft.temporary,
            tags: parseTags(draft.tags),
          },
        },
      });
      if (result.ok) {
        await onSaved();
        return;
      }
      const message = result.detail ?? result.failure.message;
      const field = metaFieldOf(message);
      if (field === null) setFormError(message);
      else setProblems([field]);
    } catch {
      setFormError("The dashboard could not reach its own server. Reload the page.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Section title="Details">
      <form onSubmit={(e) => void submit(e)} noValidate data-testid="meta-form">
        <Stack gap="sm">
          <MetaFields flag={flag} draft={draft} problems={problems} onDraft={edit} />
          {formError !== null && (
            <Callout tone="danger" title="Not saved" data-testid="meta-error" role="alert">
              {formError}
            </Callout>
          )}
          <Cluster>
            <Button
              type="submit"
              data-testid="save-meta"
              disabled={!dirty}
              busy={saving}
              busyLabel="Saving"
            >
              Save details
            </Button>
            <Button variant="ghost" disabled={!dirty || saving} onClick={() => edit(initial)}>
              Discard changes
            </Button>
          </Cluster>
        </Stack>
      </form>
    </Section>
  );
}
