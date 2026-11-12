package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.List;
import java.util.Map;
import java.util.Set;

/** Attribute clauses: reading an attribute and applying an op to it. */
final class Clauses {
  private static final Set<String> STRING_OPS =
      Set.of("contains", "startsWith", "endsWith", "matches");

  private Clauses() {}

  private static Object ownValue(Map<?, ?> source, String name) {
    return source.get(name);
  }

  /** The value a name or a slash reference reaches, or null when it is missing. */
  private static Object readAttribute(Contexts.KindContext context, String name) {
    List<String> components = name.startsWith("/") ? Reference.parse(name) : List.of(name);
    if (components == null || components.isEmpty()) {
      return null;
    }
    String first = components.get(0);
    Object current = null;
    if (first.equals("key")) {
      current = context.key();
    } else if (context.attributes() != null) {
      current = ownValue(context.attributes(), first);
    }
    for (int i = 1; i < components.size(); i++) {
      if (!Js.isRecord(current)) {
        return null;
      }
      current = ownValue((Map<?, ?>) current, components.get(i));
    }
    return current;
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null. */
  private static Integer compare(Object attribute, Object value) {
    if (attribute instanceof Number && value instanceof Number) {
      double a = ((Number) attribute).doubleValue();
      double b = ((Number) value).doubleValue();
      if (!Double.isFinite(a)) {
        return null;
      }
      return a < b ? -1 : a > b ? 1 : 0;
    }
    if (attribute instanceof String && value instanceof String) {
      return Integer.signum(((String) attribute).compareTo((String) value));
    }
    return null;
  }

  private static boolean matchesString(String op, String attribute, String value) {
    switch (op) {
      case "matches":
        Regex pattern = Regex.compile(value);
        return pattern != null && pattern.matches(attribute);
      case "contains":
        return attribute.contains(value);
      case "startsWith":
        return attribute.startsWith(value);
      default:
        return attribute.endsWith(value);
    }
  }

  private static boolean matchesSemver(String op, Object attribute, Object value) {
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
      case "semverEq":
        return order == 0;
      case "semverLt":
        return order < 0;
      case "semverLte":
        return order <= 0;
      case "semverGt":
        return order > 0;
      default:
        return order >= 0;
    }
  }

  private static boolean matchesDate(String op, Object attribute, Object value) {
    Double candidate = Dates.toInstant(attribute);
    Double target = Dates.toInstant(value);
    if (candidate == null || target == null) {
      return false;
    }
    return op.equals("before") ? candidate < target : candidate > target;
  }

  private static boolean matchesOrder(String op, int order) {
    switch (op) {
      case "lt":
        return order < 0;
      case "lte":
        return order <= 0;
      case "gt":
        return order > 0;
      default:
        return order >= 0;
    }
  }

  private static boolean matchesValue(String op, Object attribute, Object value) {
    if (op.equals("in")) {
      if (attribute instanceof Boolean) {
        return attribute.equals(value);
      }
      Integer order = compare(attribute, value);
      return order != null && order == 0;
    }
    if (STRING_OPS.contains(op)) {
      return attribute instanceof String
          && value instanceof String
          && matchesString(op, (String) attribute, (String) value);
    }
    if (op.startsWith("semver")) {
      return matchesSemver(op, attribute, value);
    }
    if (op.equals("before") || op.equals("after")) {
      return matchesDate(op, attribute, value);
    }
    Integer order = compare(attribute, value);
    return order != null && matchesOrder(op, order);
  }

  /**
   * Does one attribute clause hold? It reads the context of its {@code contextKind}. A missing
   * attribute, or a missing kind, fails it, whatever {@code negate} says.
   */
  static boolean attributeClauseMatches(Object clause, Map<String, Contexts.KindContext> contexts) {
    Object declared = Js.member(clause, "contextKind");
    Contexts.KindContext context =
        contexts.get(declared == null ? Contexts.DEFAULT_KIND : (String) declared);
    if (context == null) {
      return false;
    }
    Object attribute = readAttribute(context, (String) Js.member(clause, "attribute"));
    if (attribute == null) {
      return false;
    }
    List<?> candidates = Js.isArray(attribute) ? Js.array(attribute) : List.of(attribute);
    String op = (String) Js.member(clause, "op");
    List<?> values = Js.array(Js.member(clause, "values"));
    boolean matched = false;
    for (Object candidate : candidates) {
      for (Object value : values) {
        if (matchesValue(op, candidate, value)) {
          matched = true;
          break;
        }
      }
      if (matched) {
        break;
      }
    }
    return Boolean.TRUE.equals(Js.member(clause, "negate")) ? !matched : matched;
  }
}
