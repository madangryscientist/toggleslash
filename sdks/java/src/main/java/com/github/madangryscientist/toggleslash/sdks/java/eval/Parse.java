package com.github.madangryscientist.toggleslash.sdks.java.eval;

import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Op;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Rollout;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Target;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Checks the shape of a flag or segment, which arrives as JSON, and builds the typed form. Each
 * method answers null for a value that is malformed. Null and absent are the same for an optional
 * field.
 */
final class Parse {
  private static final Map<String, Op> OPS = new HashMap<>();

  static {
    OPS.put("in", Op.IN);
    OPS.put("contains", Op.CONTAINS);
    OPS.put("startsWith", Op.STARTS_WITH);
    OPS.put("endsWith", Op.ENDS_WITH);
    OPS.put("lt", Op.LT);
    OPS.put("lte", Op.LTE);
    OPS.put("gt", Op.GT);
    OPS.put("gte", Op.GTE);
    OPS.put("semverEq", Op.SEMVER_EQ);
    OPS.put("semverLt", Op.SEMVER_LT);
    OPS.put("semverLte", Op.SEMVER_LTE);
    OPS.put("semverGt", Op.SEMVER_GT);
    OPS.put("semverGte", Op.SEMVER_GTE);
    OPS.put("before", Op.BEFORE);
    OPS.put("after", Op.AFTER);
    OPS.put("matches", Op.MATCHES);
  }

  private Parse() {}

  private static boolean isClauseValue(Object value) {
    if (value instanceof Double d) {
      return Double.isFinite(d);
    }
    return value instanceof String || value instanceof Boolean;
  }

  private static boolean isAttributeName(Object value) {
    if (!Values.isKey(value)) {
      return false;
    }
    String name = (String) value;
    return !name.startsWith("/") || Reference.parse(name) != null;
  }

