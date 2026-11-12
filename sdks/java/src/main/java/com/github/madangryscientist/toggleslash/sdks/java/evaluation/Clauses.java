package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import java.util.List;
import java.util.Map;

/** Attribute clauses, to "Clauses" in the evaluation spec. */
final class Clauses {

  private Clauses() {}

  /**
   * Does one attribute clause hold? It reads the context of its {@code contextKind}. A missing
   * attribute, or a missing kind, fails it, whatever {@code negate} says.
   */
  static boolean matches(AttributeClause clause, Map<String, KindContext> contexts) {
    String kind = clause.contextKind() == null ? Contexts.DEFAULT_KIND : clause.contextKind();
    KindContext context = contexts.get(kind);
    if (context == null) {
      return false;
    }
    Object attribute = read(context, clause.components());
    if (attribute == null) {
      return false;
    }
    boolean matched = false;
    if (attribute instanceof List<?> candidates) {
      for (Object candidate : candidates) {
        if (anyValue(clause, candidate)) {
          matched = true;
          break;
        }
      }
    } else {
      matched = anyValue(clause, attribute);
    }
    return clause.negate() != matched;
  }

  private static boolean anyValue(AttributeClause clause, Object candidate) {
    List<Object> values = clause.values();
    for (int i = 0; i < values.size(); i++) {
      if (matchesValue(clause, i, candidate, values.get(i))) {
        return true;
      }
    }
    return false;
  }

  /** The value a name or a reference reaches, or null when it is missing. */
  private static Object read(KindContext context, List<String> components) {
    String first = components.get(0);
    Object current;
    if (first.equals("key")) {
      current = context.key();
    } else if (context.attributes() != null) {
      current = context.attributes().get(first);
    } else {
      current = null;
    }
    for (int i = 1; i < components.size(); i++) {
      if (!(current instanceof Map<?, ?> map)) {
        return null;
      }
      current = map.get(components.get(i));
    }
    return current;
  }

  private static boolean isFinite(Object value) {
    return value instanceof Number number && Double.isFinite(number.doubleValue());
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null. */
  private static Integer compare(Object attribute, Object value) {
    if (attribute instanceof Number number && value instanceof Number other) {
      if (!isFinite(attribute)) {
        return null;
      }
      return Integer.signum(Double.compare(number.doubleValue() + 0.0, other.doubleValue() + 0.0));
    }
    if (attribute instanceof String text && value instanceof String other) {
      return Integer.signum(text.compareTo(other));
    }
    return null;
  }

  private static boolean matchesValue(
      AttributeClause clause, int index, Object attribute, Object value) {
    Op op = clause.op();
    switch (op) {
      case IN:
        if (attribute instanceof Boolean) {
          return attribute.equals(value);
        }
        Integer same = compare(attribute, value);
        return same != null && same == 0;
      case CONTAINS:
        return attribute instanceof String text
            && value instanceof String other
            && text.contains(other);
      case STARTS_WITH:
        return attribute instanceof String text
            && value instanceof String other
            && text.startsWith(other);
      case ENDS_WITH:
        return attribute instanceof String text
            && value instanceof String other
            && text.endsWith(other);
      case MATCHES:
        PatternProgram program = clause.patterns()[index];
        return attribute instanceof String text && program != null && program.matches(text);
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
        Integer order = compare(attribute, value);
        return order != null && matchesOrder(op, order);
    }
  }

  private static boolean matchesOrder(Op op, int order) {
    return switch (op) {
      case LT -> order < 0;
      case LTE -> order <= 0;
      case GT -> order > 0;
      default -> order >= 0;
    };
  }

  private static boolean matchesSemver(Op op, Object attribute, Object value) {
    if (!(attribute instanceof String text) || !(value instanceof String other)) {
      return false;
    }
    Semver candidate = Semver.parse(text);
    Semver target = Semver.parse(other);
    if (candidate == null || target == null) {
      return false;
    }
    int order = candidate.compareTo(target);
    return switch (op) {
      case SEMVER_EQ -> order == 0;
      case SEMVER_LT -> order < 0;
      case SEMVER_LTE -> order <= 0;
      case SEMVER_GT -> order > 0;
      default -> order >= 0;
    };
  }

  private static boolean matchesDate(Op op, Object attribute, Object value) {
    double candidate = Instants.of(attribute);
    double target = Instants.of(value);
    if (Double.isNaN(candidate) || Double.isNaN(target)) {
      return false;
    }
    return op == Op.BEFORE ? candidate < target : candidate > target;
  }
}
