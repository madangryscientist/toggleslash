package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.ClauseValue;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.ValueKind;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.util.List;
import java.util.Map;

/** Attribute clauses. There is no coercion anywhere. */
final class ClauseMatcher {
  private ClauseMatcher() {}

  /**
   * The value a name or a slash reference reaches, or null when it is missing or null. Only the
   * context's own attributes count, {@code key} always reads the context key, and a step goes into
   * an object only.
   */
  private static Json readAttribute(KindContext context, String name) {
    List<String> components = name.startsWith("/") ? AttributeReference.parse(name) : List.of(name);
    if (components == null || components.isEmpty()) {
      return null;
    }
    Json current;
    if (components.get(0).equals("key")) {
      current = context.keyJson();
    } else if (context.attributes() == null) {
      return null;
    } else {
      current = context.attributes().get(components.get(0));
    }
    for (int i = 1; i < components.size() && current != null; i++) {
      current = current instanceof Json.Obj obj ? obj.get(components.get(i)) : null;
    }
    return current == null || current instanceof Json.Null ? null : current;
  }

  /**
   * Does one attribute clause hold? It reads the context of its kind. A missing attribute, or a
   * missing kind, fails it, whatever {@code negate} says.
   */
  static boolean attributeClauseMatches(Clause clause, Map<String, KindContext> contexts) {
    KindContext context =
        contexts.get(clause.contextKind() == null ? Contexts.DEFAULT_KIND : clause.contextKind());
    if (context == null) {
      return false;
    }
    Json attribute = readAttribute(context, clause.attribute());
    if (attribute == null) {
      return false;
    }
    boolean matched = false;
    if (attribute instanceof Json.Arr arr) {
      for (Json candidate : arr.items()) {
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

  private static boolean anyValueMatches(Clause clause, Json candidate) {
    for (ClauseValue value : clause.values()) {
      if (matches(clause.op(), candidate, value)) {
        return true;
      }
    }
    return false;
  }

  private static boolean matches(Op op, Json attribute, ClauseValue value) {
    switch (op) {
      case IN:
        return equalsValue(attribute, value);
      case CONTAINS:
      case STARTS_WITH:
      case ENDS_WITH:
        if (!(attribute instanceof Json.Str s) || value.kind() != ValueKind.STRING) {
          return false;
        }
        // Ordinal on UTF-16 code units, which is what String does.
        if (op == Op.CONTAINS) {
          return s.value().contains(value.string());
        }
        return op == Op.STARTS_WITH
            ? s.value().startsWith(value.string())
            : s.value().endsWith(value.string());
      case SEMVER_EQ:
      case SEMVER_LT:
      case SEMVER_LTE:
      case SEMVER_GT:
      case SEMVER_GTE:
        return semverMatches(op, attribute, value);
      case MATCHES:
        if (!(attribute instanceof Json.Str text) || value.kind() != ValueKind.STRING) {
          return false;
        }
        PatternVm pattern = PatternVm.compile(value.string());
        return pattern != null && pattern.isMatch(text.value());
      case BEFORE:
      case AFTER:
        Double instant = toInstant(attribute);
        Double limit = toInstant(value);
        if (instant == null || limit == null) {
          return false;
        }
        return op == Op.BEFORE ? instant < limit : instant > limit;
      default:
        Integer order = compare(attribute, value);
        return order != null && matchesOrder(op, order);
    }
  }

  private static boolean equalsValue(Json attribute, ClauseValue value) {
    if (attribute instanceof Json.Str s) {
      return value.kind() == ValueKind.STRING && s.value().equals(value.string());
    }
    if (attribute instanceof Json.Bool b) {
      return value.kind() == ValueKind.BOOLEAN && b.value() == value.bool();
    }
    if (attribute instanceof Json.Num n) {
      return value.kind() == ValueKind.NUMBER
          && Double.isFinite(n.value())
          && n.value() == value.number();
    }
    return false;
  }

  private static boolean semverMatches(Op op, Json attribute, ClauseValue value) {
    if (!(attribute instanceof Json.Str s) || value.kind() != ValueKind.STRING) {
      return false;
    }
    Semver candidate = Semver.parse(s.value());
    Semver target = Semver.parse(value.string());
    if (candidate == null || target == null) {
      return false;
    }
    int order = candidate.compareTo(target);
    return op == Op.SEMVER_EQ ? order == 0 : matchesOrder(op, order);
  }

  /** A finite number is Unix milliseconds, a string is parsed, anything else is not an instant. */
  private static Double toInstant(Json attribute) {
    if (attribute instanceof Json.Num n) {
      return Double.isFinite(n.value()) ? n.value() : null;
    }
    if (attribute instanceof Json.Str s) {
      return Instants.parse(s.value());
    }
    return null;
  }

  private static Double toInstant(ClauseValue value) {
    if (value.kind() == ValueKind.NUMBER) {
      return Double.isFinite(value.number()) ? value.number() : null;
    }
    if (value.kind() == ValueKind.STRING) {
      return Instants.parse(value.string());
    }
    return null;
  }

  /** -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise null. */
  private static Integer compare(Json attribute, ClauseValue value) {
    if (attribute instanceof Json.Num n && value.kind() == ValueKind.NUMBER) {
      if (!Double.isFinite(n.value())) {
        return null;
      }
      return n.value() < value.number() ? -1 : n.value() > value.number() ? 1 : 0;
    }
    if (attribute instanceof Json.Str s && value.kind() == ValueKind.STRING) {
      return Integer.signum(s.value().compareTo(value.string()));
    }
    return null;
  }

  private static boolean matchesOrder(Op op, int order) {
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
