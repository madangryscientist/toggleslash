package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import java.util.List;
import java.util.Map;

/** Attribute clauses: reading an attribute and comparing it with each of a clause's values. */
final class Clauses {
  private Clauses() {}

  private static Object ownValue(Map<?, ?> source, String name) {
    return source.get(name);
  }

  /** The value a name or a reference reaches, or null when it is missing. */
  private static Object readAttribute(KindContext context, List<String> components) {
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
      if (!(current instanceof Map<?, ?> record)) {
        return null;
      }
      current = ownValue(record, components.get(i));
    }
    return current;
  }

  /** A number becomes a Double, so the comparisons below see one numeric type. */
  private static Object scalar(Object candidate) {
    if (candidate instanceof Number n) {
      return n.doubleValue();
    }
    return candidate instanceof String || candidate instanceof Boolean ? candidate : null;
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); 2 otherwise. */
  private static int compare(Object attribute, Object value) {
    if (attribute instanceof Double a && value instanceof Double b) {
      if (!Double.isFinite(a)) {
        return 2;
      }
      return a < b ? -1 : a > b ? 1 : 0;
    }
    if (attribute instanceof String a && value instanceof String b) {
      return Integer.signum(a.compareTo(b));
    }
    return 2;
  }

  private static boolean matchesOrder(Op op, int order) {
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

  private static boolean matchesString(Op op, String attribute, String value, Object prepared) {
    switch (op) {
      case MATCHES:
        return prepared instanceof Pattern pattern && pattern.matches(attribute);
      case CONTAINS:
        return attribute.contains(value);
      case STARTS_WITH:
        return attribute.startsWith(value);
      default:
        return attribute.endsWith(value);
    }
  }

  private static boolean matchesSemver(Op op, Object attribute, Object prepared) {
    if (!(attribute instanceof String text) || !(prepared instanceof Semver target)) {
      return false;
    }
    Semver candidate = Semver.parse(text);
    if (candidate == null) {
      return false;
    }
    int order = candidate.compareTo(target);
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

  private static boolean matchesDate(Op op, Object attribute, Object prepared) {
    Double candidate = Dates.toInstant(attribute);
    if (candidate == null || !(prepared instanceof Double target)) {
      return false;
    }
    return op == Op.BEFORE ? candidate < target : candidate > target;
  }

  private static boolean matchesValue(Op op, Object attribute, Object value, Object prepared) {
    switch (op) {
      case IN:
        return attribute instanceof Boolean
            ? attribute.equals(value)
            : compare(attribute, value) == 0;
      case CONTAINS:
      case STARTS_WITH:
      case ENDS_WITH:
      case MATCHES:
        return attribute instanceof String a
            && value instanceof String v
            && matchesString(op, a, v, prepared);
      case SEMVER_EQ:
      case SEMVER_LT:
      case SEMVER_LTE:
      case SEMVER_GT:
      case SEMVER_GTE:
        return matchesSemver(op, attribute, prepared);
      case BEFORE:
      case AFTER:
        return matchesDate(op, attribute, prepared);
      default:
        int order = compare(attribute, value);
        return order != 2 && matchesOrder(op, order);
    }
  }

  private static boolean anyValueMatches(AttributeClause clause, Object candidate) {
    for (int i = 0; i < clause.values().size(); i++) {
      if (matchesValue(clause.op(), candidate, clause.values().get(i), clause.prepared().get(i))) {
        return true;
      }
    }
    return false;
  }

  /**
   * Does one attribute clause hold? It reads the context of its kind. A missing attribute, or a
   * missing kind, fails it, whatever {@code negate} says.
   */
  static boolean matches(AttributeClause clause, Map<String, KindContext> contexts) {
    String kind = clause.contextKind() == null ? Contexts.DEFAULT_KIND : clause.contextKind();
    KindContext context = contexts.get(kind);
    if (context == null) {
      return false;
    }
    Object attribute = readAttribute(context, clause.reference());
    if (attribute == null) {
      return false;
    }
    boolean matched = false;
    if (attribute instanceof List<?> items) {
      for (Object item : items) {
        Object candidate = scalar(item);
        if (candidate != null && anyValueMatches(clause, candidate)) {
          matched = true;
          break;
        }
      }
    } else {
      Object candidate = scalar(attribute);
      matched = candidate != null && anyValueMatches(clause, candidate);
    }
    return clause.negate() ? !matched : matched;
  }
}
