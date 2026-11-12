package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.function.Function;

/**
 * One environment's flags and segments, validated and indexed once so each evaluation skips that
 * work. {@link #load} never throws: a bad flag becomes an {@code ERROR} result when it is
 * evaluated. The ruleset is immutable and shares no state with the document it was read from.
 */
final class Ruleset {
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

  enum ValueKind {
    STRING,
    NUMBER,
    BOOLEAN
  }

  /** A clause value: a string, a finite number or a boolean. */
  record ClauseValue(ValueKind kind, String string, double number, boolean bool) {}

  /**
   * An attribute clause or a segment clause. A segment clause's values are segment keys. {@code
   * contextKind} is the kind an attribute clause reads, null for {@code user}.
   */
  record Clause(
      Op op,
      String attribute,
      List<ClauseValue> values,
      List<String> segmentKeys,
      boolean negate,
      String contextKind) {}

  record WeightedVariation(int variation, double weight) {}

  /** Exactly one of {@code variation} or {@code rollout} is set. */
  record Serve(Integer variation, List<WeightedVariation> rollout, String rolloutKind) {}

  record Rule(List<Clause> clauses, Serve serve) {}

  record Target(int variation, List<String> contextKeys, String kind) {}

  /** A target that lists a key: its place in {@code targets} and the variation it serves. */
  record TargetHit(int order, int variation) {}

  record Prerequisite(String flagKey, double variation) {}

  /**
   * What serving the off variation needs; checked apart so {@code on: false} survives a bad rule.
   */
  record FlagHeader(String key, boolean on, List<Object> variations, Integer offVariation) {}

  record Flag(
      FlagHeader header,
      String salt,
      Map<String, Map<String, TargetHit>> targets,
      List<Prerequisite> prerequisites,
      List<Rule> rules,
      Serve fallthrough,
      double version) {}

  record SegmentRule(List<Clause> clauses, Double weight, String rolloutKind) {}

  /** {@code included} and {@code excluded} are keys by kind. */
  record Segment(
      String key,
      String salt,
      Map<String, Set<String>> included,
      Map<String, Set<String>> excluded,
      List<SegmentRule> rules) {}

  /** A flag as loaded: malformed (no header), broken (a code) or ready (a flag). */
  record FlagEntry(FlagHeader header, ErrorCode code, Flag flag) {}

  private final Map<String, FlagEntry> flags;
  // A null value is a malformed segment.
  private final Map<String, Segment> segments;

