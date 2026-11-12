package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.ClauseValue;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import java.util.List;
import java.util.Map;

/** Whether one attribute clause holds. */
final class Clauses {
  private Clauses() {}

  /** The value a path reaches, or null when it is missing. */
  private static Object readAttribute(KindContext context, List<String> path) {
    String first = path.get(0);
    Object current = null;
    if (first.equals("key")) {
      current = context.key;
    } else if (context.attributes != null) {
      current = context.attributes.get(first);
    }
    for (int i = 1; i < path.size(); i++) {
      if (!(current instanceof Map)) {
        return null;
      }
      current = ((Map<?, ?>) current).get(path.get(i));
    }
    return Contexts.normaliseValue(current);
  }

  private static final int NO_ORDER = 2;

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise NO_ORDER. */
  private static int compare(Object attribute, Object value) {
    if (attribute instanceof Double && value instanceof Double) {
      double a = (Double) attribute;
      double b = (Double) value;
      if (!Double.isFinite(a)) {
        return NO_ORDER;
      }
      return a < b ? -1 : a > b ? 1 : 0;
    }
    if (attribute instanceof String && value instanceof String) {
      return Integer.signum(((String) attribute).compareTo((String) value));
    }
    return NO_ORDER;
  }

  private static boolean matchesString(Op op, String attribute, ClauseValue value) {
    String text = (String) value.raw;
    switch (op) {
      case MATCHES:
        return value.program != null && Regex.matches(value.program, attribute);
      case CONTAINS:
        return attribute.contains(text);
      case STARTS_WITH:
        return attribute.startsWith(text);
      default:
        return attribute.endsWith(text);
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

  private static boolean matchesSemver(Op op, Object attribute, ClauseValue value) {
    if (!(attribute instanceof String) || value.version == null) {
      return false;
    }
    Semver.Version candidate = Semver.parse((String) attribute);
    if (candidate == null) {
      return false;
    }
    int order = Semver.compare(candidate, value.version);
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

  private static boolean matchesDate(Op op, Object attribute, ClauseValue value) {
    double candidate = Dates.toInstant(attribute);
    double target = value.instant;
    if (Double.isNaN(candidate) || Double.isNaN(target)) {
      return false;
    }
    return op == Op.BEFORE ? candidate < target : candidate > target;
  }

  private static boolean matchesValue(Op op, Object attribute, ClauseValue value) {
    switch (op) {
      case IN:
        if (attribute instanceof Boolean) {
          return attribute.equals(value.raw);
        }
        return compare(attribute, value.raw) == 0;
      case CONTAINS:
      case STARTS_WITH:
      case ENDS_WITH:
      case MATCHES:
        return attribute instanceof String
            && value.raw instanceof String
            && matchesString(op, (String) attribute, value);
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
        int order = compare(attribute, value.raw);
        return order != NO_ORDER && matchesOrder(op, order);
    }
  }

  /**
   * Does the clause hold? It reads the context of its kind. A missing attribute, or a missing kind,
   * fails it, whatever negate says.
   */
  static boolean matches(AttributeClause clause, Contexts contexts) {
    String kind = clause.contextKind == null ? Contexts.DEFAULT_KIND : clause.contextKind;
    KindContext context = contexts.byKind.get(kind);
    if (context == null) {
      return false;
    }
    Object attribute = readAttribute(context, clause.path);
    if (attribute == null) {
      return false;
    }
    boolean matched = false;
    if (attribute instanceof List) {
      for (Object element : (List<?>) attribute) {
        if (anyValue(clause, Contexts.normaliseValue(element))) {
          matched = true;
          break;
        }
      }
    } else {
      matched = anyValue(clause, attribute);
    }
    return clause.negate ? !matched : matched;
  }

  private static boolean anyValue(AttributeClause clause, Object candidate) {
    if (candidate == null) {
      return false;
    }
    for (ClauseValue value : clause.values) {
      if (matchesValue(clause.op, candidate, value)) {
        return true;
      }
    }
    return false;
  }
}
