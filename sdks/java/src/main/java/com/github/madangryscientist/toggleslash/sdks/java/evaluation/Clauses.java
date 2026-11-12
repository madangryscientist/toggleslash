package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;
import java.util.Map;

/** Whether one attribute clause holds for a context, to {@code docs/evaluation.md}. */
final class Clauses {
  private Clauses() {}

  /** The value a name or a {@code /} reference reaches, or null when it is missing. */
  private static Object readAttribute(Contexts.KindContext context, String name) {
    List<String> components = name.startsWith("/") ? References.parse(name) : List.of(name);
    if (components == null || components.isEmpty()) {
      return null;
    }
    String first = components.get(0);
    Object current = null;
    if (first.equals("key")) {
      current = context.key();
    } else if (context.attributes() != null) {
      current = context.attributes().get(first);
    }
    for (int i = 1; i < components.size(); i++) {
      if (!(current instanceof Map<?, ?>)) {
        return null;
      }
      current = ((Map<?, ?>) current).get(components.get(i));
    }
    return current;
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise 2. */
  private static int compare(Object attribute, Object value) {
    if (attribute instanceof Double && value instanceof Double) {
      double a = (Double) attribute;
      double b = (Double) value;
      if (Double.isNaN(a) || Double.isInfinite(a)) {
        return 2;
      }
      return a < b ? -1 : (a > b ? 1 : 0);
    }
    if (attribute instanceof String && value instanceof String) {
      int order = ((String) attribute).compareTo((String) value);
      return order < 0 ? -1 : (order > 0 ? 1 : 0);
    }
    return 2;
  }

  private static boolean matchesString(
      Ruleset.Clause clause, int index, String attribute, String value) {
    switch (clause.op) {
      case MATCHES:
        {
          Pattern pattern = clause.patterns[index];
          return pattern != null && pattern.matches(attribute);
        }
      case CONTAINS:
        return attribute.contains(value);
      case STARTS_WITH:
        return attribute.startsWith(value);
      default:
        return attribute.endsWith(value);
    }
  }

  private static boolean matchesOrder(Ruleset.Op op, int order) {
    switch (op) {
      case LT:
        return order < 0;
      case LTE:
        return order <= 0;
      case GT:
        return order > 0;
      default:
        return order >= 0;
    }
  }

  private static boolean matchesSemver(Ruleset.Op op, Object attribute, Object value) {
    if (!(attribute instanceof String) || !(value instanceof String)) {
      return false;
    }
    Semver candidate = Semver.parse((String) attribute);
    Semver target = Semver.parse((String) value);
    if (candidate == null || target == null) {
      return false;
    }
    int order = Semver.compare(candidate, target);
    switch (op) {
      case SEMVER_EQ:
        return order == 0;
      case SEMVER_LT:
        return order < 0;
      case SEMVER_LTE:
        return order <= 0;
      case SEMVER_GT:
        return order > 0;
      default:
        return order >= 0;
    }
  }

  private static boolean matchesDate(Ruleset.Op op, Object attribute, Object value) {
    Double candidate = Dates.toInstant(attribute);
    Double target = Dates.toInstant(value);
    if (candidate == null || target == null) {
      return false;
    }
    return op == Ruleset.Op.BEFORE ? candidate < target : candidate > target;
  }

  private static boolean matchesValue(Ruleset.Clause clause, int index, Object attribute) {
    Object value = clause.values.get(index);
    switch (clause.op) {
      case IN:
        return attribute instanceof Boolean
            ? attribute.equals(value)
            : compare(attribute, value) == 0;
      case CONTAINS:
      case STARTS_WITH:
      case ENDS_WITH:
      case MATCHES:
        return attribute instanceof String
            && value instanceof String
            && matchesString(clause, index, (String) attribute, (String) value);
      case SEMVER_EQ:
      case SEMVER_LT:
      case SEMVER_LTE:
      case SEMVER_GT:
      case SEMVER_GTE:
        return matchesSemver(clause.op, attribute, value);
      case BEFORE:
      case AFTER:
        return matchesDate(clause.op, attribute, value);
      default:
        {
          int order = compare(attribute, value);
          return order != 2 && matchesOrder(clause.op, order);
        }
    }
  }

  /**
   * Does one attribute clause hold? It reads the context of its {@code contextKind}. A missing
   * attribute, or a missing kind, fails it, whatever {@code negate} says.
   */
  static boolean attributeClauseMatches(Ruleset.Clause clause, Contexts contexts) {
    Contexts.KindContext context =
        contexts.get(clause.contextKind == null ? Contexts.DEFAULT_KIND : clause.contextKind);
    if (context == null) {
      return false;
    }
    Object attribute = readAttribute(context, clause.attribute);
    if (attribute == null) {
      return false;
    }
    List<?> candidates = attribute instanceof List<?> ? (List<?>) attribute : List.of(attribute);
    boolean matched = false;
    outer:
    for (Object candidate : candidates) {
      for (int i = 0; i < clause.values.size(); i++) {
        if (matchesValue(clause, i, candidate)) {
          matched = true;
          break outer;
        }
      }
    }
    return clause.negate ? !matched : matched;
  }
}
