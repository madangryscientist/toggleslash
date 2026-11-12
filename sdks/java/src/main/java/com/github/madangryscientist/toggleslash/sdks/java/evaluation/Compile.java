package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rollout;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Target;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Checks the shape of a flag or segment as the spec says it must be, and builds the typed model.
 * Each method returns null for a value that is malformed. Null and absent are the same for an
 * optional field.
 */
final class Compile {
  private Compile() {}

  @SuppressWarnings("unchecked")
  private static List<Object> list(Object value) {
    return (List<Object>) value;
  }

  /**
   * What serving the off variation needs; checked apart so {@code on: false} survives a bad rule.
   */
  static Header header(Object value) {
    if (!Values.isRecord(value) || !(Values.get(value, "on") instanceof Boolean on)) {
      return null;
    }
    Object variations = Values.get(value, "variations");
    if (!Values.isKey(Values.get(value, "key"))
        || !(variations instanceof List<?> all)
        || all.isEmpty()) {
      return null;
    }
    Object off = Values.get(value, "offVariation");
    if (!Values.isAbsent(off) && !Values.isVariationIndex(off, all.size())) {
      return null;
    }
    return new Header(
        (String) Values.get(value, "key"),
        on,
        list(variations),
        Values.isAbsent(off) ? null : Values.asInt(off));
  }

  private static boolean isAttributeName(Object value) {
    if (!Values.isKey(value)) {
      return false;
    }
    String name = (String) value;
    return !name.startsWith("/") || Reference.parse(name) != null;
  }

  private static Object prepare(Op op, Object value) {
    switch (op) {
      case MATCHES:
        return value instanceof String s ? Pattern.compile(s) : null;
      case SEMVER_EQ:
      case SEMVER_LT:
      case SEMVER_LTE:
      case SEMVER_GT:
      case SEMVER_GTE:
        return value instanceof String s ? Semver.parse(s) : null;
      case BEFORE:
      case AFTER:
        return Dates.toInstant(value instanceof Number n ? (Object) n.doubleValue() : value);
      default:
        return null;
    }
  }

