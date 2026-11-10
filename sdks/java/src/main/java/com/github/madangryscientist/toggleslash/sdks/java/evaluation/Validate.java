package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.function.Predicate;

/**
 * The shape of a ruleset, checked at run time because rulesets arrive as JSON. {@code
 * docs/evaluation.md} is normative for what counts as malformed. Null and absent are the same for
 * an optional field. A value here is a JSON tree: {@code Map}, {@code List}, {@code String}, {@code
 * Double}, {@code Boolean} or null.
 */
final class Validate {
  static final int BUCKET_COUNT = 100_000;

  private static final Set<String> ATTRIBUTE_OPS =
      Set.of(
          "in",
          "contains",
          "startsWith",
          "endsWith",
          "lt",
          "lte",
          "gt",
          "gte",
          "semverEq",
          "semverLt",
          "semverLte",
          "semverGt",
          "semverGte",
          "before",
          "after",
          "matches");

  private Validate() {}

  static boolean isRecord(Object value) {
    return value instanceof Map;
  }

  /** The record's field, or null. The caller has checked {@link #isRecord}. */
  static Object field(Object record, String name) {
    return ((Map<?, ?>) record).get(name);
  }

  /** ASCII only: {@code [A-Za-z0-9._-]+}, and neither {@code kind} nor {@code multi}. */
  static boolean isKind(Object value) {
    if (!(value instanceof String text) || text.isEmpty()) {
      return false;
    }
    for (int i = 0; i < text.length(); i++) {
      char c = text.charAt(i);
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
    return !text.equals("kind") && !text.equals("multi");
  }

  private static boolean isOptionalKind(Object value) {
    return value == null || isKind(value);
  }

  private static boolean isKey(Object value) {
    return value instanceof String text && !text.isEmpty();
  }

  /** An integer of at least 0. */
  static boolean isCount(Object value) {
    return value instanceof Double d && Double.isFinite(d) && d == Math.floor(d) && d >= 0;
  }

  private static boolean isVariationIndex(Object value, int variationCount) {
    return isCount(value) && (Double) value < variationCount;
  }

  private static boolean isStringArray(Object value) {
    return value instanceof List<?> list && list.stream().allMatch(item -> item instanceof String);
  }

  private static boolean isClauseValue(Object value) {
    if (value instanceof Double d) {
      return Double.isFinite(d);
    }
    return value instanceof String || value instanceof Boolean;
  }

  private static boolean isNegate(Object value) {
    return value == null || value instanceof Boolean;
  }

  private static boolean isAttributeName(Object value) {
    if (!isKey(value)) {
      return false;
    }
    String name = (String) value;
    return !name.startsWith("/") || Reference.parse(name) != null;
  }

  static boolean isAttributeClause(Object value) {
    if (!isRecord(value)
        || !(field(value, "op") instanceof String op)
        || !ATTRIBUTE_OPS.contains(op)) {
      return false;
    }
    return isAttributeName(field(value, "attribute"))
        && isNegate(field(value, "negate"))
        && isOptionalKind(field(value, "contextKind"))
        && field(value, "values") instanceof List<?> values
        && values.stream().allMatch(Validate::isClauseValue);
  }

  private static boolean isSegmentClause(Object value) {
    if (!isRecord(value) || !"segmentMatch".equals(field(value, "op"))) {
      return false;
    }
    return isNegate(field(value, "negate")) && isStringArray(field(value, "values"));
  }

  private static boolean isWeightedVariation(Object value, int variationCount) {
    return isRecord(value)
        && isVariationIndex(field(value, "variation"), variationCount)
        && isCount(field(value, "weight"))
        && (Double) field(value, "weight") <= BUCKET_COUNT;
  }

  private static boolean isRollout(Object value, int variationCount) {
    if (!isRecord(value) || !isOptionalKind(field(value, "contextKind"))) {
      return false;
    }
    if (!(field(value, "variations") instanceof List<?> variations) || variations.isEmpty()) {
      return false;
    }
    double total = 0;
    for (Object item : variations) {
      if (!isWeightedVariation(item, variationCount)) {
        return false;
      }
      total += (Double) field(item, "weight");
    }
    return total == BUCKET_COUNT;
  }

  /** {@code variation} (an index) or {@code rollout}, not both. */
  static boolean isServe(Object value, int variationCount) {
    if (!isRecord(value)) {
      return false;
    }
    Object variation = field(value, "variation");
    Object rollout = field(value, "rollout");
    if (rollout == null) {
      return isVariationIndex(variation, variationCount);
    }
    return variation == null && isRollout(rollout, variationCount);
  }

  private static boolean isRule(Object value, int variationCount) {
    if (!isRecord(value) || !(field(value, "clauses") instanceof List<?> clauses)) {
      return false;
    }
    return clauses.stream().allMatch(c -> isAttributeClause(c) || isSegmentClause(c))
        && isServe(value, variationCount);
  }

  private static boolean isTarget(Object value, int variationCount) {
    return isRecord(value)
        && isVariationIndex(field(value, "variation"), variationCount)
        && isStringArray(field(value, "contextKeys"))
        && isOptionalKind(field(value, "contextKind"));
  }

  private static boolean isPrerequisite(Object value) {
    return isRecord(value) && isKey(field(value, "flagKey")) && isCount(field(value, "variation"));
  }

  /** What serving the off variation needs. It is checked apart so {@code on: false} survives. */
  static boolean isFlagHeader(Object value) {
    if (!isRecord(value) || !(field(value, "on") instanceof Boolean)) {
      return false;
    }
    if (!isKey(field(value, "key"))
        || !(field(value, "variations") instanceof List<?> variations)
        || variations.isEmpty()) {
      return false;
    }
    Object offVariation = field(value, "offVariation");
    return offVariation == null || isVariationIndex(offVariation, variations.size());
  }

  private static boolean everyItem(Object value, Predicate<Object> check) {
    return value instanceof List<?> list && list.stream().allMatch(check);
  }

  static boolean isFlagBody(Object value, int variationCount) {
    if (!isRecord(value)) {
      return false;
    }
    return field(value, "salt") instanceof String
        && isCount(field(value, "version"))
        && everyItem(field(value, "targets"), item -> isTarget(item, variationCount))
        && everyItem(field(value, "prerequisites"), Validate::isPrerequisite)
        && everyItem(field(value, "rules"), item -> isRule(item, variationCount))
        && isServe(field(value, "fallthrough"), variationCount);
  }

  private static boolean isSegmentRule(Object value) {
    if (!isRecord(value)) {
      return false;
    }
    Object weight = field(value, "weight");
    boolean weightIsValid = weight == null || (isCount(weight) && (Double) weight <= BUCKET_COUNT);
    return weightIsValid
        && isOptionalKind(field(value, "rolloutContextKind"))
        && everyItem(field(value, "clauses"), Validate::isAttributeClause);
  }

  private static boolean isContextList(Object value) {
    return isRecord(value)
        && isKind(field(value, "contextKind"))
        && isStringArray(field(value, "values"));
  }

  private static boolean isOptionalContextLists(Object value) {
    return value == null || everyItem(value, Validate::isContextList);
  }

  static boolean isSegment(Object value) {
    if (!isRecord(value)) {
      return false;
    }
    return isKey(field(value, "key"))
        && field(value, "salt") instanceof String
        && isStringArray(field(value, "included"))
        && isStringArray(field(value, "excluded"))
        && isOptionalContextLists(field(value, "includedContexts"))
        && isOptionalContextLists(field(value, "excludedContexts"))
        && everyItem(field(value, "rules"), Validate::isSegmentRule);
  }
}
