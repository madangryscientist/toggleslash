package com.github.madangryscientist.toggleslash.sdks.java.eval;

import com.github.madangryscientist.toggleslash.sdks.java.eval.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Op;
import java.util.List;
import java.util.Map;

/** Decides whether one attribute clause holds. */
final class Clauses {
  private Clauses() {}

  private static Object ownValue(Map<String, Object> source, String name) {
    return source.get(name);
  }

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
      current = ownValue(context.attributes(), first);
    }
    for (int i = 1; i < components.size(); i++) {
      if (!Values.isRecord(current)) {
        return null;
      }
      current = ownValue(Values.record(current), components.get(i));
    }
    return current;
  }

  private static int sign(double a, double b) {
    if (a < b) {
      return -1;
    }
    return a > b ? 1 : 0;
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null. */
  private static Integer compare(Object attribute, Object value) {
    if (attribute instanceof Double a && value instanceof Double v) {
      return Double.isFinite(a) ? sign(a, v) : null;
    }
    if (attribute instanceof String a && value instanceof String v) {
      int order = a.compareTo(v);
      return Integer.signum(order);
    }
    return null;
  }

  private static boolean matchesString(Op op, String attribute, String value) {
    switch (op) {
      case MATCHES:
        PatternVm pattern = PatternVm.compile(value);
        return pattern != null && pattern.matches(attribute);
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
    Semver candidate = Semver.parse(a);
    Semver target = Semver.parse(v);
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
    Double candidate = Instants.toInstant(attribute);
    Double target = Instants.toInstant(value);
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
        Integer order = compare(attribute, value);
        return order != null && order == 0;
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
        Integer compared = compare(attribute, value);
        return compared != null && matchesOrder(op, compared);
    }
  }

  /**
   * Does the clause hold? It reads the context of its kind. A missing attribute, or a missing kind,
   * fails it, whatever {@code negate} says.
   */
  static boolean matches(Clause clause, Contexts contexts) {
    KindContext context =
        contexts.get(clause.contextKind() == null ? Contexts.DEFAULT_KIND : clause.contextKind());
    if (context == null) {
      return false;
    }
    Object attribute = readAttribute(context, clause.attribute());
    if (attribute == null) {
      return false;
    }
    List<?> candidates = attribute instanceof List<?> list ? list : List.of(attribute);
    boolean matched = false;
    for (Object candidate : candidates) {
      for (Object value : clause.values()) {
        if (matchesValue(clause.op(), candidate, value)) {
          matched = true;
          break;
        }
      }
      if (matched) {
        break;
      }
    }
    return clause.negate() ? !matched : matched;
  }
}
