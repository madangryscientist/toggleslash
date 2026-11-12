package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** The validated shape of a ruleset. */
final class Model {
  private Model() {}

  /** A clause operator. */
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

  /** The kind of a clause value. */
  enum ValueKind {
    STRING,
    NUMBER,
    BOOLEAN
  }

  /** A clause value: a string, a finite number or a boolean. */
  record ClauseValue(ValueKind kind, String string, double number, boolean bool) {}

  /**
   * An attribute clause or a segment clause. A segment clause's values are segment keys.
   *
   * @param contextKind the kind an attribute clause reads, null for user
   */
  record Clause(
      Op op,
      String attribute,
      List<ClauseValue> values,
      List<String> segmentKeys,
      boolean negate,
      String contextKind) {}

  /** One entry of a rollout. */
  record WeightedVariation(int variation, double weight) {}

  /**
   * What a rule or fallthrough serves: exactly one of variation or rollout.
   *
   * @param rolloutKind the kind a rollout buckets by, null for user
   */
  record Serve(Integer variation, List<WeightedVariation> rollout, String rolloutKind) {}

  /** A flag rule. */
  record Rule(List<Clause> clauses, Serve serve) {}

  /** A flag target. */
  record Target(int variation, List<String> contextKeys, String kind) {}

  /** A target that lists a key: its place in the targets and the variation it serves. */
  record TargetHit(int order, int variation) {}

  /** A prerequisite. */
  record Prerequisite(String flagKey, double variation) {}

  /** What serving the off variation needs; checked apart so an off flag survives a bad rule. */
  record FlagHeader(String key, boolean on, List<Json> variations, Integer offVariation) {}

  /** A valid flag. */
  record Flag(
      FlagHeader header,
      String salt,
      double version,
      Map<String, Map<String, TargetHit>> targets,
      List<Prerequisite> prerequisites,
      List<Rule> rules,
      Serve fallthrough) {}

  /** A segment rule. */
  record SegmentRule(List<Clause> clauses, Double weight, String rolloutKind) {}

  /** A valid segment; included and excluded are key sets by kind. */
  record Segment(
      String key,
      String salt,
      Map<String, Set<String>> included,
      Map<String, Set<String>> excluded,
      List<SegmentRule> rules) {}

  /** A flag as loaded. */
  sealed interface FlagEntry permits Malformed, Broken, Ready {}

  /** The header is invalid: not even the off variation can be served. */
  record Malformed() implements FlagEntry {}

  /** The header is fine but the body is not, or a segment it names is missing or malformed. */
  record Broken(FlagHeader header, ErrorCode code) implements FlagEntry {}

  /** A flag that can be evaluated. */
  record Ready(Flag flag) implements FlagEntry {}
}
