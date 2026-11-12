package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.ClauseValue;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.ContextList;
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
import java.util.List;
import java.util.Map;

/**
 * The shape of a ruleset, checked at runtime because rulesets arrive as JSON. Each method returns
 * the model for a valid value and {@code null} for an invalid one. Null and absent are the same for
 * an optional field.
 */
final class Validate {
  private Validate() {}

  static boolean isRecord(Object value) {
    return value instanceof Map;
  }

  static boolean isKey(Object value) {
    return value instanceof String && !((String) value).isEmpty();
  }

  /** {@code ^[A-Za-z0-9._-]+$}, and neither {@code kind} nor {@code multi}. */
  static boolean isKind(Object value) {
    if (!(value instanceof String)) {
      return false;
    }
    String s = (String) value;
    if (s.isEmpty() || s.equals("kind") || s.equals("multi")) {
      return false;
    }
    for (int i = 0; i < s.length(); i++) {
      char c = s.charAt(i);
      boolean ok =
          (c >= 'A' && c <= 'Z')
              || (c >= 'a' && c <= 'z')
              || (c >= '0' && c <= '9')
              || c == '.'
              || c == '_'
              || c == '-';
      if (!ok) {
        return false;
      }
    }
    return true;
  }

  private static boolean isOptionalKind(Object value) {
    return value == null || isKind(value);
  }

  /** A finite whole number at least 0. */
  static boolean isCount(Object value) {
    if (!(value instanceof Number)) {
      return false;
    }
    double d = ((Number) value).doubleValue();
    return Double.isFinite(d) && d == Math.floor(d) && d >= 0;
  }

  private static boolean isVariationIndex(Object value, int variationCount) {
    return isCount(value) && ((Number) value).doubleValue() < variationCount;
  }

  private static Object get(Object record, String name) {
    return ((Map<?, ?>) record).get(name);
  }

  private static List<String> stringList(Object value) {
    if (!(value instanceof List)) {
      return null;
    }
    List<String> out = new ArrayList<>();
    for (Object item : (List<?>) value) {
      if (!(item instanceof String)) {
        return null;
      }
      out.add((String) item);
    }
    return out;
  }

  private static boolean isNegate(Object value) {
    return value == null || value instanceof Boolean;
  }

  private static boolean isAttributeName(Object value) {
    if (!isKey(value)) {
      return false;
    }
    String s = (String) value;
    return !s.startsWith("/") || Reference.parse(s) != null;
  }

  private static ClauseValue clauseValue(Object value, Op op) {
    if (value instanceof Number) {
      double d = ((Number) value).doubleValue();
      if (!Double.isFinite(d)) {
        return null;
      }
      ClauseValue cv = new ClauseValue(d);
      if (op == Op.BEFORE || op == Op.AFTER) {
        cv.instant = d;
      }
      return cv;
    }
    if (value instanceof Boolean) {
      return new ClauseValue(value);
    }
    if (!(value instanceof String)) {
      return null;
    }
    String s = (String) value;
    ClauseValue cv = new ClauseValue(s);
    switch (op) {
      case SEMVER_EQ:
      case SEMVER_LT:
      case SEMVER_LTE:
      case SEMVER_GT:
      case SEMVER_GTE:
        cv.version = Semver.parse(s);
        break;
      case BEFORE:
      case AFTER:
        cv.instant = Dates.parseInstant(s);
        break;
      case MATCHES:
        cv.program = Regex.compile(s);
        break;
      default:
        break;
    }
    return cv;
  }

  static AttributeClause attributeClause(Object value) {
    if (!isRecord(value)) {
      return null;
    }
    Op op = Op.of(get(value, "op"));
    Object values = get(value, "values");
    Object attribute = get(value, "attribute");
    if (op == null
        || !isAttributeName(attribute)
        || !isNegate(get(value, "negate"))
        || !isOptionalKind(get(value, "contextKind"))
        || !(values instanceof List)) {
      return null;
    }
    List<ClauseValue> parsed = new ArrayList<>();
    for (Object item : (List<?>) values) {
      ClauseValue cv = clauseValue(item, op);
      if (cv == null) {
        return null;
      }
      parsed.add(cv);
    }
    String name = (String) attribute;
    List<String> path = name.startsWith("/") ? Reference.parse(name) : List.of(name);
    return new AttributeClause(
        op,
        path,
        parsed,
        Boolean.TRUE.equals(get(value, "negate")),
        (String) get(value, "contextKind"));
  }

