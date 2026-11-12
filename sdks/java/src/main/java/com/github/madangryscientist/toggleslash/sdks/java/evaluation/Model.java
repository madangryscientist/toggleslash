package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;
import java.util.Map;
import java.util.Set;

/** The validated shapes a ruleset loads into. */
final class Model {

  private Model() {}

  /** An attribute operator. */
  enum Op {
    IN("in"),
    CONTAINS("contains"),
    STARTS_WITH("startsWith"),
    ENDS_WITH("endsWith"),
    LT("lt"),
    LTE("lte"),
    GT("gt"),
    GTE("gte"),
    SEMVER_EQ("semverEq"),
    SEMVER_LT("semverLt"),
    SEMVER_LTE("semverLte"),
    SEMVER_GT("semverGt"),
    SEMVER_GTE("semverGte"),
    BEFORE("before"),
    AFTER("after"),
    MATCHES("matches");

    private final String wire;

    Op(String wire) {
      this.wire = wire;
    }

    static Op of(Object name) {
      for (Op op : values()) {
        if (op.wire.equals(name)) {
          return op;
        }
      }
      return null;
    }
  }

  sealed interface Clause permits AttributeClause, SegmentClause {}

  /**
   * An attribute clause. {@code values} hold {@link String}, {@link Double} and {@link Boolean};
   * {@code patterns} is parallel to {@code values} and holds the compiled pattern of each string
   * value for {@code matches} (null for an invalid one), and is null for the other operators.
   */
  record AttributeClause(
      Op op,
      List<String> components,
      List<Object> values,
      PatternProgram[] patterns,
      boolean negate,
      String contextKind)
      implements Clause {}

  record SegmentClause(List<String> values, boolean negate) implements Clause {}

  record Rollout(int[] variations, int[] weights, String contextKind) {}

  /** A variation index, or a rollout when {@code rollout} is set. */
  record Serve(int variation, Rollout rollout) {}

  record Rule(List<Clause> clauses, Serve serve) {}

  record Target(int variation, List<String> contextKeys, String contextKind) {}

  record Prerequisite(String flagKey, int variation) {}

  /** What serving the off variation needs. {@code offVariation} is -1 when there is none. */
  record Header(String key, boolean on, List<Object> variations, int offVariation) {}

  /** The place of a target in {@code targets} and the variation it serves. */
  record TargetHit(int order, int variation) {}

  record Flag(
      Header header,
      String salt,
      long version,
      List<Prerequisite> prerequisites,
      List<Rule> rules,
      Serve fallthrough,
      Map<String, Map<String, TargetHit>> targets) {}

  record SegmentRule(List<AttributeClause> clauses, int weight, String rolloutContextKind) {}

  /** A segment, with its keys by kind. {@code weight} of a rule is -1 when absent. */
  record Segment(
      String key,
      String salt,
      Map<String, Set<String>> included,
      Map<String, Set<String>> excluded,
      List<SegmentRule> rules) {}
}
