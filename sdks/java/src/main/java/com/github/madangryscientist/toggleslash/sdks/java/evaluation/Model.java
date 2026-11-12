package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * The typed shape of a validated ruleset. Every field has been checked when a value of these types
 * exists.
 */
final class Model {
  private Model() {}

  /** The ops an attribute clause may have. */
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

    final String text;

    Op(String text) {
      this.text = text;
    }

    static Op of(Object text) {
      for (Op op : values()) {
        if (op.text.equals(text)) {
          return op;
        }
      }
      return null;
    }
  }

  /** A clause of a rule: an attribute clause or a segment clause. */
  interface Clause {
    boolean negate();
  }

  /**
   * An attribute clause. {@code values} are strings, doubles and booleans. {@code prepared} holds,
   * for each value, what the op reads it as: a {@link Pattern}, a {@link Semver} or an instant
   * ({@code Double}), or null when the value is not valid for the op (so it never matches).
   */
  record AttributeClause(
      Op op,
      String attribute,
      List<String> reference,
      List<Object> values,
      List<Object> prepared,
      boolean negate,
      String contextKind)
      implements Clause {}

  /** A segment clause. */
  record SegmentClause(List<String> keys, boolean negate) implements Clause {}

  /**
   * A rollout: the variation and weight of each bucket range, and the kind the bucket is read from.
   */
  record Rollout(int[] variations, int[] weights, String contextKind) {}

  /**
   * What a rule, a fallthrough or a segment rule serves: a variation, or a rollout when {@code
   * rollout} is set.
   */
  record Serve(int variation, Rollout rollout) {}

  /** A rule. */
  record Rule(List<Clause> clauses, Serve serve) {}

  /** A target: the keys of one kind that are served a variation. */
  record Target(int variation, List<String> contextKeys, String contextKind) {}

  /** A prerequisite. */
  record Prerequisite(String flagKey, int variation) {}

  /** What serving the off variation needs. */
  record Header(String key, boolean on, List<Object> variations, Integer offVariation) {}

  /** A flag whose body validated. */
  record Flag(
      Header header,
      String salt,
      double version,
      List<Prerequisite> prerequisites,
      List<Rule> rules,
      Serve fallthrough,
      Map<String, Map<String, int[]>> targets) {}

  /** A segment rule. */
  record SegmentRule(List<AttributeClause> clauses, Integer weight, String rolloutContextKind) {}

  /** A segment that validated, with its context lists indexed by kind. */
  record Segment(
      String key,
      String salt,
      Map<String, Set<String>> included,
      Map<String, Set<String>> excluded,
      List<SegmentRule> rules) {}
}
