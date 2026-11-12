package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * A ruleset, validated and indexed once so each evaluation skips that work. It never throws: a bad
 * flag becomes an {@code ERROR} result when it is evaluated.
 */
public final class Ruleset {
  /** The attribute operators. */
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

  /** A clause of a rule or a segment rule. */
  sealed interface Clause permits AttributeClause, SegmentClause {}

  /**
   * An attribute clause.
   *
   * @param op the operator
   * @param path the attribute name as one component, or the components of a reference
   * @param values the clause values: Double, String or Boolean
   * @param negate whether the any-match is flipped
   * @param contextKind the kind it reads, null for user
   */
  record AttributeClause(
      Op op, List<String> path, List<Object> values, boolean negate, String contextKind)
      implements Clause {}

  /**
   * A segment clause.
   *
   * @param values the segment keys
   * @param negate whether the any-match is flipped
   */
  record SegmentClause(List<String> values, boolean negate) implements Clause {}

  /**
   * A percentage rollout.
   *
   * @param contextKind the kind it buckets, null for user
   * @param variations the variation indexes
   * @param weights the weights, in the same order, summing to 100000
   */
  record Rollout(String contextKind, int[] variations, int[] weights) {}

  /**
   * What a rule or the fallthrough serves.
   *
   * @param variation the variation index, or -1 with a rollout
   * @param rollout the rollout, or null
   */
  record Serve(int variation, Rollout rollout) {}

  /**
   * A rule.
   *
   * @param clauses its clauses
   * @param serve what it serves
   */
  record Rule(List<Clause> clauses, Serve serve) {}

  /**
   * A prerequisite.
   *
   * @param flagKey the flag
   * @param variation the variation it must serve
   */
  record Prerequisite(String flagKey, int variation) {}

  /**
   * A valid flag.
   *
   * @param key its key
   * @param on whether it is on
   * @param variations its variations
   * @param offVariation the off variation, or -1
   * @param salt its salt
   * @param prerequisites its prerequisites
   * @param rules its rules
   * @param fallthrough its fallthrough
   */
  record Flag(
      String key,
      boolean on,
      List<Object> variations,
      int offVariation,
      String salt,
      List<Prerequisite> prerequisites,
      List<Rule> rules,
      Serve fallthrough) {}

  /**
   * A segment rule.
   *
   * @param clauses its attribute clauses
   * @param weight its weight, or -1 for none
   * @param rolloutContextKind the kind it buckets, null for user
   */
  record SegmentRule(List<AttributeClause> clauses, int weight, String rolloutContextKind) {}

  /** A valid segment, with its listed keys by kind. */
  record Segment(
      String key,
      String salt,
      Map<String, Set<String>> included,
      Map<String, Set<String>> excluded,
      List<SegmentRule> rules) {}

  /** A target that lists a key: its place in {@code targets} and the variation it serves. */
  record TargetHit(int order, int variation) {}

  /** How far a flag entry can be evaluated. */
  enum Status {
    MALFORMED,
    BROKEN,
    READY
  }

  /** A flag entry as the ruleset holds it. */
  static final class FlagEntry {
    final Status status;
    final List<Object> variations;
    final boolean on;
    final int offVariation;
    final ErrorCode code;
    final Flag flag;
    final Map<String, Map<String, TargetHit>> targets;
    final Long version;

    FlagEntry(
        Status status,
        List<Object> variations,
        boolean on,
        int offVariation,
        ErrorCode code,
        Flag flag,
        Map<String, Map<String, TargetHit>> targets,
        Long version) {
      this.status = status;
      this.variations = variations;
      this.on = on;
      this.offVariation = offVariation;
      this.code = code;
      this.flag = flag;
      this.targets = targets;
      this.version = version;
    }
  }

  final Map<String, FlagEntry> flags;
  final Map<String, Segment> segments;
  // A segment key that is present but malformed.
  final Set<String> malformedSegments;

  private Ruleset(
      Map<String, FlagEntry> flags, Map<String, Segment> segments, Set<String> malformedSegments) {
    this.flags = flags;
    this.segments = segments;
    this.malformedSegments = malformedSegments;
  }

