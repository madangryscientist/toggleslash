package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.ClauseValue;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.FlagHeader;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Target;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.TargetHit;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.ValueKind;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.WeightedVariation;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.function.Function;

/** The shape of a ruleset, checked at runtime because rulesets arrive as JSON. */
final class Loader {
  private Loader() {}

  private static boolean isAbsent(Json value) {
    return value == null || value instanceof Json.Null;
  }

  private static Json get(Json.Obj obj, String name) {
    return obj.get(name);
  }

  /** A non-negative whole finite number, or -1. */
  private static double count(Json value) {
    if (value instanceof Json.Num n) {
      double d = n.value();
      if (Double.isFinite(d) && d == Math.floor(d) && d >= 0) {
        return d;
      }
    }
    return -1;
  }

  private static int index(Json value, int variationCount) {
    double d = count(value);
    return d < 0 || d >= variationCount ? -1 : (int) d;
  }

  private static String key(Json value) {
    return value instanceof Json.Str s && !s.value().isEmpty() ? s.value() : null;
  }

  private static String string(Json value) {
    return value instanceof Json.Str s ? s.value() : null;
  }

  private static List<String> strings(Json value) {
    if (!(value instanceof Json.Arr array)) {
      return null;
    }
    List<String> out = new ArrayList<>();
    for (Json item : array.items()) {
      String text = string(item);
      if (text == null) {
        return null;
      }
      out.add(text);
    }
    return out;
  }

  /** The negate flag: absent is false, a boolean is itself, anything else is null. */
  private static Boolean negate(Json value) {
    if (isAbsent(value)) {
      return Boolean.FALSE;
    }
    return value instanceof Json.Bool b ? Boolean.valueOf(b.value()) : null;
  }

  private static ClauseValue clauseValue(Json value) {
    if (value instanceof Json.Str s) {
      return new ClauseValue(ValueKind.STRING, s.value(), 0, false);
    }
    if (value instanceof Json.Bool b) {
      return new ClauseValue(ValueKind.BOOLEAN, "", 0, b.value());
    }
    if (value instanceof Json.Num n && Double.isFinite(n.value())) {
      return new ClauseValue(ValueKind.NUMBER, "", n.value(), false);
    }
    return null;
  }

  private static Op attributeOp(String op) {
    if (op == null) {
      return null;
    }
    switch (op) {
      case "in":
        return Op.IN;
      case "contains":
        return Op.CONTAINS;
      case "startsWith":
        return Op.STARTS_WITH;
      case "endsWith":
        return Op.ENDS_WITH;
      case "lt":
        return Op.LT;
      case "lte":
        return Op.LTE;
      case "gt":
        return Op.GT;
      case "gte":
        return Op.GTE;
      case "semverEq":
        return Op.SEMVER_EQ;
      case "semverLt":
        return Op.SEMVER_LT;
      case "semverLte":
        return Op.SEMVER_LTE;
      case "semverGt":
        return Op.SEMVER_GT;
      case "semverGte":
        return Op.SEMVER_GTE;
      case "before":
        return Op.BEFORE;
      case "after":
        return Op.AFTER;
      case "matches":
        return Op.MATCHES;
      default:
        return null;
    }
  }

  private static String opName(Json.Obj value) {
    return get(value, "op") instanceof Json.Str s ? s.value() : null;
  }

  /**
   * An optional kind: absent is valid and null. Returns false when it is present and not a kind.
   */
  private static boolean optionalKind(Json value, String[] out) {
    out[0] = null;
    if (isAbsent(value)) {
      return true;
    }
    String text = string(value);
    if (text == null || !Contexts.isKind(text)) {
      return false;
    }
    out[0] = text;
    return true;
  }

  private static Clause readAttributeClause(Json value) {
    if (!(value instanceof Json.Obj obj)) {
      return null;
    }
    Op op = attributeOp(opName(obj));
    if (op == null) {
      return null;
    }
    String[] contextKind = new String[1];
    if (!optionalKind(get(obj, "contextKind"), contextKind)) {
      return null;
    }
    String attribute = key(get(obj, "attribute"));
    if (attribute == null) {
      return null;
    }
    if (attribute.startsWith("/") && AttributeReference.parse(attribute) == null) {
      return null;
    }
    Boolean negate = negate(get(obj, "negate"));
    if (negate == null || !(get(obj, "values") instanceof Json.Arr values)) {
      return null;
    }
    List<ClauseValue> parsed = new ArrayList<>();
    for (Json item : values.items()) {
      ClauseValue clauseValue = clauseValue(item);
      if (clauseValue == null) {
        return null;
      }
      parsed.add(clauseValue);
    }
    return new Clause(op, attribute, parsed, List.of(), negate, contextKind[0]);
  }