  private static Clause attributeClause(Object value) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    Object opName = map.get("op");
    Op op = opName instanceof String name ? OPS.get(name) : null;
    Object values = map.get("values");
    if (op == null
        || !isAttributeName(map.get("attribute"))
        || !Values.isNegate(map.get("negate"))
        || !Values.isOptionalKind(map.get("contextKind"))
        || !(values instanceof List<?> items)) {
      return null;
    }
    for (Object item : items) {
      if (!isClauseValue(item)) {
        return null;
      }
    }
    return new Clause(
        op,
        (String) map.get("attribute"),
        Values.list(values),
        map.get("negate") == Boolean.TRUE,
        (String) map.get("contextKind"));
  }

  private static Clause segmentClause(Object value) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    if (!"segmentMatch".equals(map.get("op"))
        || !Values.isNegate(map.get("negate"))
        || !Values.isStringArray(map.get("values"))) {
      return null;
    }
    return new Clause(
        Op.SEGMENT_MATCH,
        null,
        Values.list(map.get("values")),
        map.get("negate") == Boolean.TRUE,
        null);
  }

  private static Rollout rollout(Object value, int variationCount) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    if (!Values.isOptionalKind(map.get("contextKind"))) {
      return null;
    }
    if (!(map.get("variations") instanceof List<?> items) || items.isEmpty()) {
      return null;
    }
    int[] variations = new int[items.size()];
    int[] weights = new int[items.size()];
    long total = 0;
    for (int i = 0; i < items.size(); i++) {
      Object item = items.get(i);
      if (!Values.isRecord(item)) {
        return null;
      }
      Map<String, Object> entry = Values.record(item);
      Object weight = entry.get("weight");
      if (!Values.isVariationIndex(entry.get("variation"), variationCount)
          || !Values.isCount(weight)
          || (Double) weight > Values.BUCKET_COUNT) {
        return null;
      }
      variations[i] = Values.toInt(entry.get("variation"));
      weights[i] = Values.toInt(weight);
      total += weights[i];
    }
    if (total != Values.BUCKET_COUNT) {
      return null;
    }
    return new Rollout((String) map.get("contextKind"), variations, weights);
  }

  private static Serve serve(Map<String, Object> map, int variationCount) {
    Object variation = map.get("variation");
    Object rollout = map.get("rollout");
    if (Values.isAbsent(rollout)) {
      return Values.isVariationIndex(variation, variationCount)
          ? new Serve(Values.toInt(variation), null)
          : null;
    }
    if (!Values.isAbsent(variation)) {
      return null;
    }
    Rollout parsed = rollout(rollout, variationCount);
    return parsed == null ? null : new Serve(0, parsed);
  }

  private static Rule rule(Object value, int variationCount) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    if (!(map.get("clauses") instanceof List<?> items)) {
      return null;
    }
    List<Clause> clauses = new ArrayList<>(items.size());
    for (Object item : items) {
      Clause clause = attributeClause(item);
      if (clause == null) {
        clause = segmentClause(item);
      }
      if (clause == null) {
        return null;
      }
      clauses.add(clause);
    }
    Serve serve = serve(map, variationCount);
    return serve == null ? null : new Rule(clauses, serve);
  }

  private static Target target(Object value, int variationCount) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    if (!Values.isVariationIndex(map.get("variation"), variationCount)
        || !Values.isStringArray(map.get("contextKeys"))
        || !Values.isOptionalKind(map.get("contextKind"))) {
      return null;
    }
    List<String> keys = new ArrayList<>();
    for (Object key : Values.list(map.get("contextKeys"))) {
      keys.add((String) key);
    }
    return new Target(Values.toInt(map.get("variation")), keys, (String) map.get("contextKind"));
  }

  private static Prerequisite prerequisite(Object value) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    if (!Values.isKey(map.get("flagKey")) || !Values.isCount(map.get("variation"))) {
      return null;
    }
    return new Prerequisite((String) map.get("flagKey"), (Double) map.get("variation"));
  }

  /**
   * What serving the off variation needs. It is checked apart so {@code on: false} survives a bad
   * rule.
   *
   * @param value a flag as JSON
   * @return the header, or null
   */
  static Header header(Object value) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    if (!(map.get("on") instanceof Boolean on)
        || !Values.isKey(map.get("key"))
        || !(map.get("variations") instanceof List<?> variations)
        || variations.isEmpty()) {
      return null;
    }
    Object off = map.get("offVariation");
    if (!Values.isAbsent(off) && !Values.isVariationIndex(off, variations.size())) {
      return null;
    }
    return new Header(
        (String) map.get("key"),
        on,
        Values.list(map.get("variations")),
        Values.isAbsent(off) ? null : Values.toInt(off));
  }

  /**
   * A flag whose header is valid, with its body, or null when the body is malformed.
   *
   * @param header the header
   * @param value the flag as JSON
   * @return the flag or null
   */
  static Flag flag(Header header, Object value) {
    Map<String, Object> map = Values.record(value);
    int count = header.variations().size();
    if (!(map.get("salt") instanceof String salt)
        || !Values.isCount(map.get("version"))
        || !(map.get("targets") instanceof List<?> rawTargets)
        || !(map.get("prerequisites") instanceof List<?> rawPrerequisites)
        || !(map.get("rules") instanceof List<?> rawRules)) {
      return null;
    }
    List<Target> targets = new ArrayList<>();
    for (Object item : rawTargets) {
      Target target = target(item, count);
      if (target == null) {
        return null;
      }
      targets.add(target);
    }
    List<Prerequisite> prerequisites = new ArrayList<>();
    for (Object item : rawPrerequisites) {
      Prerequisite prerequisite = prerequisite(item);
      if (prerequisite == null) {
        return null;
      }
      prerequisites.add(prerequisite);
    }
    List<Rule> rules = new ArrayList<>();
    for (Object item : rawRules) {
      Rule rule = rule(item, count);
      if (rule == null) {
        return null;
      }
      rules.add(rule);
    }
    Serve fallthrough =
        Values.isRecord(map.get("fallthrough"))
            ? serve(Values.record(map.get("fallthrough")), count)
            : null;
    if (fallthrough == null) {
      return null;
    }
    double version = (Double) map.get("version");
    Long v = version >= 0x1p63 ? null : Long.valueOf((long) version);
    return new Flag(header, salt, v, targets, prerequisites, rules, fallthrough);
  }

  private static Map<String, Set<String>> keysByKind(Object userKeys, Object lists) {
    Map<String, Set<String>> index = new HashMap<>();
    Set<String> users = new HashSet<>();
    for (Object key : Values.list(userKeys)) {
      users.add((String) key);
    }
    index.put("user", users);
    if (lists != null) {
      for (Object item : Values.list(lists)) {
        Map<String, Object> list = Values.record(item);
        Set<String> keys =
            index.computeIfAbsent((String) list.get("contextKind"), k -> new HashSet<>());
        for (Object value : Values.list(list.get("values"))) {
          keys.add((String) value);
        }
      }
    }
    return index;
  }

  private static boolean isContextLists(Object value) {
    if (Values.isAbsent(value)) {
      return true;
    }
    if (!(value instanceof List<?> items)) {
      return false;
    }
    for (Object item : items) {
      if (!Values.isRecord(item)
          || !Values.isKind(Values.record(item).get("contextKind"))
          || !Values.isStringArray(Values.record(item).get("values"))) {
        return false;
      }
    }
    return true;
  }

  private static SegmentRule segmentRule(Object value) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    Object weight = map.get("weight");
    boolean weightOk =
        Values.isAbsent(weight)
            || (Values.isCount(weight) && (Double) weight <= Values.BUCKET_COUNT);
    if (!weightOk
        || !Values.isOptionalKind(map.get("rolloutContextKind"))
        || !(map.get("clauses") instanceof List<?> items)) {
      return null;
    }
    List<Clause> clauses = new ArrayList<>();
    for (Object item : items) {
      Clause clause = attributeClause(item);
      if (clause == null) {
        return null;
      }
      clauses.add(clause);
    }
    return new SegmentRule(clauses, (Double) weight, (String) map.get("rolloutContextKind"));
  }

  /**
   * A segment, or null when it is malformed.
   *
   * @param value the segment as JSON
   * @return the segment or null
   */
  static Segment segment(Object value) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Map<String, Object> map = Values.record(value);
    if (!Values.isKey(map.get("key"))
        || !(map.get("salt") instanceof String salt)
        || !Values.isStringArray(map.get("included"))
        || !Values.isStringArray(map.get("excluded"))
        || !isContextLists(map.get("includedContexts"))
        || !isContextLists(map.get("excludedContexts"))
        || !(map.get("rules") instanceof List<?> rawRules)) {
      return null;
    }
    List<SegmentRule> rules = new ArrayList<>();
    for (Object item : rawRules) {
      SegmentRule rule = segmentRule(item);
      if (rule == null) {
        return null;
      }
      rules.add(rule);
    }
    return new Segment(
        (String) map.get("key"),
        salt,
        keysByKind(map.get("included"), map.get("includedContexts")),
        keysByKind(map.get("excluded"), map.get("excludedContexts")),
        rules);
  }
}