  /**
   * The version of a flag, for the hook event.
   *
   * @param key the flag key
   * @return its version, or null when the flag is missing or has none
   */
  public Long flagVersion(String key) {
    FlagEntry entry = flags.get(key);
    return entry == null ? null : entry.version;
  }

  /**
   * Builds a ruleset from parsed JSON.
   *
   * @param input the ruleset as the JSON reader gives it
   * @return the ruleset; an input that is not an object, or without arrays, is empty
   */
  public static Ruleset create(Object input) {
    Object rawFlags = null;
    Object rawSegments = null;
    if (Shape.isRecord(input)) {
      rawFlags = Shape.get(input, "flags");
      rawSegments = Shape.get(input, "segments");
    }
    Map<String, Segment> segments = new HashMap<>();
    Set<String> malformed = new HashSet<>();
    Set<String> seenSegments = new HashSet<>();
    if (rawSegments instanceof List<?> list) {
      for (Object item : list) {
        Object key = Shape.isRecord(item) ? Shape.get(item, "key") : null;
        if (key instanceof String k && seenSegments.add(k)) {
          Segment segment = compileSegment(item);
          if (segment == null) {
            malformed.add(k);
          } else {
            segments.put(k, segment);
          }
        }
      }
    }
    Ruleset partial = new Ruleset(new HashMap<>(), segments, malformed);
    if (rawFlags instanceof List<?> list) {
      Set<String> seen = new HashSet<>();
      for (Object item : list) {
        Object key = Shape.isRecord(item) ? Shape.get(item, "key") : null;
        if (key instanceof String k && seen.add(k)) {
          partial.flags.put(k, compileFlag(item, partial));
        }
      }
    }
    return partial;
  }

  // ---- validation ----

  private static boolean isVariationIndex(Object value, int count) {
    return Shape.isCount(value) && (Double) value < count;
  }

  private static boolean isClauseValue(Object value) {
    if (value instanceof Double d) {
      return !d.isInfinite() && !d.isNaN();
    }
    return value instanceof String || value instanceof Boolean;
  }

  private static boolean isNegate(Object value) {
    return Shape.isAbsent(value) || value instanceof Boolean;
  }

  private static boolean isAttributeName(Object value) {
    if (!Shape.isKey(value)) {
      return false;
    }
    String s = (String) value;
    return !s.startsWith("/") || Shape.parseReference(s) != null;
  }

  private static boolean negate(Object value) {
    return Boolean.TRUE.equals(value);
  }

  /** Reads an attribute clause, or null when it is not one. */
  private static AttributeClause attributeClause(Object value) {
    if (!Shape.isRecord(value)) {
      return null;
    }
    Op op = Op.of(Shape.get(value, "op"));
    Object values = Shape.get(value, "values");
    Object attribute = Shape.get(value, "attribute");
    if (op == null
        || !isAttributeName(attribute)
        || !isNegate(Shape.get(value, "negate"))
        || !Shape.isOptionalKind(Shape.get(value, "contextKind"))
        || !(values instanceof List<?> list)) {
      return null;
    }
    List<Object> clauseValues = new ArrayList<>();
    for (Object item : list) {
      if (!isClauseValue(item)) {
        return null;
      }
      clauseValues.add(item);
    }
    String name = (String) attribute;
    List<String> path = name.startsWith("/") ? Shape.parseReference(name) : List.of(name);
    return new AttributeClause(
        op,
        path,
        clauseValues,
        negate(Shape.get(value, "negate")),
        (String) Shape.get(value, "contextKind"));
  }

  private static SegmentClause segmentClause(Object value) {
    if (!Shape.isRecord(value) || !"segmentMatch".equals(Shape.get(value, "op"))) {
      return null;
    }
    Object values = Shape.get(value, "values");
    if (!isNegate(Shape.get(value, "negate")) || !Shape.isStringArray(values)) {
      return null;
    }
    List<String> keys = new ArrayList<>();
    for (Object item : (List<?>) values) {
      keys.add((String) item);
    }
    return new SegmentClause(keys, negate(Shape.get(value, "negate")));
  }

