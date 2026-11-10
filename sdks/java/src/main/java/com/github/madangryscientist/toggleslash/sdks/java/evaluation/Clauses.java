package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Attribute clauses, to "Clauses" in {@code docs/evaluation.md}. Every input is already valid. */
final class Clauses {
  private static final Set<String> STRING_OPS =
      Set.of("contains", "startsWith", "endsWith", "matches");
  private static final Set<String> TYPED_OPS =
      Set.of("semverEq", "semverLt", "semverLte", "semverGt", "semverGte", "before", "after");

  private Clauses() {}

  /** The value a name or a {@code /} reference reaches, or null when it is missing. */
  private static Object readAttribute(KindContext context, String name) {
    List<String> components = name.startsWith("/") ? Reference.parse(name) : List.of(name);
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
    for (String component : components.subList(1, components.size())) {
      if (!(current instanceof Map<?, ?> record)) {
        return null;
      }
      current = record.get(component);
    }
    return current;
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null. */
  private static Integer compare(Object attribute, Object value) {
    if (attribute instanceof Double a && value instanceof Double b) {
      if (!Double.isFinite(a)) {
        return null;
      }
      return a < b ? -1 : a > b ? 1 : 0;
    }
    if (attribute instanceof String a && value instanceof String b) {
      return Integer.signum(a.compareTo(b));
    }
    return null;
  }

  private static boolean matchesString(String op, String attribute, String value) {
    return switch (op) {
      case "matches" -> {
        Pattern pattern = Pattern.compile(value);
        yield pattern != null && pattern.matches(attribute);
      }
      case "contains" -> attribute.contains(value);
      case "startsWith" -> attribute.startsWith(value);
      default -> attribute.endsWith(value);
    };
  }

  private static boolean matchesOrder(String op, int order) {
    return switch (op) {
      case "lt" -> order < 0;
      case "lte" -> order <= 0;
      case "gt" -> order > 0;
      default -> order >= 0;
    };
  }

  private static boolean matchesSemver(String op, Object attribute, Object value) {
    if (!(attribute instanceof String a) || !(value instanceof String b)) {
      return false;
    }
    Semver.Version candidate = Semver.parse(a);
    Semver.Version target = Semver.parse(b);
    if (candidate == null || target == null) {
      return false;
    }
    int order = Semver.compare(candidate, target);
    return switch (op) {
      case "semverEq" -> order == 0;
      case "semverLt" -> order < 0;
      case "semverLte" -> order <= 0;
      case "semverGt" -> order > 0;
      default -> order >= 0;
    };
  }

  private static boolean matchesDate(String op, Object attribute, Object value) {
    Double candidate = Dates.toInstant(attribute);
    Double target = Dates.toInstant(value);
    if (candidate == null || target == null) {
      return false;
    }
    return op.equals("before") ? candidate < target : candidate > target;
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
      return attribute instanceof String a && value instanceof String v && matchesString(op, a, v);
    }
    if (TYPED_OPS.contains(op)) {
      return op.equals("before") || op.equals("after")
          ? matchesDate(op, attribute, value)
          : matchesSemver(op, attribute, value);
    }
    Integer order = compare(attribute, value);
    return order != null && matchesOrder(op, order);
  }

  /**
   * Does one attribute clause hold? It reads the context of its {@code contextKind}. A missing
   * attribute, or a missing kind, fails it, whatever {@code negate} says.
   */
  static boolean attributeClauseMatches(Object clause, Map<String, KindContext> contexts) {
    Object kind = Validate.field(clause, "contextKind");
    KindContext context = contexts.get(kind == null ? Contexts.DEFAULT_KIND : (String) kind);
    if (context == null) {
      return false;
    }
    Object attribute = readAttribute(context, (String) Validate.field(clause, "attribute"));
    if (attribute == null) {
      return false;
    }
    String op = (String) Validate.field(clause, "op");
    List<?> values = (List<?>) Validate.field(clause, "values");
    List<?> candidates = attribute instanceof List<?> list ? list : List.of(attribute);
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
    return Boolean.TRUE.equals(Validate.field(clause, "negate")) ? !matched : matched;
  }
}
