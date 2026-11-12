package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;

/** The validated shape of a ruleset: what {@link Validate} builds and {@link Evaluator} reads. */
final class Model {
  private Model() {}

  /** The attribute ops. */
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

    final String wire;

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

  /** A clause of a rule. */
  interface Clause {}

  /** One value of an attribute clause, with what its op needs parsed once. */
  static final class ClauseValue {
    /** The value: a String, a Double or a Boolean. */
    final Object raw;

    /** For the semver ops: the parsed version, or null. */
    Semver.Version version;

    /** For before and after: the instant, or NaN. */
    double instant = Double.NaN;

    /** For matches: the compiled pattern, or null when it is not valid. */
    Regex.Program program;

    ClauseValue(Object raw) {
      this.raw = raw;
    }
  }

  /** A clause on an attribute. */
  static final class AttributeClause implements Clause {
    final Op op;
    final List<String> path;
    final List<ClauseValue> values;
    final boolean negate;
    final String contextKind;

    AttributeClause(
        Op op, List<String> path, List<ClauseValue> values, boolean negate, String contextKind) {
      this.op = op;
      this.path = path;
      this.values = values;
      this.negate = negate;
      this.contextKind = contextKind;
    }
  }

  /** A clause on segment membership. */
  static final class SegmentClause implements Clause {
    final List<String> values;
    final boolean negate;

    SegmentClause(List<String> values, boolean negate) {
      this.values = values;
      this.negate = negate;
    }
  }

  /** A rollout: variations and weights that sum to 100000. */
  static final class Rollout {
    final int[] variations;
    final int[] weights;
    final String contextKind;

    Rollout(int[] variations, int[] weights, String contextKind) {
      this.variations = variations;
      this.weights = weights;
      this.contextKind = contextKind;
    }
  }

  /** What a rule or the fallthrough serves: a variation, or a rollout. */
  static final class Serve {
    final int variation;
    final Rollout rollout;

    Serve(int variation, Rollout rollout) {
      this.variation = variation;
      this.rollout = rollout;
    }
  }

  /** A rule. */
  static final class Rule {
    final List<Clause> clauses;
    final Serve serve;

    Rule(List<Clause> clauses, Serve serve) {
      this.clauses = clauses;
      this.serve = serve;
    }
  }

  /** A target. */
  static final class Target {
    final int variation;
    final List<String> contextKeys;
    final String contextKind;

    Target(int variation, List<String> contextKeys, String contextKind) {
      this.variation = variation;
      this.contextKeys = contextKeys;
      this.contextKind = contextKind;
    }
  }

  /** A prerequisite. */
  static final class Prerequisite {
    final String flagKey;
    final int variation;

    Prerequisite(String flagKey, int variation) {
      this.flagKey = flagKey;
      this.variation = variation;
    }
  }

  /** What serving the off variation needs. */
  static final class Header {
    final String key;
    final boolean on;
    final List<?> variations;
    final int offVariation;

    Header(String key, boolean on, List<?> variations, int offVariation) {
      this.key = key;
      this.on = on;
      this.variations = variations;
      this.offVariation = offVariation;
    }
  }

  /** The rest of a flag. */
  static final class Flag {
    final Header header;
    final String salt;
    final double version;
    final List<Target> targets;
    final List<Prerequisite> prerequisites;
    final List<Rule> rules;
    final Serve fallthrough;

    Flag(
        Header header,
        String salt,
        double version,
        List<Target> targets,
        List<Prerequisite> prerequisites,
        List<Rule> rules,
        Serve fallthrough) {
      this.header = header;
      this.salt = salt;
      this.version = version;
      this.targets = targets;
      this.prerequisites = prerequisites;
      this.rules = rules;
      this.fallthrough = fallthrough;
    }
  }

  /** The keys of one kind that a segment includes or excludes. */
  static final class ContextList {
    final String contextKind;
    final List<String> values;

    ContextList(String contextKind, List<String> values) {
      this.contextKind = contextKind;
      this.values = values;
    }
  }

  /** A segment rule. {@code weight} is -1 when absent. */
  static final class SegmentRule {
    final List<AttributeClause> clauses;
    final int weight;
    final String rolloutContextKind;

    SegmentRule(List<AttributeClause> clauses, int weight, String rolloutContextKind) {
      this.clauses = clauses;
      this.weight = weight;
      this.rolloutContextKind = rolloutContextKind;
    }
  }

  /** A segment. */
  static final class Segment {
    final String key;
    final String salt;
    final List<String> included;
    final List<String> excluded;
    final List<ContextList> includedContexts;
    final List<ContextList> excludedContexts;
    final List<SegmentRule> rules;

    Segment(
        String key,
        String salt,
        List<String> included,
        List<String> excluded,
        List<ContextList> includedContexts,
        List<ContextList> excludedContexts,
        List<SegmentRule> rules) {
      this.key = key;
      this.salt = salt;
      this.included = included;
      this.excluded = excluded;
      this.includedContexts = includedContexts;
      this.excludedContexts = excludedContexts;
      this.rules = rules;
    }
  }
}