  private static Rollout rollout(Object value, int variationCount) {
    if (!Shape.isRecord(value) || !Shape.isOptionalKind(Shape.get(value, "contextKind"))) {
      return null;
    }
    Object variations = Shape.get(value, "variations");
    if (!(variations instanceof List<?> list) || list.isEmpty()) {
      return null;
    }
    int[] indexes = new int[list.size()];
    int[] weights = new int[list.size()];
    long total = 0;
    for (int i = 0; i < list.size(); i++) {
      Object item = list.get(i);
      if (!Shape.isRecord(item)) {
        return null;
      }
      Object weight = Shape.get(item, "weight");
      Object variation = Shape.get(item, "variation");
      if (!isVariationIndex(variation, variationCount)
          || !Shape.isCount(weight)
          || (Double) weight > Bucket.COUNT) {
        return null;
      }
      indexes[i] = (int) (double) (Double) variation;
      weights[i] = (int) (double) (Double) weight;
      total += weights[i];
    }
    if (total != Bucket.COUNT) {
      return null;
    }
    return new Rollout((String) Shape.get(value, "contextKind"), indexes, weights);
  }

  private static Serve serve(Object value, int variationCount) {
    if (!Shape.isRecord(value)) {
      return null;
    }
    Object variation = Shape.get(value, "variation");
    Object rollout = Shape.get(value, "rollout");
    if (Shape.isAbsent(rollout)) {
      return isVariationIndex(variation, variationCount)
          ? new Serve((int) (double) (Double) variation, null)
          : null;
    }
    if (!Shape.isAbsent(variation)) {
      return null;
    }
    Rollout r = rollout(rollout, variationCount);
    return r == null ? null : new Serve(-1, r);
  }

  private static Rule rule(Object value, int variationCount) {
    if (!Shape.isRecord(value)) {
      return null;
    }
    Object clauses = Shape.get(value, "clauses");
    if (!(clauses instanceof List<?> list)) {
      return null;
    }
    List<Clause> parsed = new ArrayList<>();
    for (Object item : list) {
      Clause clause = attributeClause(item);
      if (clause == null) {
        clause = segmentClause(item);
      }
      if (clause == null) {
        return null;
      }
      parsed.add(clause);
    }
    Serve serve = serve(value, variationCount);
    return serve == null ? null : new Rule(parsed, serve);
  }

  /**
   * What serving the off variation needs: checked apart so {@code on: false} survives a bad rule.
   */
  private static boolean isFlagHeader(Object value) {
    if (!Shape.isRecord(value) || !(Shape.get(value, "on") instanceof Boolean)) {
      return false;
    }
    Object variations = Shape.get(value, "variations");
    if (!Shape.isKey(Shape.get(value, "key"))
        || !(variations instanceof List<?> list)
        || list.isEmpty()) {
      return false;
    }
    Object off = Shape.get(value, "offVariation");
    return Shape.isAbsent(off) || isVariationIndex(off, list.size());
  }

  private static FlagEntry compileFlag(Object value, Ruleset ruleset) {
    if (!isFlagHeader(value)) {
      return new FlagEntry(
          Status.MALFORMED, null, false, -1, ErrorCode.MALFORMED_FLAG, null, null, null);
    }
    @SuppressWarnings("unchecked")
    List<Object> variations = (List<Object>) Shape.get(value, "variations");
    boolean on = (Boolean) Shape.get(value, "on");
    Object off = Shape.get(value, "offVariation");
    int offVariation = off == null ? -1 : (int) (double) (Double) off;
    Flag flag = flagBody(value, variations, on, offVariation);
    if (flag == null) {
      return new FlagEntry(
          Status.BROKEN, variations, on, offVariation, ErrorCode.MALFORMED_FLAG, null, null, null);
    }
    long version = (long) (double) (Double) Shape.get(value, "version");
    ErrorCode problem = segmentProblem(flag, ruleset);
    if (problem != null) {
      return new FlagEntry(
          Status.BROKEN, variations, on, offVariation, problem, null, null, version);
    }
    Map<String, Map<String, TargetHit>> targets =
        targetIndex((List<?>) Shape.get(value, "targets"));
    return new FlagEntry(Status.READY, variations, on, offVariation, null, flag, targets, version);
  }