  private static Clause readSegmentClause(Json value) {
    if (!(value instanceof Json.Obj obj) || !"segmentMatch".equals(opName(obj))) {
      return null;
    }
    Boolean negate = negate(get(obj, "negate"));
    List<String> keys = strings(get(obj, "values"));
    if (negate == null || keys == null) {
      return null;
    }
    return new Clause(Op.SEGMENT_MATCH, "", List.of(), keys, negate, null);
  }

  private static <T> List<T> readAll(Json value, Function<Json, T> read) {
    if (!(value instanceof Json.Arr array)) {
      return null;
    }
    List<T> items = new ArrayList<>();
    for (Json item : array.items()) {
      T parsed = read.apply(item);
      if (parsed == null) {
        return null;
      }
      items.add(parsed);
    }
    return items;
  }

  /** The weighted variations, or null when the rollout is invalid. */
  private static Serve readRollout(Json value, int variationCount) {
    if (!(value instanceof Json.Obj obj)) {
      return null;
    }
    String[] kind = new String[1];
    if (!optionalKind(get(obj, "contextKind"), kind)) {
      return null;
    }
    if (!(get(obj, "variations") instanceof Json.Arr variations) || variations.items().isEmpty()) {
      return null;
    }
    List<WeightedVariation> rollout = new ArrayList<>();
    double total = 0;
    for (Json item : variations.items()) {
      if (!(item instanceof Json.Obj entry)) {
        return null;
      }
      int variation = index(get(entry, "variation"), variationCount);
      double weight = count(get(entry, "weight"));
      if (variation < 0 || weight < 0 || weight > Bucketing.BUCKET_COUNT) {
        return null;
      }
      rollout.add(new WeightedVariation(variation, weight));
      total += weight;
    }
    return total == Bucketing.BUCKET_COUNT ? new Serve(null, rollout, kind[0]) : null;
  }

  /** Exactly one of variation or rollout. */
  private static Serve readServe(Json value, int variationCount) {
    if (!(value instanceof Json.Obj obj)) {
      return null;
    }
    Json variation = get(obj, "variation");
    Json rollout = get(obj, "rollout");
    if (isAbsent(rollout)) {
      int i = index(variation, variationCount);
      return i < 0 ? null : new Serve(i, null, null);
    }
    return isAbsent(variation) ? readRollout(rollout, variationCount) : null;
  }

  private static Rule readRule(Json value, int variationCount) {
    if (!(value instanceof Json.Obj obj)) {
      return null;
    }
    List<Clause> clauses =
        readAll(
            get(obj, "clauses"),
            clause -> {
              Clause attribute = readAttributeClause(clause);
              return attribute != null ? attribute : readSegmentClause(clause);
            });
    Serve serve = readServe(obj, variationCount);
    if (clauses == null || serve == null) {
      return null;
    }
    return new Rule(clauses, serve);
  }

  /** What serving the off variation needs: key, on, variations and offVariation. */
  static FlagHeader readHeader(Json.Obj value) {
    if (!(get(value, "on") instanceof Json.Bool on)) {
      return null;
    }
    String key = key(get(value, "key"));
    if (key == null
        || !(get(value, "variations") instanceof Json.Arr variations)
        || variations.items().isEmpty()) {
      return null;
    }
    Json offVariation = get(value, "offVariation");
    Integer off = null;
    if (!isAbsent(offVariation)) {
      int i = index(offVariation, variations.items().size());
      if (i < 0) {
        return null;
      }
      off = i;
    }
    return new FlagHeader(key, on.value(), variations.items(), off);
  }