  private Ruleset(Map<String, FlagEntry> flags, Map<String, Segment> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  /** The ruleset with no flags and no segments. */
  static final Ruleset EMPTY = new Ruleset(Collections.emptyMap(), Collections.emptyMap());

  FlagEntry flag(String key) {
    return flags.get(key);
  }

  boolean hasSegment(String key) {
    return segments.containsKey(key);
  }

  /** The segment, or null when it is missing or malformed. */
  Segment segment(String key) {
    return segments.get(key);
  }

  int flagCount() {
    return flags.size();
  }

  /** The flag's version, or null when the ruleset has no such flag or the flag is malformed. */
  Double versionOf(String key) {
    FlagEntry entry = flags.get(key);
    return entry != null && entry.flag() != null ? entry.flag().version() : null;
  }

  /**
   * Loads a ruleset from its JSON form as {@link Json#parse} reads it. A root that is not an object
   * is an empty ruleset.
   *
   * @param root the parsed document
   * @return the ruleset
   */
  static Ruleset load(Object root) {
    Object rawFlags = null;
    Object rawSegments = null;
    if (root instanceof Map) {
      rawFlags = ((Map<?, ?>) root).get("flags");
      rawSegments = ((Map<?, ?>) root).get("segments");
    }
    Map<String, Segment> segments = indexByKey(rawSegments, Validation::readSegment);
    Map<String, FlagEntry> flags = indexByKey(rawFlags, item -> compileFlag(item, segments));
    return new Ruleset(flags, segments);
  }

  /**
   * First entry per key wins, even when it is the malformed one; an entry with no string key is
   * dropped.
   */
  private static <T> Map<String, T> indexByKey(Object items, Function<Map<?, ?>, T> compile) {
    Map<String, T> index = new HashMap<>();
    if (!(items instanceof List)) {
      return index;
    }
    for (Object item : (List<?>) items) {
      if (!(item instanceof Map)) {
        continue;
      }
      Object key = ((Map<?, ?>) item).get("key");
      if (!(key instanceof String)) {
        continue;
      }
      if (!index.containsKey(key)) {
        index.put((String) key, compile.apply((Map<?, ?>) item));
      }
    }
    return index;
  }

  private static FlagEntry compileFlag(Map<?, ?> value, Map<String, Segment> segments) {
    FlagHeader header = Validation.readHeader(value);
    if (header == null) {
      return new FlagEntry(null, ErrorCode.MALFORMED_FLAG, null);
    }
    Flag flag = Validation.readBody(value, header);
    if (flag == null) {
      return new FlagEntry(header, ErrorCode.MALFORMED_FLAG, null);
    }
    ErrorCode problem = segmentProblem(flag, segments);
    return problem != null
        ? new FlagEntry(header, problem, null)
        : new FlagEntry(header, null, flag);
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Flag flag, Map<String, Segment> segments) {
    for (Rule rule : flag.rules()) {
      for (Clause clause : rule.clauses()) {
        if (clause.op() != Op.SEGMENT_MATCH) {
          continue;
        }
        for (String key : clause.segmentKeys()) {
          if (!segments.containsKey(key)) {
            return ErrorCode.SEGMENT_NOT_FOUND;
          }
          if (segments.get(key) == null) {
            return ErrorCode.MALFORMED_SEGMENT;
          }
        }
      }
    }
    return null;
  }

  /** The shape of a ruleset, checked at runtime because rulesets arrive as JSON. */
  private static final class Validation {
    private Validation() {}

    private static boolean isInteger(double number) {
      return !Double.isNaN(number) && !Double.isInfinite(number) && number == Math.floor(number);
    }

    /** The value as a count (an integer, 0 or more), or -1 when it is not one. */
    private static double count(Object value) {
      if (!(value instanceof Double)) {
        return -1;
      }
      double number = (Double) value;
      return isInteger(number) && number >= 0 ? number : -1;
    }

    /** The value as an index below {@code variationCount}, or -1. */
    private static int index(Object value, int variationCount) {
      double count = count(value);
      return count < 0 || count >= variationCount ? -1 : (int) count;
    }

    private static String key(Object value) {
      return value instanceof String && !((String) value).isEmpty() ? (String) value : null;
    }

    /** Absent and null are true-and-false-free: returns 0 absent, 1 true, 2 false, -1 bad. */
    private static int negate(Object value) {
      if (value == null) {
        return 0;
      }
      if (!(value instanceof Boolean)) {
        return -1;
      }
      return (Boolean) value ? 1 : 2;
    }

    private static List<String> strings(Object value) {
      if (!(value instanceof List)) {
        return null;
      }
      List<String> result = new ArrayList<>();
      for (Object item : (List<?>) value) {
        if (!(item instanceof String)) {
          return null;
        }
        result.add((String) item);
      }
      return result;
    }

    private static ClauseValue clauseValue(Object value) {
      if (value instanceof String) {
        return new ClauseValue(ValueKind.STRING, (String) value, 0, false);
      }
      if (value instanceof Boolean) {
        return new ClauseValue(ValueKind.BOOLEAN, "", 0, (Boolean) value);
      }
      if (value instanceof Double) {
        double number = (Double) value;
        if (!Double.isNaN(number) && !Double.isInfinite(number)) {
          return new ClauseValue(ValueKind.NUMBER, "", number, false);
        }
      }
      return null;
    }

    private static Op attributeOp(Object op) {
      if (!(op instanceof String)) {
        return null;
      }
      switch ((String) op) {
        case "in":
          return Op.IN;
        case "contains":
          return Op.CONTAINS;
        case "startsWith":
          return Op.STARTS_WITH;
        case "endsWith":
          return Op.ENDS_WITH;
        case "lt":
          return Op.LT;
        case "lte":
          return Op.LTE;
        case "gt":
          return Op.GT;
        case "gte":
          return Op.GTE;
        case "semverEq":
          return Op.SEMVER_EQ;
        case "semverLt":
          return Op.SEMVER_LT;
        case "semverLte":
          return Op.SEMVER_LTE;
        case "semverGt":
          return Op.SEMVER_GT;
        case "semverGte":
          return Op.SEMVER_GTE;
        case "before":
          return Op.BEFORE;
        case "after":
          return Op.AFTER;
        case "matches":
          return Op.MATCHES;
        default:
          return null;
      }
    }

    /** An optional kind: absent is valid. Returns false when present and not a valid kind. */
    private static boolean optionalKindOk(Object value) {
      return value == null || (value instanceof String && ContextKinds.isKind((String) value));
    }

    private static Clause attributeClause(Object raw) {
      if (!(raw instanceof Map)) {
        return null;
      }
      Map<?, ?> value = (Map<?, ?>) raw;
      Op op = attributeOp(value.get("op"));
      if (op == null) {
        return null;
      }
      Object contextKind = value.get("contextKind");
      if (!optionalKindOk(contextKind)) {
        return null;
      }
      String attribute = key(value.get("attribute"));
      if (attribute == null) {
        return null;
      }
      if (attribute.startsWith("/") && AttributeReference.parse(attribute) == null) {
        return null;
      }
      int negate = negate(value.get("negate"));
      if (negate < 0 || !(value.get("values") instanceof List)) {
        return null;
      }
      List<ClauseValue> parsed = new ArrayList<>();
      for (Object item : (List<?>) value.get("values")) {
        ClauseValue clauseValue = clauseValue(item);
        if (clauseValue == null) {
          return null;
        }
        parsed.add(clauseValue);
      }
      return new Clause(op, attribute, parsed, List.of(), negate == 1, (String) contextKind);
    }

    private static Clause segmentClause(Object raw) {
      if (!(raw instanceof Map)) {
        return null;
      }
      Map<?, ?> value = (Map<?, ?>) raw;
      if (!"segmentMatch".equals(value.get("op"))) {
        return null;
      }
      int negate = negate(value.get("negate"));
      List<String> keys = strings(value.get("values"));
      if (negate < 0 || keys == null) {
        return null;
      }
      return new Clause(Op.SEGMENT_MATCH, "", List.of(), keys, negate == 1, null);
    }

    private static <T> List<T> readAll(Object value, Function<Object, T> read) {
      if (!(value instanceof List)) {
        return null;
      }
      List<T> items = new ArrayList<>();
      for (Object item : (List<?>) value) {
        T parsed = read.apply(item);
        if (parsed == null) {
          return null;
        }
        items.add(parsed);
      }
      return items;
    }

    private static Serve rolloutServe(Object raw, int variationCount) {
      if (!(raw instanceof Map)) {
        return null;
      }
      Map<?, ?> obj = (Map<?, ?>) raw;
      Object kind = obj.get("contextKind");
      if (!optionalKindOk(kind)) {
        return null;
      }
      Object variations = obj.get("variations");
      if (!(variations instanceof List) || ((List<?>) variations).isEmpty()) {
        return null;
      }
      List<WeightedVariation> rollout = new ArrayList<>();
      double total = 0;
      for (Object item : (List<?>) variations) {
        if (!(item instanceof Map)) {
          return null;
        }
        int index = index(((Map<?, ?>) item).get("variation"), variationCount);
        double weight = count(((Map<?, ?>) item).get("weight"));
        if (index < 0 || weight < 0 || weight > Bucketing.BUCKET_COUNT) {
          return null;
        }
        rollout.add(new WeightedVariation(index, weight));
        total += weight;
      }
      return total == Bucketing.BUCKET_COUNT ? new Serve(null, rollout, (String) kind) : null;
    }

    /** Exactly one of {@code variation} or {@code rollout}. */
    private static Serve serve(Object raw, int variationCount) {
      if (!(raw instanceof Map)) {
        return null;
      }
      Map<?, ?> value = (Map<?, ?>) raw;
      Object variation = value.get("variation");
      Object rollout = value.get("rollout");
      if (rollout == null) {
        int index = index(variation, variationCount);
        return index < 0 ? null : new Serve(index, null, null);
      }
      return variation == null ? rolloutServe(rollout, variationCount) : null;
    }

    private static Rule rule(Object raw, int variationCount) {
      if (!(raw instanceof Map)) {
        return null;
      }
      Map<?, ?> value = (Map<?, ?>) raw;
      List<Clause> clauses =
          readAll(
              value.get("clauses"),
              item -> {
                Clause attribute = attributeClause(item);
                return attribute != null ? attribute : segmentClause(item);
              });
      if (clauses == null) {
        return null;
      }
      Serve serve = serve(value, variationCount);
      return serve == null ? null : new Rule(clauses, serve);
    }

    /** What serving the off variation needs: key, on, variations and offVariation. */
    static FlagHeader readHeader(Map<?, ?> value) {
      Object on = value.get("on");
      if (!(on instanceof Boolean)) {
        return null;
      }
      String key = key(value.get("key"));
      if (key == null) {
        return null;
      }
      Object variations = value.get("variations");
      if (!(variations instanceof List) || ((List<?>) variations).isEmpty()) {
        return null;
      }
      List<Object> copy = new ArrayList<>((List<?>) variations);
      Integer off = null;
      Object offVariation = value.get("offVariation");
      if (offVariation != null) {
        int index = index(offVariation, copy.size());
        if (index < 0) {
          return null;
        }
        off = index;
      }
      return new FlagHeader(key, (Boolean) on, Collections.unmodifiableList(copy), off);
    }

    /** The rest of the flag, or null when anything in it is invalid. */
    static Flag readBody(Map<?, ?> value, FlagHeader header) {
      int count = header.variations().size();
      Object salt = value.get("salt");
      if (!(salt instanceof String)) {
        return null;
      }
      double version = count(value.get("version"));
      if (version < 0) {
        return null;
      }
      List<Target> rawTargets = readAll(value.get("targets"), item -> target(item, count));
      if (rawTargets == null) {
        return null;
      }
      Map<String, Map<String, TargetHit>> targets = new HashMap<>();
      for (int order = 0; order < rawTargets.size(); order++) {
        Target target = rawTargets.get(order);
        Map<String, TargetHit> keys = targets.computeIfAbsent(target.kind(), k -> new HashMap<>());
        for (String contextKey : target.contextKeys()) {
          keys.putIfAbsent(contextKey, new TargetHit(order, target.variation()));
        }
      }
      List<Prerequisite> prerequisites =
          readAll(value.get("prerequisites"), Validation::prerequisite);
      List<Rule> rules = readAll(value.get("rules"), item -> rule(item, count));
      if (prerequisites == null || rules == null) {
        return null;
      }
      Serve fallthrough = serve(value.get("fallthrough"), count);
      if (fallthrough == null) {
        return null;
      }
      return new Flag(header, (String) salt, targets, prerequisites, rules, fallthrough, version);
    }

    private static Target target(Object raw, int variationCount) {
      if (!(raw instanceof Map)) {
        return null;
      }
      Map<?, ?> value = (Map<?, ?>) raw;
      int variation = index(value.get("variation"), variationCount);
      if (variation < 0 || !optionalKindOk(value.get("contextKind"))) {
        return null;
      }
      List<String> keys = strings(value.get("contextKeys"));
      if (keys == null) {
        return null;
      }
      Object kind = value.get("contextKind");
      return new Target(variation, keys, kind == null ? ContextKinds.DEFAULT : (String) kind);
    }

    private static Prerequisite prerequisite(Object raw) {
      if (!(raw instanceof Map)) {
        return null;
      }
      Map<?, ?> value = (Map<?, ?>) raw;
      String flagKey = key(value.get("flagKey"));
      double variation = count(value.get("variation"));
      if (flagKey == null || variation < 0) {
        return null;
      }
      return new Prerequisite(flagKey, variation);
    }

    private static SegmentRule segmentRule(Object raw) {
      if (!(raw instanceof Map)) {
        return null;
      }
      Map<?, ?> value = (Map<?, ?>) raw;
      Object weight = value.get("weight");
      Double parsedWeight = null;
      if (weight != null) {
        double count = count(weight);
        if (count < 0 || count > Bucketing.BUCKET_COUNT) {
          return null;
        }
        parsedWeight = count;
      }
      Object rolloutKind = value.get("rolloutContextKind");
      if (!optionalKindOk(rolloutKind)) {
        return null;
      }
      List<Clause> clauses = readAll(value.get("clauses"), Validation::attributeClause);
      return clauses == null ? null : new SegmentRule(clauses, parsedWeight, (String) rolloutKind);
    }

    /**
     * Keys by kind: {@code userKeys} under {@code user}, merged with an optional array of {@code {
     * contextKind, values }}. Null when the array is present and malformed.
     */
    private static Map<String, Set<String>> keysByKind(List<String> userKeys, Object lists) {
      Map<String, Set<String>> index = new HashMap<>();
      index.put(ContextKinds.DEFAULT, new HashSet<>(userKeys));
      if (lists == null) {
        return index;
      }
      if (!(lists instanceof List)) {
        return null;
      }
      for (Object item : (List<?>) lists) {
        if (!(item instanceof Map)) {
          return null;
        }
        Object kind = ((Map<?, ?>) item).get("contextKind");
        if (!(kind instanceof String) || !ContextKinds.isKind((String) kind)) {
          return null;
        }
        List<String> values = strings(((Map<?, ?>) item).get("values"));
        if (values == null) {
          return null;
        }
        index.computeIfAbsent((String) kind, k -> new HashSet<>()).addAll(values);
      }
      return index;
    }

    /** A segment, or null when it is malformed. */
    static Segment readSegment(Map<?, ?> value) {
      String key = key(value.get("key"));
      Object salt = value.get("salt");
      if (key == null || !(salt instanceof String)) {
        return null;
      }
      List<String> included = strings(value.get("included"));
      List<String> excluded = strings(value.get("excluded"));
      if (included == null || excluded == null) {
        return null;
      }
      Map<String, Set<String>> includedByKind = keysByKind(included, value.get("includedContexts"));
      Map<String, Set<String>> excludedByKind = keysByKind(excluded, value.get("excludedContexts"));
      if (includedByKind == null || excludedByKind == null) {
        return null;
      }
      List<SegmentRule> rules = readAll(value.get("rules"), Validation::segmentRule);
      return rules == null
          ? null
          : new Segment(key, (String) salt, includedByKind, excludedByKind, rules);
    }
  }
}
