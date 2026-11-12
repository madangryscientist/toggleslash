package com.github.madangryscientist.toggleslash.sdks.java.eval;

import com.github.madangryscientist.toggleslash.sdks.java.ErrorCode;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** The checked shape of a ruleset: what validation builds and the evaluator reads. */
final class Model {
  static final int MALFORMED = 0;
  static final int BROKEN = 1;
  static final int READY = 2;

  private Model() {}

  /** The ops of an attribute clause, and the segment op. */
  enum Op {
    IN,
    CONTAINS,
    STARTS_WITH,
    ENDS_WITH,
    LT,
    LTE,
    GT,
    GTE,
    SEMVER_EQ,
    SEMVER_LT,
    SEMVER_LTE,
    SEMVER_GT,
    SEMVER_GTE,
    BEFORE,
    AFTER,
    MATCHES,
    SEGMENT_MATCH
  }

  /**
   * A clause. For an attribute op, {@code values} holds Boolean, String and Double; for the segment
   * op, Strings, and {@code attribute} and {@code contextKind} are null.
   */
  record Clause(Op op, String attribute, List<Object> values, boolean negate, String contextKind) {}

  /** A rollout: parallel arrays of variation indexes and weights that sum to 100000. */
  record Rollout(String contextKind, int[] variations, int[] weights) {}

  /**
   * What a rule or the fallthrough serves: a variation, or a rollout when {@code rollout} is set.
   */
  record Serve(int variation, Rollout rollout) {}

  /** A rule: all clauses must hold. */
  record Rule(List<Clause> clauses, Serve serve) {}

  /** A target. */
  record Target(int variation, List<String> contextKeys, String contextKind) {}

  /** A prerequisite; {@code variation} is a count, not necessarily a valid index. */
  record Prerequisite(String flagKey, double variation) {}

  /** What serving the off variation needs. */
  record Header(String key, boolean on, List<Object> variations, Integer offVariation) {}

  /** A flag whose body is valid. */
  record Flag(
      Header header,
      String salt,
      Long version,
      List<Target> targets,
      List<Prerequisite> prerequisites,
      List<Rule> rules,
      Serve fallthrough) {}

  /** A target that lists a key: its place in {@code targets} and the variation it serves. */
  record TargetHit(int order, int variation) {}

  /** A flag entry of a ruleset: status is {@link #MALFORMED}, {@link #BROKEN} or {@link #READY}. */
  record Entry(
      int status,
      Header header,
      Flag flag,
      ErrorCode code,
      Map<String, Map<String, TargetHit>> targets) {}

  /** A segment rule. */
  record SegmentRule(List<Clause> clauses, Double weight, String rolloutContextKind) {}

  /** A segment, with its lists indexed by kind. */
  record Segment(
      String key,
      String salt,
      Map<String, Set<String>> included,
      Map<String, Set<String>> excluded,
      List<SegmentRule> rules) {}

  /** A segment entry: malformed when {@code segment} is null. */
  record SegmentEntry(Segment segment) {}
}