  private static Flag flagBody(
      Object value, List<Object> variations, boolean on, int offVariation) {
    int count = variations.size();
    Object salt = Shape.get(value, "salt");
    if (!(salt instanceof String) || !Shape.isCount(Shape.get(value, "version"))) {
      return null;
    }
    Object targets = Shape.get(value, "targets");
    Object prerequisites = Shape.get(value, "prerequisites");
    Object rules = Shape.get(value, "rules");
    if (!(targets instanceof List<?> targetList)
        || !(prerequisites instanceof List<?> prerequisiteList)
        || !(rules instanceof List<?> ruleList)) {
      return null;
    }
    for (Object target : targetList) {
      if (!isTarget(target, count)) {
        return null;
      }
    }
    List<Prerequisite> prerequisitesOut = new ArrayList<>();
    for (Object item : prerequisiteList) {
      if (!Shape.isRecord(item)
          || !Shape.isKey(Shape.get(item, "flagKey"))
          || !Shape.isCount(Shape.get(item, "variation"))) {
        return null;
      }
      prerequisitesOut.add(
          new Prerequisite(
              (String) Shape.get(item, "flagKey"),
              clampCount((Double) Shape.get(item, "variation"))));
    }
    List<Rule> rulesOut = new ArrayList<>();
    for (Object item : ruleList) {
      Rule rule = rule(item, count);
      if (rule == null) {
        return null;
      }
      rulesOut.add(rule);
    }
    Serve fallthrough = serve(Shape.get(value, "fallthrough"), count);
    if (fallthrough == null) {
      return null;
    }
    return new Flag(
        (String) Shape.get(value, "key"),
        on,
        variations,
        offVariation,
        (String) salt,
        prerequisitesOut,
        rulesOut,
        fallthrough);
  }