  private static SegmentClause segmentClause(Object value) {
    if (!isRecord(value) || !"segmentMatch".equals(get(value, "op"))) {
      return null;
    }
    List<String> values = stringList(get(value, "values"));
    if (!isNegate(get(value, "negate")) || values == null) {
      return null;
    }
    return new SegmentClause(values, Boolean.TRUE.equals(get(value, "negate")));
  }

  private static Rollout rollout(Object value, int variationCount) {
    if (!isRecord(value) || !isOptionalKind(get(value, "contextKind"))) {
      return null;
    }
    Object variations = get(value, "variations");
    if (!(variations instanceof List) || ((List<?>) variations).isEmpty()) {
      return null;
    }
    List<?> list = (List<?>) variations;
    int[] indexes = new int[list.size()];
    int[] weights = new int[list.size()];
    long total = 0;
    for (int i = 0; i < list.size(); i++) {
      Object item = list.get(i);
      if (!isRecord(item)) {
        return null;
      }
      Object variation = get(item, "variation");
      Object weight = get(item, "weight");
      if (!isVariationIndex(variation, variationCount)
          || !isCount(weight)
          || ((Number) weight).doubleValue() > Bucket.COUNT) {
        return null;
      }
      indexes[i] = ((Number) variation).intValue();
      weights[i] = ((Number) weight).intValue();
      total += weights[i];
    }
    if (total != Bucket.COUNT) {
      return null;
    }
    return new Rollout(indexes, weights, (String) get(value, "contextKind"));
  }

  private static Serve serve(Object value, int variationCount) {
    if (!isRecord(value)) {
      return null;
    }
    Object variation = get(value, "variation");
    Object rollout = get(value, "rollout");
    if (rollout == null) {
      return isVariationIndex(variation, variationCount)
          ? new Serve(((Number) variation).intValue(), null)
          : null;
    }
    if (variation != null) {
      return null;
    }
    Rollout parsed = rollout(rollout, variationCount);
    return parsed == null ? null : new Serve(-1, parsed);
  }

