package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.List;
import java.util.Map;

/**
 * Attribute clauses, to the "Clauses" section of docs/evaluation.md. There is no coercion anywhere.
 */
final class ClauseMatcher {
  private ClauseMatcher() {}

  /**
   * The value a name or a {@code /} reference reaches, or null when it is missing or null. Only the
   * context's own attributes count, {@code key} always reads the context key, and a step goes into
   * an object only.
   */
  private static Object readAttribute(ContextKinds.KindContext context, String name) {
    // Load refused an invalid reference, so parse does not return null here.
    List<String> components = name.startsWith("/") ? AttributeReference.parse(name) : List.of(name);
    if (components == null || components.isEmpty()) {
      return null;
    }
    Object current;
    if (components.get(0).equals("key")) {
      current = context.key();
    } else if (context.attributes() == null) {
      return null;
    } else {
      current = context.attributes().get(components.get(0));
    }
    for (int i = 1; i < components.size(); i++) {
      if (!(current instanceof Map)) {
        return null;
      }
      current = ((Map<?, ?>) current).get(components.get(i));
    }
    return current;
  }

  /**
   * Does one attribute clause hold? It reads the context of its {@code contextKind}. A missing
   * attribute, or a missing kind, fails it, whatever {@code negate} says.
   */
  static boolean attributeClauseMatches(
      Ruleset.Clause clause, Map<String, ContextKinds.KindContext> contexts) {
    ContextKinds.KindContext context =
        contexts.get(clause.contextKind() == null ? ContextKinds.DEFAULT : clause.contextKind());
    if (context == null) {
      return false;
    }
    Object attribute = readAttribute(context, clause.attribute());
    if (attribute == null) {
      return false;
    }
    boolean matched = false;
    if (attribute instanceof List) {
      for (Object candidate : (List<?>) attribute) {
        if (anyValueMatches(clause, candidate)) {
          matched = true;
          break;
        }
      }
    } else {
      matched = anyValueMatches(clause, attribute);
    }
    return clause.negate() != matched;
  }

  private static boolean anyValueMatches(Ruleset.Clause clause, Object candidate) {
    for (Ruleset.ClauseValue value : clause.values()) {
      if (matches(clause.op(), candidate, value)) {
        return true;
      }
    }
    return false;
  }

  private static boolean matches(Ruleset.Op op, Object attribute, Ruleset.ClauseValue value) {
    switch (op) {
      case IN:
        if (attribute instanceof String) {
          return value.kind() == Ruleset.ValueKind.STRING && attribute.equals(value.string());
        }
        if (attribute instanceof Boolean) {
          return value.kind() == Ruleset.ValueKind.BOOLEAN
              && ((Boolean) attribute).booleanValue() == value.bool();
        }
        if (attribute instanceof Number) {
          double number = ((Number) attribute).doubleValue();
          return value.kind() == Ruleset.ValueKind.NUMBER
              && !Double.isNaN(number)
              && !Double.isInfinite(number)
              && number == value.number();
        }
        return false;
      case CONTAINS:
      case STARTS_WITH:
      case ENDS_WITH:
        if (!(attribute instanceof String) || value.kind() != Ruleset.ValueKind.STRING) {
          return false;
        }
        // Code units, as String's own methods do: no locale, no normalisation.
        String text = (String) attribute;
        if (op == Ruleset.Op.CONTAINS) {
          return text.contains(value.string());
        }
        return op == Ruleset.Op.STARTS_WITH
            ? text.startsWith(value.string())
            : text.endsWith(value.string());
      case SEMVER_EQ:
      case SEMVER_LT:
      case SEMVER_LTE:
      case SEMVER_GT:
      case SEMVER_GTE:
        {
          if (!(attribute instanceof String) || value.kind() != Ruleset.ValueKind.STRING) {
            return false;
          }
          SemanticVersion candidate = SemanticVersion.parse((String) attribute);
          SemanticVersion target = SemanticVersion.parse(value.string());
          if (candidate == null || target == null) {
            return false;
          }
          int order = candidate.compareTo(target);
          return op == Ruleset.Op.SEMVER_EQ ? order == 0 : matchesOrder(op, order);
        }
      case MATCHES:
        {
          // Compiled per call; an invalid pattern does not match.
          if (!(attribute instanceof String) || value.kind() != Ruleset.ValueKind.STRING) {
            return false;
          }
          Pattern pattern = Pattern.compile(value.string());
          return pattern != null && pattern.isMatch((String) attribute);
        }
      case BEFORE:
      case AFTER:
        {
          Double instant = toInstant(attribute);
          Double limit = toInstant(value);
          if (instant == null || limit == null) {
            return false;
          }
          return op == Ruleset.Op.BEFORE ? instant < limit : instant > limit;
        }
      default:
        {
          Integer order = compare(attribute, value);
          return order != null && matchesOrder(op, order);
        }
    }
  }

  /** A finite number is Unix milliseconds, a string is parsed, anything else is not an instant. */
  private static Double toInstant(Object attribute) {
    if (attribute instanceof Number) {
      double number = ((Number) attribute).doubleValue();
      return Double.isNaN(number) || Double.isInfinite(number) ? null : number;
    }
    if (attribute instanceof String) {
      return RfcInstant.parse((String) attribute);
    }
    return null;
  }

  private static Double toInstant(Ruleset.ClauseValue value) {
    if (value.kind() == Ruleset.ValueKind.NUMBER) {
      return Double.isNaN(value.number()) || Double.isInfinite(value.number())
          ? null
          : value.number();
    }
    if (value.kind() == Ruleset.ValueKind.STRING) {
      return RfcInstant.parse(value.string());
    }
    return null;
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null. */
  private static Integer compare(Object attribute, Ruleset.ClauseValue value) {
    if (attribute instanceof Number && value.kind() == Ruleset.ValueKind.NUMBER) {
      double number = ((Number) attribute).doubleValue();
      if (Double.isNaN(number) || Double.isInfinite(number)) {
        return null;
      }
      return number < value.number() ? -1 : number > value.number() ? 1 : 0;
    }
    if (attribute instanceof String && value.kind() == Ruleset.ValueKind.STRING) {
      return Integer.signum(((String) attribute).compareTo(value.string()));
    }
    return null;
  }

  private static boolean matchesOrder(Ruleset.Op op, int order) {
    switch (op) {
      case LT:
      case SEMVER_LT:
        return order < 0;
      case LTE:
      case SEMVER_LTE:
        return order <= 0;
      case GT:
      case SEMVER_GT:
        return order > 0;
      default:
        return order >= 0;
    }
  }
}
