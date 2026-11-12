package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.Set;
import java.util.function.Predicate;

/**
 * The shape of a ruleset, checked at runtime because rulesets arrive as JSON. {@code
 * docs/evaluation.md} is normative for what counts as malformed. Null and absent are the same for
 * an optional member.
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

  /** ASCII {@code [A-Za-z0-9._-]+}, and neither {@code kind} nor {@code multi}. */
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
          (c >= 'a' && c <= 'z')
              || (c >= 'A' && c <= 'Z')
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

  static boolean isKey(Object value) {
    return value instanceof String && !((String) value).isEmpty();
  }

  static boolean isCount(Object value) {
    if (!Js.isNumber(value)) {
      return false;
    }
    double d = Js.number(value);
    return Double.isFinite(d) && d == Math.rint(d) && d >= 0;
  }

  static boolean isVariationIndex(Object value, int variationCount) {
    return isCount(value) && Js.number(value) < variationCount;
  }

  private static boolean isStringArray(Object value) {
    return everyItem(value, item -> item instanceof String);
  }

  private static boolean isClauseValue(Object value) {
    if (Js.isNumber(value)) {
      return Js.isFinite(value);
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
    String s = (String) value;
    return !s.startsWith("/") || Reference.parse(s) != null;
  }

  private static boolean everyItem(Object value, Predicate<Object> check) {
    if (!Js.isArray(value)) {
      return false;
    }
    for (Object item : Js.array(value)) {
      if (!check.test(item)) {
        return false;
      }
    }
    return true;
  }

  static boolean isAttributeClause(Object value) {
    if (!Js.isRecord(value)) {
      return false;
    }
    Object op = Js.member(value, "op");
    if (!(op instanceof String) || !ATTRIBUTE_OPS.contains(op)) {
      return false;
    }
    return isAttributeName(Js.member(value, "attribute"))
        && isNegate(Js.member(value, "negate"))
        && isOptionalKind(Js.member(value, "contextKind"))
        && everyItem(Js.member(value, "values"), Validate::isClauseValue);
  }

  private static boolean isSegmentClause(Object value) {
    if (!Js.isRecord(value) || !"segmentMatch".equals(Js.member(value, "op"))) {
      return false;
    }
    return isNegate(Js.member(value, "negate")) && isStringArray(Js.member(value, "values"));
  }

  private static boolean isWeightedVariation(Object value, int variationCount) {
    if (!Js.isRecord(value)) {
      return false;
    }
    Object weight = Js.member(value, "weight");
    return isVariationIndex(Js.member(value, "variation"), variationCount)
        && isCount(weight)
        && Js.number(weight) <= BUCKET_COUNT;
  }

  private static boolean isRollout(Object value, int variationCount) {
    if (!Js.isRecord(value) || !isOptionalKind(Js.member(value, "contextKind"))) {
      return false;
    }
    Object variations = Js.member(value, "variations");
    if (!Js.isArray(variations) || Js.array(variations).isEmpty()) {
      return false;
    }
    double total = 0;
    for (Object item : Js.array(variations)) {
      if (!isWeightedVariation(item, variationCount)) {
        return false;
      }
      total += Js.number(Js.member(item, "weight"));
    }
    return total == BUCKET_COUNT;
  }

  static boolean isServe(Object value, int variationCount) {
    if (!Js.isRecord(value)) {
      return false;
    }
    Object variation = Js.member(value, "variation");
    Object rollout = Js.member(value, "rollout");
    if (rollout == null) {
      return isVariationIndex(variation, variationCount);
    }
    return variation == null && isRollout(rollout, variationCount);
  }

  private static boolean isRule(Object value, int variationCount) {
    if (!Js.isRecord(value)) {
      return false;
    }
    return everyItem(
            Js.member(value, "clauses"),
            clause -> isAttributeClause(clause) || isSegmentClause(clause))
        && isServe(value, variationCount);
  }

  private static boolean isTarget(Object value, int variationCount) {
    if (!Js.isRecord(value)) {
      return false;
    }
    return isVariationIndex(Js.member(value, "variation"), variationCount)
        && isStringArray(Js.member(value, "contextKeys"))
        && isOptionalKind(Js.member(value, "contextKind"));
  }

  private static boolean isPrerequisite(Object value) {
    return Js.isRecord(value)
        && isKey(Js.member(value, "flagKey"))
        && isCount(Js.member(value, "variation"));
  }

  /**
   * What serving the off variation needs. It is checked apart so {@code on: false} survives a bad
   * rule.
   */
  static boolean isFlagHeader(Object value) {
    if (!Js.isRecord(value) || !(Js.member(value, "on") instanceof Boolean)) {
      return false;
    }
    Object variations = Js.member(value, "variations");
    if (!isKey(Js.member(value, "key"))
        || !Js.isArray(variations)
        || Js.array(variations).isEmpty()) {
      return false;
    }
    Object offVariation = Js.member(value, "offVariation");
    return offVariation == null || isVariationIndex(offVariation, Js.array(variations).size());
  }

  static boolean isFlagBody(Object value, int variationCount) {
    if (!Js.isRecord(value)) {
      return false;
    }
    return Js.member(value, "salt") instanceof String
        && isCount(Js.member(value, "version"))
        && everyItem(Js.member(value, "targets"), item -> isTarget(item, variationCount))
        && everyItem(Js.member(value, "prerequisites"), Validate::isPrerequisite)
        && everyItem(Js.member(value, "rules"), item -> isRule(item, variationCount))
        && isServe(Js.member(value, "fallthrough"), variationCount);
  }

  private static boolean isSegmentRule(Object value) {
    if (!Js.isRecord(value)) {
      return false;
    }
    Object weight = Js.member(value, "weight");
    boolean weightIsValid =
        weight == null || (isCount(weight) && Js.number(weight) <= BUCKET_COUNT);
    return weightIsValid
        && isOptionalKind(Js.member(value, "rolloutContextKind"))
        && everyItem(Js.member(value, "clauses"), Validate::isAttributeClause);
  }

  private static boolean isContextList(Object value) {
    return Js.isRecord(value)
        && isKind(Js.member(value, "contextKind"))
        && isStringArray(Js.member(value, "values"));
  }

  private static boolean isOptionalContextLists(Object value) {
    return value == null || everyItem(value, Validate::isContextList);
  }

  static boolean isSegment(Object value) {
    if (!Js.isRecord(value)) {
      return false;
    }
    return isKey(Js.member(value, "key"))
        && Js.member(value, "salt") instanceof String
        && isStringArray(Js.member(value, "included"))
        && isStringArray(Js.member(value, "excluded"))
        && isOptionalContextLists(Js.member(value, "includedContexts"))
        && isOptionalContextLists(Js.member(value, "excludedContexts"))
        && everyItem(Js.member(value, "rules"), Validate::isSegmentRule);
  }
}