  private static Rule rule(Object value, int variationCount) {
    if (!isRecord(value)) {
      return null;
    }
    Object clauses = get(value, "clauses");
    if (!(clauses instanceof List)) {
      return null;
    }
    List<Clause> parsed = new ArrayList<>();
    for (Object item : (List<?>) clauses) {
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

  private static Target target(Object value, int variationCount) {
    if (!isRecord(value)) {
      return null;
    }
    Object variation = get(value, "variation");
    List<String> keys = stringList(get(value, "contextKeys"));
    if (!isVariationIndex(variation, variationCount)
        || keys == null
        || !isOptionalKind(get(value, "contextKind"))) {
      return null;
    }
    return new Target(((Number) variation).intValue(), keys, (String) get(value, "contextKind"));
  }

  private static Prerequisite prerequisite(Object value) {
    if (!isRecord(value) || !isKey(get(value, "flagKey")) || !isCount(get(value, "variation"))) {
      return null;
    }
    double variation = ((Number) get(value, "variation")).doubleValue();
    // A variation index past the int range matches no index; keep it out of range.
    int index = variation > Integer.MAX_VALUE ? Integer.MAX_VALUE : (int) variation;
    return new Prerequisite((String) get(value, "flagKey"), index);
  }

  /** What serving the off variation needs; checked apart so on false survives a bad rule. */
  static Header header(Object value) {
    if (!isRecord(value) || !(get(value, "on") instanceof Boolean)) {
      return null;
    }
    Object variations = get(value, "variations");
    if (!isKey(get(value, "key")) || !(variations instanceof List)) {
      return null;
    }
    List<?> list = (List<?>) variations;
    if (list.isEmpty()) {
      return null;
    }
    Object off = get(value, "offVariation");
    int offIndex = -1;
    if (off != null) {
      if (!isVariationIndex(off, list.size())) {
        return null;
      }
      offIndex = ((Number) off).intValue();
    }
    return new Header((String) get(value, "key"), (Boolean) get(value, "on"), list, offIndex);
  }

  /** The body of a flag whose header is valid, or null. */
  static Flag body(Object value, Header header) {
    int count = header.variations.size();
    Object salt = get(value, "salt");
    Object version = get(value, "version");
    if (!(salt instanceof String) || !isCount(version)) {
      return null;
    }
    Object targets = get(value, "targets");
    Object prerequisites = get(value, "prerequisites");
    Object rules = get(value, "rules");
    if (!(targets instanceof List)
        || !(prerequisites instanceof List)
        || !(rules instanceof List)) {
      return null;
    }
    List<Target> parsedTargets = new ArrayList<>();
    for (Object item : (List<?>) targets) {
      Target t = target(item, count);
      if (t == null) {
        return null;
      }
      parsedTargets.add(t);
    }
    List<Prerequisite> parsedPrerequisites = new ArrayList<>();
    for (Object item : (List<?>) prerequisites) {
      Prerequisite p = prerequisite(item);
      if (p == null) {
        return null;
      }
      parsedPrerequisites.add(p);
    }
    List<Rule> parsedRules = new ArrayList<>();
    for (Object item : (List<?>) rules) {
      Rule r = rule(item, count);
      if (r == null) {
        return null;
      }
      parsedRules.add(r);
    }
    Serve fallthrough = serve(get(value, "fallthrough"), count);
    if (fallthrough == null) {
      return null;
    }
    return new Flag(
        header,
        (String) salt,
        ((Number) version).doubleValue(),
        parsedTargets,
        parsedPrerequisites,
        parsedRules,
        fallthrough);
  }

  private static SegmentRule segmentRule(Object value) {
    if (!isRecord(value)) {
      return null;
    }
    Object weight = get(value, "weight");
    int parsedWeight = -1;
    if (weight != null) {
      if (!isCount(weight) || ((Number) weight).doubleValue() > Bucket.COUNT) {
        return null;
      }
      parsedWeight = ((Number) weight).intValue();
    }
    Object clauses = get(value, "clauses");
    if (!isOptionalKind(get(value, "rolloutContextKind")) || !(clauses instanceof List)) {
      return null;
    }
    List<AttributeClause> parsed = new ArrayList<>();
    for (Object item : (List<?>) clauses) {
      AttributeClause clause = attributeClause(item);
      if (clause == null) {
        return null;
      }
      parsed.add(clause);
    }
    return new SegmentRule(parsed, parsedWeight, (String) get(value, "rolloutContextKind"));
  }

  /** Optional context lists: absent gives an empty list, an invalid one gives null. */
  private static List<ContextList> contextLists(Object value) {
    List<ContextList> out = new ArrayList<>();
    if (value == null) {
      return out;
    }
    if (!(value instanceof List)) {
      return null;
    }
    for (Object item : (List<?>) value) {
      if (!isRecord(item) || !isKind(get(item, "contextKind"))) {
        return null;
      }
      List<String> values = stringList(get(item, "values"));
      if (values == null) {
        return null;
      }
      out.add(new ContextList((String) get(item, "contextKind"), values));
    }
    return out;
  }

  static Segment segment(Object value) {
    if (!isRecord(value) || !isKey(get(value, "key")) || !(get(value, "salt") instanceof String)) {
      return null;
    }
    List<String> included = stringList(get(value, "included"));
    List<String> excluded = stringList(get(value, "excluded"));
    List<ContextList> includedContexts = contextLists(get(value, "includedContexts"));
    List<ContextList> excludedContexts = contextLists(get(value, "excludedContexts"));
    Object rules = get(value, "rules");
    if (included == null
        || excluded == null
        || includedContexts == null
        || excludedContexts == null
        || !(rules instanceof List)) {
      return null;
    }
    List<SegmentRule> parsedRules = new ArrayList<>();
    for (Object item : (List<?>) rules) {
      SegmentRule rule = segmentRule(item);
      if (rule == null) {
        return null;
      }
      parsedRules.add(rule);
    }
    return new Segment(
        (String) get(value, "key"),
        (String) get(value, "salt"),
        included,
        excluded,
        includedContexts,
        excludedContexts,
        parsedRules);
  }
}