  /** A count as an int, saturating: a prerequisite variation past the int range matches nothing. */
  private static int clampCount(double d) {
    return d > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) d;
  }

  private static boolean isTarget(Object value, int count) {
    return Shape.isRecord(value)
        && isVariationIndex(Shape.get(value, "variation"), count)
        && Shape.isStringArray(Shape.get(value, "contextKeys"))
        && Shape.isOptionalKind(Shape.get(value, "contextKind"));
  }

  private static Map<String, Map<String, TargetHit>> targetIndex(List<?> targets) {
    Map<String, Map<String, TargetHit>> index = new HashMap<>();
    for (int order = 0; order < targets.size(); order++) {
      Object target = targets.get(order);
      Object kindValue = Shape.get(target, "contextKind");
      String kind = kindValue == null ? Contexts.DEFAULT_KIND : (String) kindValue;
      int variation = (int) (double) (Double) Shape.get(target, "variation");
      Map<String, TargetHit> keys = index.computeIfAbsent(kind, k -> new HashMap<>());
      for (Object contextKey : (List<?>) Shape.get(target, "contextKeys")) {
        keys.putIfAbsent((String) contextKey, new TargetHit(order, variation));
      }
    }
    return index;
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Flag flag, Ruleset ruleset) {
    for (Rule rule : flag.rules()) {
      for (Clause clause : rule.clauses()) {
        if (clause instanceof SegmentClause segment) {
          for (String key : segment.values()) {
            if (ruleset.malformedSegments.contains(key)) {
              return ErrorCode.MALFORMED_SEGMENT;
            }
            if (!ruleset.segments.containsKey(key)) {
              return ErrorCode.SEGMENT_NOT_FOUND;
            }
          }
        }
      }
    }
    return null;
  }

  // ---- segments ----

  private static Map<String, Set<String>> keysByKind(Object userKeys, Object lists) {
    Map<String, Set<String>> index = new HashMap<>();
    Set<String> users = new HashSet<>();
    for (Object key : (List<?>) userKeys) {
      users.add((String) key);
    }
    index.put(Contexts.DEFAULT_KIND, users);
    if (lists instanceof List<?> list) {
      for (Object entry : list) {
        String kind = (String) Shape.get(entry, "contextKind");
        Set<String> keys = index.computeIfAbsent(kind, k -> new HashSet<>());
        for (Object value : (List<?>) Shape.get(entry, "values")) {
          keys.add((String) value);
        }
      }
    }
    return index;
  }

  private static boolean isOptionalContextLists(Object value) {
    if (Shape.isAbsent(value)) {
      return true;
    }
    if (!(value instanceof List<?> list)) {
      return false;
    }
    for (Object item : list) {
      if (!Shape.isRecord(item)
          || !Shape.isKind(Shape.get(item, "contextKind"))
          || !Shape.isStringArray(Shape.get(item, "values"))) {
        return false;
      }
    }
    return true;
  }

  private static Segment compileSegment(Object value) {
    if (!Shape.isKey(Shape.get(value, "key"))
        || !(Shape.get(value, "salt") instanceof String)
        || !Shape.isStringArray(Shape.get(value, "included"))
        || !Shape.isStringArray(Shape.get(value, "excluded"))
        || !isOptionalContextLists(Shape.get(value, "includedContexts"))
        || !isOptionalContextLists(Shape.get(value, "excludedContexts"))
        || !(Shape.get(value, "rules") instanceof List<?> rules)) {
      return null;
    }
    List<SegmentRule> parsed = new ArrayList<>();
    for (Object item : rules) {
      if (!Shape.isRecord(item)) {
        return null;
      }
      Object weight = Shape.get(item, "weight");
      boolean weightOk =
          Shape.isAbsent(weight) || (Shape.isCount(weight) && (Double) weight <= Bucket.COUNT);
      if (!weightOk
          || !Shape.isOptionalKind(Shape.get(item, "rolloutContextKind"))
          || !(Shape.get(item, "clauses") instanceof List<?> clauses)) {
        return null;
      }
      List<AttributeClause> parsedClauses = new ArrayList<>();
      for (Object clause : clauses) {
        AttributeClause attribute = attributeClause(clause);
        if (attribute == null) {
          return null;
        }
        parsedClauses.add(attribute);
      }
      parsed.add(
          new SegmentRule(
              parsedClauses,
              weight == null ? -1 : (int) (double) (Double) weight,
              (String) Shape.get(item, "rolloutContextKind")));
    }
    return new Segment(
        (String) Shape.get(value, "key"),
        (String) Shape.get(value, "salt"),
        keysByKind(Shape.get(value, "included"), Shape.get(value, "includedContexts")),
        keysByKind(Shape.get(value, "excluded"), Shape.get(value, "excludedContexts")),
        parsed);
  }

  /**
   * Tells whether the context has, in any of its kinds, a key that the lists hold for that kind.
   *
   * @param keys the keys by kind
   * @param contexts the context
   * @return whether one is listed
   */
  static boolean listsContext(
      Map<String, Set<String>> keys, Map<String, Contexts.KindContext> contexts) {
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.entrySet()) {
      Set<String> set = keys.get(entry.getKey());
      if (set != null && set.contains(entry.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  /**
   * The variation of the first target in array order that matches the context.
   *
   * @param targets the target index
   * @param contexts the context
   * @return the variation, or -1 for none
   */
  static int targetedVariation(
      Map<String, Map<String, TargetHit>> targets, Map<String, Contexts.KindContext> contexts) {
    TargetHit first = null;
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.entrySet()) {
      Map<String, TargetHit> byKey = targets.get(entry.getKey());
      TargetHit hit = byKey == null ? null : byKey.get(entry.getValue().key());
      if (hit != null && (first == null || hit.order() < first.order())) {
        first = hit;
      }
    }
    return first == null ? -1 : first.variation();
  }
}
