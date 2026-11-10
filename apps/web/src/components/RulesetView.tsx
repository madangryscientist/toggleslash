import {
  BulletList,
  Callout,
  Card,
  CodeBlock,
  EmptyState,
  MetaList,
  PageHeader,
  Pill,
  Stack,
  Text,
} from "@hackcove/ui-react";
import { Link } from "@tanstack/react-router";
import type { KindMembers, PastFlag, PastRuleset, PastSegment } from "#/api/types";
import { describeClause, describeServe, variationLabel } from "#/targeting/describe";
import { stamp } from "./text";

interface Scope {
  project: string;
  env: string;
}

function FlagCard({ flag, scope }: { flag: PastFlag; scope: Scope }) {
  const { config, variations } = flag;
  return (
    <Card
      title={flag.key}
      status={
        <Pill tone={config.on ? "emerald" : "slate"} dot data-testid={`past-on-${flag.key}`}>
          {config.on ? "On" : "Off"}
        </Pill>
      }
      data-testid={`past-flag-${flag.key}`}
      actions={
        <Link to="/flags/$flag" params={{ flag: flag.key }} search={scope}>
          The flag now
        </Link>
      }
    >
      <Stack gap="xs">
        <Text>
          Variations: {variations.map((v, i) => `${i} = ${v}`).join(", ") || "none"}.{" "}
          {config.on
            ? "Targeting was on."
            : `Targeting was off, so it served ${
                config.offVariation === null
                  ? "no variation"
                  : variationLabel(config.offVariation, variations)
              }.`}
        </Text>
        <BulletList>
          {config.prerequisites.map((p) => (
            <li key={`pre-${p.flagKey}-${p.variation}`}>
              Prerequisite: {p.flagKey} must serve variation {p.variation}
            </li>
          ))}
          {config.targets.map((t, i) => (
            // One variation can hold several targets (one per context kind), so the position is the key.
            <li key={`target-${i}`}>
              Targets
              {t.contextKind === undefined || t.contextKind === "user" ? "" : ` (${t.contextKind})`}
              : {t.contextKeys.join(", ") || "no contexts"} get{" "}
              {variationLabel(t.variation, variations)}
            </li>
          ))}
          {config.rules.map((rule, i) => (
            // A rule has no id; its position is its identity, and that is how the evaluator reads it.
            <li key={`rule-${i}`} data-testid={`past-rule-${flag.key}-${i}`}>
              Rule {i + 1}: {rule.clauses.map(describeClause).join(" and ") || "always"} →{" "}
              {describeServe(rule.serve, variations)}
            </li>
          ))}
          <li>Otherwise: {describeServe(config.fallthrough, variations)}</li>
        </BulletList>
        <MetaList>
          <span>Version {config.version}</span>
        </MetaList>
      </Stack>
    </Card>
  );
}

/** User keys as they are, then the other kinds' as `kind:key`. */
function members(users: string[], others: KindMembers[]): string {
  const all = [...users, ...others.flatMap((o) => o.values.map((v) => `${o.contextKind}:${v}`))];
  return all.join(", ") || "none";
}

function SegmentCard({ segment }: { segment: PastSegment }) {
  return (
    <Card title={`Segment ${segment.key}`} data-testid={`past-segment-${segment.key}`}>
      <Stack gap="xs">
        <Text>
          Included: {members(segment.included, segment.includedContexts)}. Excluded:{" "}
          {members(segment.excluded, segment.excludedContexts)}.
        </Text>
        <CodeBlock label={`Rules of ${segment.key}`}>{segment.rules}</CodeBlock>
      </Stack>
    </Card>
  );
}

interface Props extends Scope {
  hash: string;
  flag: string | null;
  ruleset: PastRuleset;
}

/** The flags as the environment served them under one hash. Read only, and without salts. */
export function RulesetView({ project, env, hash, flag, ruleset }: Props) {
  return (
    <Stack width="full">
      <PageHeader
        title="Flags under a past ruleset"
        subtitle={`${project} · ${env} · first served ${stamp(ruleset.firstServedAt)}`}
      />
      <Text data-testid="ruleset-hash">
        Hash <code>{hash}</code>
      </Text>
      {flag !== null && (
        <Callout tone="info" title={`Only ${flag}`}>
          <Text>
            Narrowed to one flag and the segments its rules name.{" "}
            <Link to="/rulesets/$hash" params={{ hash }} search={{ project, env }}>
              Show every flag under this hash
            </Link>
            .
          </Text>
        </Callout>
      )}
      {ruleset.flags.length === 0 ? (
        <EmptyState title="No flags">
          <Text>This ruleset had no flags in it.</Text>
        </EmptyState>
      ) : (
        ruleset.flags.map((f) => <FlagCard key={f.key} flag={f} scope={{ project, env }} />)
      )}
      {ruleset.segments.map((s) => (
        <SegmentCard key={s.key} segment={s} />
      ))}
    </Stack>
  );
}

/** The clear answer when the server has nothing for that hash (and flag): the server says why, then where a hash comes from. */
export function RulesetNotFound({
  project,
  env,
  hash,
  message,
}: Scope & { hash: string; message: string }) {
  return (
    <Callout tone="warn" title="No ruleset found for that hash" data-testid="ruleset-not-found">
      <Stack gap="xs">
        <Text>
          {message} Looked up: <code>{hash}</code> in {env} of {project}.
        </Text>
        <Text>
          A hash comes from a trace's <code>toggleslash.ruleset.etag</code>, with its{" "}
          <code>toggleslash.project</code> and <code>toggleslash.environment</code>. The server
          keeps a ruleset the first time it answers an SDK with a 200, so a hash only ever seen as a
          304 has nothing kept.
        </Text>
      </Stack>
    </Callout>
  );
}

/** The page needs both keys, because a hash belongs to one environment. */
export function RulesetNeedsScope({ hash }: { hash: string }) {
  return (
    <Callout tone="info" title="Which project and environment?" data-testid="ruleset-needs-scope">
      <Text>
        A hash belongs to one environment, so this page needs <code>?project=</code> and{" "}
        <code>&amp;env=</code> in its address (a trace carries both). Without them there is nothing
        to look <code>{hash}</code> up in.
      </Text>
    </Callout>
  );
}