  private static AttributeClause attributeClause(Object value) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Op op = Op.of(Values.get(value, "op"));
    Object values = Values.get(value, "values");
    if (op == null
        || !isAttributeName(Values.get(value, "attribute"))
        || !Values.isNegate(Values.get(value, "negate"))
        || !Values.isOptionalKind(Values.get(value, "contextKind"))
        || !(values instanceof List<?> items)) {
      return null;
    }
    List<Object> normal = new ArrayList<>();
    List<Object> prepared = new ArrayList<>();
    for (Object item : items) {
      if (!Values.isClauseValue(item)) {
        return null;
      }
      Object v = item instanceof Number n ? (Object) n.doubleValue() : item;
      normal.add(v);
      prepared.add(prepare(op, v));
    }
    String attribute = (String) Values.get(value, "attribute");
    return new AttributeClause(
        op,
        attribute,
        attribute.startsWith("/") ? Reference.parse(attribute) : List.of(attribute),
        normal,
        prepared,
        Boolean.TRUE.equals(Values.get(value, "negate")),
        (String) Values.get(value, "contextKind"));
  }

  private static SegmentClause segmentClause(Object value) {
    if (!Values.isRecord(value)
        || !"segmentMatch".equals(Values.get(value, "op"))
        || !Values.isNegate(Values.get(value, "negate"))
        || !Values.isStringList(Values.get(value, "values"))) {
      return null;
    }
    List<String> keys = new ArrayList<>();
    for (Object key : list(Values.get(value, "values"))) {
      keys.add((String) key);
    }
    return new SegmentClause(keys, Boolean.TRUE.equals(Values.get(value, "negate")));
  }

  private static Rollout rollout(Object value, int variationCount) {
    if (!Values.isRecord(value) || !Values.isOptionalKind(Values.get(value, "contextKind"))) {
      return null;
    }
    if (!(Values.get(value, "variations") instanceof List<?> items) || items.isEmpty()) {
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
      Object variation = Values.get(item, "variation");
      Object weight = Values.get(item, "weight");
      if (!Values.isVariationIndex(variation, variationCount)
          || !Values.isCount(weight)
          || ((Number) weight).doubleValue() > Bucket.COUNT) {
        return null;
      }
      variations[i] = Values.asInt(variation);
      weights[i] = Values.asInt(weight);
      total += weights[i];
    }
    if (total != Bucket.COUNT) {
      return null;
    }
    return new Rollout(variations, weights, (String) Values.get(value, "contextKind"));
  }

  private static Serve serve(Object value, int variationCount) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Object variation = Values.get(value, "variation");
    Object rollout = Values.get(value, "rollout");
    if (Values.isAbsent(rollout)) {
      return Values.isVariationIndex(variation, variationCount)
          ? new Serve(Values.asInt(variation), null)
          : null;
    }
    Rollout parsed = Values.isAbsent(variation) ? rollout(rollout, variationCount) : null;
    return parsed == null ? null : new Serve(-1, parsed);
  }

  private static Rule rule(Object value, int variationCount) {
    if (!Values.isRecord(value) || !(Values.get(value, "clauses") instanceof List<?> raw)) {
      return null;
    }
    List<Clause> clauses = new ArrayList<>();
    for (Object item : raw) {
      Clause clause = attributeClause(item);
      if (clause == null) {
        clause = segmentClause(item);
      }
      if (clause == null) {
        return null;
      }
      clauses.add(clause);
    }
    Serve serve = serve(value, variationCount);
    return serve == null ? null : new Rule(clauses, serve);
  }

  private static Target target(Object value, int variationCount) {
    if (!Values.isRecord(value)
        || !Values.isVariationIndex(Values.get(value, "variation"), variationCount)
        || !Values.isStringList(Values.get(value, "contextKeys"))
        || !Values.isOptionalKind(Values.get(value, "contextKind"))) {
      return null;
    }
    List<String> keys = new ArrayList<>();
    for (Object key : list(Values.get(value, "contextKeys"))) {
      keys.add((String) key);
    }
    return new Target(
        Values.asInt(Values.get(value, "variation")),
        keys,
        (String) Values.get(value, "contextKind"));
  }

  private static Prerequisite prerequisite(Object value) {
    if (!Values.isRecord(value)
        || !Values.isKey(Values.get(value, "flagKey"))
        || !Values.isCount(Values.get(value, "variation"))) {
      return null;
    }
    double variation = ((Number) Values.get(value, "variation")).doubleValue();
    // A variation beyond int range can never equal a served index.
    return new Prerequisite(
        (String) Values.get(value, "flagKey"),
        variation > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) variation);
  }

  private static Map<String, Map<String, int[]>> targetIndex(List<Target> targets) {
    Map<String, Map<String, int[]>> index = new HashMap<>();
    for (int order = 0; order < targets.size(); order++) {
      Target target = targets.get(order);
      String kind = target.contextKind() == null ? Contexts.DEFAULT_KIND : target.contextKind();
      Map<String, int[]> keys = index.computeIfAbsent(kind, k -> new HashMap<>());
      for (String key : target.contextKeys()) {
        keys.putIfAbsent(key, new int[] {order, target.variation()});
      }
    }
    return index;
  }

  /** The flag with the header already checked, or null when the body is malformed. */
  static Flag flag(Object value, Header header) {
    int count = header.variations().size();
    Object salt = Values.get(value, "salt");
    if (!(salt instanceof String) || !Values.isCount(Values.get(value, "version"))) {
      return null;
    }
    List<Target> targets = new ArrayList<>();
    if (!(Values.get(value, "targets") instanceof List<?> rawTargets)) {
      return null;
    }
    for (Object item : rawTargets) {
      Target target = target(item, count);
      if (target == null) {
        return null;
      }
      targets.add(target);
    }
    List<Prerequisite> prerequisites = new ArrayList<>();
    if (!(Values.get(value, "prerequisites") instanceof List<?> rawPrerequisites)) {
      return null;
    }
    for (Object item : rawPrerequisites) {
      Prerequisite prerequisite = prerequisite(item);
      if (prerequisite == null) {
        return null;
      }
      prerequisites.add(prerequisite);
    }
    List<Rule> rules = new ArrayList<>();
    if (!(Values.get(value, "rules") instanceof List<?> rawRules)) {
      return null;
    }
    for (Object item : rawRules) {
      Rule rule = rule(item, count);
      if (rule == null) {
        return null;
      }
      rules.add(rule);
    }
    Serve fallthrough = serve(Values.get(value, "fallthrough"), count);
    if (fallthrough == null) {
      return null;
    }
    return new Flag(
        header,
        (String) salt,
        ((Number) Values.get(value, "version")).doubleValue(),
        prerequisites,
        rules,
        fallthrough,
        targetIndex(targets));
  }

  private static SegmentRule segmentRule(Object value) {
    if (!Values.isRecord(value)) {
      return null;
    }
    Object weight = Values.get(value, "weight");
    boolean weightIsValid =
        Values.isAbsent(weight)
            || (Values.isCount(weight) && ((Number) weight).doubleValue() <= Bucket.COUNT);
    if (!weightIsValid
        || !Values.isOptionalKind(Values.get(value, "rolloutContextKind"))
        || !(Values.get(value, "clauses") instanceof List<?> raw)) {
      return null;
    }
    List<AttributeClause> clauses = new ArrayList<>();
    for (Object item : raw) {
      AttributeClause clause = attributeClause(item);
      if (clause == null) {
        return null;
      }
      clauses.add(clause);
    }
    return new SegmentRule(
        clauses,
        Values.isAbsent(weight) ? null : Values.asInt(weight),
        (String) Values.get(value, "rolloutContextKind"));
  }

  /**
   * The context lists of one side of a segment: the user keys, then each list's kind and values.
   */
  private static Map<String, Set<String>> keysByKind(Object userKeys, Object lists) {
    Map<String, Set<String>> index = new HashMap<>();
    Set<String> users = new HashSet<>();
    for (Object key : list(userKeys)) {
      users.add((String) key);
    }
    index.put(Contexts.DEFAULT_KIND, users);
    if (lists instanceof List<?> entries) {
      for (Object entry : entries) {
        Set<String> keys =
            index.computeIfAbsent((String) Values.get(entry, "contextKind"), k -> new HashSet<>());
        for (Object key : list(Values.get(entry, "values"))) {
          keys.add((String) key);
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
          || !Values.isKind(Values.get(item, "contextKind"))
          || !Values.isStringList(Values.get(item, "values"))) {
        return false;
      }
    }
    return true;
  }

  /** The segment, or null when it is malformed. */
  static Segment segment(Object value) {
    if (!Values.isRecord(value)
        || !Values.isKey(Values.get(value, "key"))
        || !(Values.get(value, "salt") instanceof String salt)
        || !Values.isStringList(Values.get(value, "included"))
        || !Values.isStringList(Values.get(value, "excluded"))
        || !isContextLists(Values.get(value, "includedContexts"))
        || !isContextLists(Values.get(value, "excludedContexts"))
        || !(Values.get(value, "rules") instanceof List<?> rawRules)) {
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
        (String) Values.get(value, "key"),
        salt,
        keysByKind(Values.get(value, "included"), Values.get(value, "includedContexts")),
        keysByKind(Values.get(value, "excluded"), Values.get(value, "excludedContexts")),
        rules);
  }
}
