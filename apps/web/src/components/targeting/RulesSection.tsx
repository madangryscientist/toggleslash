import { Button, Card, Cluster, Section, Stack, Text } from "@hackcove/ui-react";
import { at, under } from "#/targeting/problems";
import { removeAt, replaceAt, moveBy } from "#/targeting/list";
import { emptyClause } from "#/targeting/draft";
import type { RuleDraft } from "#/targeting/draft";
import type { FieldProblem } from "#/targeting/problems";
import type { SectionProps } from "./shared";
import { ServeEditor } from "../ServeEditor";
import { ClauseRow } from "./ClauseRow";

interface RuleProps {
  index: number;
  count: number;
  rule: RuleDraft;
  variations: string[];
  problems: FieldProblem[];
  disabled: boolean;
  onChange: (rule: RuleDraft) => void;
  onMove: (by: -1 | 1) => void;
  onRemove: () => void;
}

function RuleActions({
  id,
  first,
  last,
  disabled,
  onMove,
  onRemove,
}: {
  id: string;
  first: boolean;
  last: boolean;
  disabled: boolean;
  onMove: (by: -1 | 1) => void;
  onRemove: () => void;
}) {
  return (
    <Cluster>
      <Button variant="ghost" size="sm" disabled={disabled || first} onClick={() => onMove(-1)}>
        Move up
      </Button>
      <Button variant="ghost" size="sm" disabled={disabled || last} onClick={() => onMove(1)}>
        Move down
      </Button>
      <Button
        variant="ghost"
        size="sm"
        tone="danger"
        data-testid={`${id}-remove`}
        disabled={disabled}
        onClick={onRemove}
      >
        Remove rule
      </Button>
    </Cluster>
  );
}

function RuleCard({
  index,
  count,
  rule,
  variations,
  problems,
  disabled,
  onChange,
  onMove,
  onRemove,
}: RuleProps) {
  const path = `rules[${index}]`;
  const id = `rule-${index}`;
  const clausesProblem = at(problems, `${path}.clauses`);
  const serveProblems = under(problems, path).filter((p) => !p.path.startsWith(`${path}.clauses`));
  return (
    <Card
      title={`Rule ${index + 1}`}
      data-testid={id}
      actions={
        <RuleActions
          id={id}
          first={index === 0}
          last={index === count - 1}
          disabled={disabled}
          onMove={onMove}
          onRemove={onRemove}
        />
      }
    >
      <Stack gap="md">
        <Text tone="muted">If all of these clauses match:</Text>
        {clausesProblem !== undefined && <Text tone="danger">{clausesProblem.message}</Text>}
        {rule.clauses.map((clause, j) => (
          <ClauseRow
            key={j}
            id={`${id}-clause-${j}`}
            clause={clause}
            problems={problems}
            path={`${path}.clauses[${j}]`}
            disabled={disabled}
            onChange={(c) => onChange({ ...rule, clauses: replaceAt(rule.clauses, j, c) })}
            onRemove={() => onChange({ ...rule, clauses: removeAt(rule.clauses, j) })}
          />
        ))}
        <div>
          <Button
            variant="outline"
            size="sm"
            data-testid={`${id}-add-clause`}
            disabled={disabled}
            onClick={() => onChange({ ...rule, clauses: [...rule.clauses, emptyClause()] })}
          >
            Add clause
          </Button>
        </div>
        <ServeEditor
          id={id}
          label="Then serve"
          serve={rule.serve}
          variations={variations}
          problems={serveProblems}
          path={path}
          disabled={disabled}
          onChange={(serve) => onChange({ ...rule, serve })}
        />
      </Stack>
    </Card>
  );
}

export function RulesSection({ draft, onDraft, problems, flag, disabled }: SectionProps) {
  return (
    <Section
      title="Rules"
      actions={
        <Button
          variant="outline"
          size="sm"
          data-testid="add-rule"
          disabled={disabled}
          onClick={() =>
            onDraft({
              ...draft,
              rules: [
                ...draft.rules,
                { clauses: [emptyClause()], serve: { kind: "variation", variation: 0 } },
              ],
            })
          }
        >
          Add rule
        </Button>
      }
    >
      <Text tone="muted">
        Checked in order when the flag is on; the first rule that matches decides.
      </Text>
      {draft.rules.length === 0 && (
        <Text tone="muted">No rules: everyone gets the fallthrough.</Text>
      )}
      {draft.rules.map((rule, i) => (
        <RuleCard
          key={i}
          index={i}
          count={draft.rules.length}
          rule={rule}
          variations={flag.variations}
          problems={problems}
          disabled={disabled}
          onChange={(r) => onDraft({ ...draft, rules: replaceAt(draft.rules, i, r) })}
          onMove={(by) => onDraft({ ...draft, rules: moveBy(draft.rules, i, by) })}
          onRemove={() => onDraft({ ...draft, rules: removeAt(draft.rules, i) })}
        />
      ))}
    </Section>
  );
}

export function FallthroughSection({ draft, onDraft, problems, flag, disabled }: SectionProps) {
  return (
    <Section title="Fallthrough">
      <ServeEditor
        id="fallthrough"
        label="When on and no rule matches, serve"
        serve={draft.fallthrough}
        variations={flag.variations}
        problems={under(problems, "fallthrough")}
        path="fallthrough"
        disabled={disabled}
        onChange={(fallthrough) => onDraft({ ...draft, fallthrough })}
      />
    </Section>
  );
}

/** Edits one environment's whole config and saves it against the version the page read. */
