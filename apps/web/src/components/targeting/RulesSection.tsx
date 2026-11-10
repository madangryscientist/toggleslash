import {
  Button,
  Card,
  Checkbox,
  Cluster,
  ClusterGrow,
  Field,
  Input,
  Section,
  Select,
  Stack,
  Text,
} from "@hackcove/ui-react";
import { at, under } from "#/targeting/problems";
import { removeAt, replaceAt, moveBy } from "#/targeting/list";
import { emptyClause } from "#/targeting/draft";
import { ATTRIBUTE_OPS, SEGMENT_OP } from "#/targeting/model";
import type { ClauseDraft, RuleDraft } from "#/targeting/draft";
import type { FieldProblem } from "#/targeting/problems";
import type { SectionProps } from "./shared";
import { ServeEditor } from "../ServeEditor";

interface ClauseProps {
  id: string;
  clause: ClauseDraft;
  problems: FieldProblem[];
  path: string;
  onChange: (clause: ClauseDraft) => void;
  onRemove: () => void;
}

function ClauseRow({ id, clause, problems, path, onChange, onRemove }: ClauseProps) {
  const segment = clause.op === SEGMENT_OP;
  const own = under(problems, path);
  const opProblem = at(problems, `${path}.op`);
  const attributeProblem = at(problems, `${path}.attribute`);
  const valuesProblem = own.find((p) => p !== opProblem && p !== attributeProblem);
  return (
    <Stack gap="xs" data-testid={id}>
      <Cluster wrap align="start">
        <Field label="Operator" htmlFor={`${id}-op`} error={opProblem?.message}>
          <Select
            id={`${id}-op`}
            data-testid={`${id}-op`}
            width="auto"
            value={clause.op}
            onChange={(e) => onChange({ ...clause, op: e.currentTarget.value })}
          >
            {ATTRIBUTE_OPS.map((op) => (
              <option key={op} value={op}>
                {op}
              </option>
            ))}
            <option value={SEGMENT_OP}>in segment</option>
          </Select>
        </Field>
        {!segment && (
          <Field
            label="Attribute"
            htmlFor={`${id}-attribute`}
            error={attributeProblem?.message}
            hint="A name, or a path such as /address/city."
          >
            <Input
              id={`${id}-attribute`}
              data-testid={`${id}-attribute`}
              width="auto"
              value={clause.attribute}
              onChange={(e) => onChange({ ...clause, attribute: e.currentTarget.value })}
            />
          </Field>
        )}
        <ClusterGrow>
          <Field
            label={segment ? "Segment keys" : "Values"}
            htmlFor={`${id}-values`}
            hint='Separated by commas. Quote a value ("42") to keep it text.'
            error={valuesProblem?.message}
          >
            <Input
              id={`${id}-values`}
              data-testid={`${id}-values`}
              value={clause.values}
              onChange={(e) => onChange({ ...clause, values: e.currentTarget.value })}
            />
          </Field>
        </ClusterGrow>
      </Cluster>
      <Cluster>
        <Checkbox
          label="Negate"
          checked={clause.negate}
          onChange={(e) => onChange({ ...clause, negate: e.currentTarget.checked })}
        />
        <Button variant="ghost" size="sm" tone="danger" onClick={onRemove}>
          Remove clause
        </Button>
      </Cluster>
    </Stack>
  );
}

interface RuleProps {
  index: number;
  count: number;
  rule: RuleDraft;
  variations: string[];
  problems: FieldProblem[];
  onChange: (rule: RuleDraft) => void;
  onMove: (by: -1 | 1) => void;
  onRemove: () => void;
}

function RuleCard({
  index,
  count,
  rule,
  variations,
  problems,
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
        <Cluster>
          <Button variant="ghost" size="sm" disabled={index === 0} onClick={() => onMove(-1)}>
            Move up
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={index === count - 1}
            onClick={() => onMove(1)}
          >
            Move down
          </Button>
          <Button
            variant="ghost"
            size="sm"
            tone="danger"
            data-testid={`${id}-remove`}
            onClick={onRemove}
          >
            Remove rule
          </Button>
        </Cluster>
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
            onChange={(c) => onChange({ ...rule, clauses: replaceAt(rule.clauses, j, c) })}
            onRemove={() => onChange({ ...rule, clauses: removeAt(rule.clauses, j) })}
          />
        ))}
        <div>
          <Button
            variant="outline"
            size="sm"
            data-testid={`${id}-add-clause`}
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
          onChange={(serve) => onChange({ ...rule, serve })}
        />
      </Stack>
    </Card>
  );
}

export function RulesSection({ draft, onDraft, problems, flag }: SectionProps) {
  return (
    <Section
      title="Rules"
      actions={
        <Button
          variant="outline"
          size="sm"
          data-testid="add-rule"
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
          onChange={(r) => onDraft({ ...draft, rules: replaceAt(draft.rules, i, r) })}
          onMove={(by) => onDraft({ ...draft, rules: moveBy(draft.rules, i, by) })}
          onRemove={() => onDraft({ ...draft, rules: removeAt(draft.rules, i) })}
        />
      ))}
    </Section>
  );
}

export function FallthroughSection({ draft, onDraft, problems, flag }: SectionProps) {
  return (
    <Section title="Fallthrough">
      <ServeEditor
        id="fallthrough"
        label="When on and no rule matches, serve"
        serve={draft.fallthrough}
        variations={flag.variations}
        problems={under(problems, "fallthrough")}
        path="fallthrough"
        onChange={(fallthrough) => onDraft({ ...draft, fallthrough })}
      />
    </Section>
  );
}

/** Edits one environment's whole config and saves it against the version the page read. */