  /** The rest of the flag, or null when anything in it is invalid. */
  static Flag readBody(Json.Obj value, FlagHeader header) {
    int count = header.variations().size();
    String salt = string(get(value, "salt"));
    double version = count(get(value, "version"));
    if (salt == null || version < 0) {
      return null;
    }
    List<Target> rawTargets = readAll(get(value, "targets"), item -> readTarget(item, count));
    if (rawTargets == null) {
      return null;
    }
    Map<String, Map<String, TargetHit>> targets = new HashMap<>();
    for (int order = 0; order < rawTargets.size(); order++) {
      Target target = rawTargets.get(order);
      Map<String, TargetHit> keys = targets.computeIfAbsent(target.kind(), k -> new HashMap<>());
      for (String contextKey : target.contextKeys()) {
        keys.putIfAbsent(contextKey, new TargetHit(order, target.variation()));
      }
    }
    List<Prerequisite> prerequisites =
        readAll(get(value, "prerequisites"), Loader::readPrerequisite);
    List<Rule> rules = readAll(get(value, "rules"), item -> readRule(item, count));
    if (prerequisites == null || rules == null) {
      return null;
    }
    Json rawFallthrough = get(value, "fallthrough");
    Serve fallthrough = rawFallthrough == null ? null : readServe(rawFallthrough, count);
    if (fallthrough == null) {
      return null;
    }
    return new Flag(header, salt, version, targets, prerequisites, rules, fallthrough);
  }

  private static Target readTarget(Json value, int variationCount) {
    if (!(value instanceof Json.Obj obj)) {
      return null;
    }
    int variation = index(get(obj, "variation"), variationCount);
    String[] kind = new String[1];
    if (variation < 0 || !optionalKind(get(obj, "contextKind"), kind)) {
      return null;
    }
    List<String> keys = strings(get(obj, "contextKeys"));
    if (keys == null) {
      return null;
    }
    return new Target(variation, keys, kind[0] == null ? Contexts.DEFAULT_KIND : kind[0]);
  }

  private static Prerequisite readPrerequisite(Json value) {
    if (!(value instanceof Json.Obj obj)) {
      return null;
    }
    String flagKey = key(get(obj, "flagKey"));
    double variation = count(get(obj, "variation"));
    if (flagKey == null || variation < 0) {
      return null;
    }
    return new Prerequisite(flagKey, variation);
  }

  private static SegmentRule readSegmentRule(Json value) {
    if (!(value instanceof Json.Obj obj)) {
      return null;
    }
    Json weight = get(obj, "weight");
    Double parsedWeight = null;
    if (!isAbsent(weight)) {
      double w = count(weight);
      if (w < 0 || w > Bucketing.BUCKET_COUNT) {
        return null;
      }
      parsedWeight = w;
    }
    String[] rolloutKind = new String[1];
    if (!optionalKind(get(obj, "rolloutContextKind"), rolloutKind)) {
      return null;
    }
    List<Clause> clauses = readAll(get(obj, "clauses"), Loader::readAttributeClause);
    return clauses == null ? null : new SegmentRule(clauses, parsedWeight, rolloutKind[0]);
  }

  /**
   * Keys by kind: the user keys under user, merged with an optional array of kind and values. Null
   * when the array is present and malformed.
   */
  private static Map<String, Set<String>> readKeysByKind(List<String> userKeys, Json lists) {
    Map<String, Set<String>> index = new HashMap<>();
    index.put(Contexts.DEFAULT_KIND, new HashSet<>(userKeys));
    if (isAbsent(lists)) {
      return index;
    }
    if (!(lists instanceof Json.Arr array)) {
      return null;
    }
    for (Json item : array.items()) {
      if (!(item instanceof Json.Obj obj)) {
        return null;
      }
      String kind = string(get(obj, "contextKind"));
      if (kind == null || !Contexts.isKind(kind)) {
        return null;
      }
      List<String> values = strings(get(obj, "values"));
      if (values == null) {
        return null;
      }
      index.computeIfAbsent(kind, k -> new HashSet<>()).addAll(values);
    }
    return index;
  }

  /** A segment, or null when it is malformed. */
  static Segment readSegment(Json.Obj value) {
    String key = key(get(value, "key"));
    String salt = string(get(value, "salt"));
    if (key == null || salt == null) {
      return null;
    }
    List<String> included = strings(get(value, "included"));
    List<String> excluded = strings(get(value, "excluded"));
    if (included == null || excluded == null) {
      return null;
    }
    Map<String, Set<String>> includedByKind =
        readKeysByKind(included, get(value, "includedContexts"));
    Map<String, Set<String>> excludedByKind =
        readKeysByKind(excluded, get(value, "excludedContexts"));
    if (includedByKind == null || excludedByKind == null) {
      return null;
    }
    List<SegmentRule> rules = readAll(get(value, "rules"), Loader::readSegmentRule);
    return rules == null ? null : new Segment(key, salt, includedByKind, excludedByKind, rules);
  }
}
