package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Op;
import java.util.List;
import java.util.Map;

/** Attribute clauses to {@code docs/evaluation.md}. */
final class Clauses {
  private Clauses() {}

  /** A number of any Java type is a binary64 number; other values are left alone. */
  private static Object normalise(Object value) {
    if (value instanceof Double) {
      return value;
    }
    if (value instanceof Number n) {
      return n.doubleValue();
    }
    return value;
  }

  /** The value a name or a reference reaches, or null when it is missing. */
  private static Object readAttribute(KindContext context, List<String> path) {
    String first = path.get(0);
    Object current = null;
    if (first.equals("key")) {
      current = context.key();
    } else if (context.attributes() != null) {
      current = context.attributes().get(first);
    }
    for (int i = 1; i < path.size(); i++) {
      if (!(current instanceof Map<?, ?> map)) {
        return null;
      }
      current = map.get(path.get(i));
    }
    return current;
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null. */
  private static Integer compare(Object attribute, Object value) {
    if (attribute instanceof Double a && value instanceof Double v) {
      if (a.isNaN() || a.isInfinite()) {
        return null;
      }
      return a < v ? -1 : a > v ? 1 : 0;
    }
    if (attribute instanceof String a && value instanceof String v) {
      return Integer.signum(a.compareTo(v));
    }
    return null;
  }

  private static boolean matchesString(Op op, String attribute, String value) {
    switch (op) {
      case MATCHES:
        {
          Patterns.Compiled pattern = Patterns.compile(value);
          return pattern != null && Patterns.matches(pattern, attribute);
        }
      case CONTAINS:
        return attribute.contains(value);
      case STARTS_WITH:
        return attribute.startsWith(value);
      default:
        return attribute.endsWith(value);
    }
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

  private static boolean matchesSemver(Op op, Object attribute, Object value) {
    if (!(attribute instanceof String a) || !(value instanceof String v)) {
      return false;
    }
    Semver.Version candidate = Semver.parse(a);
    Semver.Version target = Semver.parse(v);
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

  private static boolean matchesDate(Op op, Object attribute, Object value) {
    Double candidate = Dates.toInstant(attribute);
    Double target = Dates.toInstant(value);
    if (candidate == null || target == null) {
      return false;
    }
    return op == Op.BEFORE ? candidate < target : candidate > target;
  }

  private static boolean matchesValue(Op op, Object attribute, Object value) {
    switch (op) {
      case IN:
        if (attribute instanceof Boolean) {
          return attribute.equals(value);
        }
        {
          Integer order = compare(attribute, value);
          return order != null && order == 0;
        }
      case CONTAINS:
      case STARTS_WITH:
      case ENDS_WITH:
      case MATCHES:
        return attribute instanceof String a
            && value instanceof String v
            && matchesString(op, a, v);
      case SEMVER_EQ:
      case SEMVER_LT:
      case SEMVER_LTE:
      case SEMVER_GT:
      case SEMVER_GTE:
        return matchesSemver(op, attribute, value);
      case BEFORE:
      case AFTER:
        return matchesDate(op, attribute, value);
      default:
        {
          Integer order = compare(attribute, value);
          return order != null && matchesOrder(op, order);
        }
    }
  }

  /**
   * Does one attribute clause hold? It reads the context of its kind. A missing attribute, or a
   * missing kind, fails it, whatever {@code negate} says.
   *
   * @param clause the clause
   * @param contexts the context
   * @return whether it holds
   */
  static boolean attributeClauseMatches(AttributeClause clause, Map<String, KindContext> contexts) {
    KindContext context =
        contexts.get(clause.contextKind() == null ? Contexts.DEFAULT_KIND : clause.contextKind());
    if (context == null) {
      return false;
    }
    Object attribute = readAttribute(context, clause.path());
    if (attribute == null) {
      return false;
    }
    boolean matched = false;
    if (attribute instanceof List<?> list) {
      for (Object candidate : list) {
        if (anyValue(clause, normalise(candidate))) {
          matched = true;
          break;
        }
      }
    } else {
      matched = anyValue(clause, normalise(attribute));
    }
    return clause.negate() ? !matched : matched;
  }

  private static boolean anyValue(AttributeClause clause, Object candidate) {
    for (Object value : clause.values()) {
      if (matchesValue(clause.op(), candidate, value)) {
        return true;
      }
    }
    return false;
  }
}
