import {
  Button,
  Checkbox,
  Cluster,
  ClusterGrow,
  Field,
  Input,
  Select,
  Stack,
} from "@hackcove/ui-react";
import { at, under } from "#/targeting/problems";
import { ATTRIBUTE_OPS, SEGMENT_OP } from "#/targeting/model";
import type { ClauseDraft } from "#/targeting/draft";
import type { FieldProblem } from "#/targeting/problems";

interface ClauseProps {
  id: string;
  clause: ClauseDraft;
  problems: FieldProblem[];
  path: string;
  disabled: boolean;
  onChange: (clause: ClauseDraft) => void;
  onRemove: () => void;
}

function ClauseKind({
  id,
  clause,
  error,
  disabled,
  onChange,
}: {
  id: string;
  clause: ClauseDraft;
  error: string | undefined;
  disabled: boolean;
  onChange: (clause: ClauseDraft) => void;
}) {
  return (
    <Field label="Context kind" htmlFor={`${id}-kind`} error={error} hint="Blank is user.">
      <Input
        id={`${id}-kind`}
        data-testid={`${id}-kind`}
        disabled={disabled}
        width="auto"
        placeholder="user"
        value={clause.contextKind}
        onChange={(e) => onChange({ ...clause, contextKind: e.currentTarget.value })}
      />
    </Field>
  );
}

function ClauseValues({
  id,
  clause,
  error,
  disabled,
  onChange,
}: {
  id: string;
  clause: ClauseDraft;
  error: string | undefined;
  disabled: boolean;
  onChange: (clause: ClauseDraft) => void;
}) {
  return (
    <ClusterGrow>
      <Field
        label={clause.op === SEGMENT_OP ? "Segment keys" : "Values"}
        htmlFor={`${id}-values`}
        hint='Separated by commas. Quote a value ("42") to keep it text.'
        error={error}
      >
        <Input
          id={`${id}-values`}
          data-testid={`${id}-values`}
          disabled={disabled}
          value={clause.values}
          onChange={(e) => onChange({ ...clause, values: e.currentTarget.value })}
        />
      </Field>
    </ClusterGrow>
  );
}

function ClauseFooter({
  clause,
  disabled,
  onChange,
  onRemove,
}: Pick<ClauseProps, "disabled" | "onChange" | "onRemove"> & { clause: ClauseDraft }) {
  return (
    <Cluster>
      <Checkbox
        label="Negate"
        disabled={disabled}
        checked={clause.negate}
        onChange={(e) => onChange({ ...clause, negate: e.currentTarget.checked })}
      />
      <Button variant="ghost" size="sm" tone="danger" disabled={disabled} onClick={onRemove}>
        Remove clause
      </Button>
    </Cluster>
  );
}

export function ClauseRow({
  id,
  clause,
  problems,
  path,
  disabled,
  onChange,
  onRemove,
}: ClauseProps) {
  const segment = clause.op === SEGMENT_OP;
  const own = under(problems, path);
  const opProblem = at(problems, `${path}.op`);
  const attributeProblem = at(problems, `${path}.attribute`);
  const kindProblem = at(problems, `${path}.contextKind`);
  const valuesProblem = own.find(
    (p) => p !== opProblem && p !== attributeProblem && p !== kindProblem,
  );
  return (
    <Stack gap="xs" data-testid={id}>
      <Cluster wrap align="start">
        <Field label="Operator" htmlFor={`${id}-op`} error={opProblem?.message}>
          <Select
            id={`${id}-op`}
            data-testid={`${id}-op`}
            disabled={disabled}
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
              disabled={disabled}
              width="auto"
              value={clause.attribute}
              onChange={(e) => onChange({ ...clause, attribute: e.currentTarget.value })}
            />
          </Field>
        )}
        {!segment && (
          <ClauseKind
            id={id}
            clause={clause}
            error={kindProblem?.message}
            disabled={disabled}
            onChange={onChange}
          />
        )}
        <ClauseValues
          id={id}
          clause={clause}
          error={valuesProblem?.message}
          disabled={disabled}
          onChange={onChange}
        />
      </Cluster>
      <ClauseFooter clause={clause} disabled={disabled} onChange={onChange} onRemove={onRemove} />
    </Stack>
  );
}
