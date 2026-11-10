package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.OptionalInt;
import java.util.Set;
import java.util.function.Function;

/**
 * A validated, indexed ruleset ({@code {flags, segments}} as JSON hands it over), and the
 * evaluation of one flag against it, to {@code docs/evaluation.md}. It is immutable and safe to
 * share between threads. Neither creating one nor evaluating on it throws: a bad flag becomes an
 * {@code ERROR} result when it is evaluated.
 */
public final class Ruleset {
  /** Prerequisite links from the requested flag; {@code docs/evaluation.md} pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private enum Status {
    MALFORMED,
    BROKEN,
    READY
  }

  /** A target that lists a key: its place in {@code targets} and the variation it serves. */
  private record TargetHit(int order, int variation) {}

  /** A flag after validation. {@code header} is the flag itself when it has a valid header. */
  private record FlagEntry(
      Status status, Map<?, ?> flag, ErrorCode code, Map<String, Map<String, TargetHit>> targets) {}

  /** A segment after validation, with the keys it lists by kind. */
  private record ReadySegment(
      Map<?, ?> segment, Map<String, Set<String>> included, Map<String, Set<String>> excluded) {}

  /** Per-evaluation state. */
  private static final class Run {
    final Map<String, KindContext> contexts;
    final Set<String> active = new HashSet<>();
    final Map<String, Result> completed = new HashMap<>();

    Run(Map<String, KindContext> contexts) {
      this.contexts = contexts;
    }
  }

  private static final Object MALFORMED_SEGMENT = new Object();

  private final Map<String, FlagEntry> flags;
  private final Map<String, Object> segments;

  private Ruleset(Map<String, FlagEntry> flags, Map<String, Object> segments) {
    this.flags = flags;
    this.segments = segments;
  }

  /**
   * Validates and indexes a ruleset once, so each evaluation skips that work. Anything that is not
   * a record with a {@code flags} and a {@code segments} array is an empty ruleset on that side.
   */
  public static Ruleset of(Object input) {
    Object rawFlags = Validate.isRecord(input) ? Validate.field(input, "flags") : null;
    Object rawSegments = Validate.isRecord(input) ? Validate.field(input, "segments") : null;
    Map<String, Object> segments = indexByKey(rawSegments, Ruleset::compileSegment);
    Map<String, FlagEntry> flags = indexByKey(rawFlags, item -> compileFlag(item, segments));
    return new Ruleset(flags, segments);
  }

  /**
   * Does a body parsed from JSON have the shape of a ruleset: {@code flags} and {@code segments}
   * arrays?
   */
  public static boolean isRuleset(Object body) {
    return Validate.isRecord(body)
        && Validate.field(body, "flags") instanceof List
        && Validate.field(body, "segments") instanceof List;
  }

  private static <T> Map<String, T> indexByKey(Object items, Function<Object, T> compile) {
    Map<String, T> index = new HashMap<>();
    if (items instanceof List<?> list) {
      for (Object item : list) {
        Object key = Validate.isRecord(item) ? Validate.field(item, "key") : null;
        if (key instanceof String text && !index.containsKey(text)) {
          index.put(text, compile.apply(item));
        }
      }
    }
    return index;
  }

  private static Map<String, Set<String>> keysByKind(Object userKeys, Object lists) {
    Map<String, Set<String>> index = new HashMap<>();
    Set<String> users = new HashSet<>();
    for (Object key : (List<?>) userKeys) {
      users.add((String) key);
    }
    index.put(Contexts.DEFAULT_KIND, users);
    if (lists instanceof List<?> list) {
      for (Object entry : list) {
        String kind = (String) Validate.field(entry, "contextKind");
        Set<String> keys = index.computeIfAbsent(kind, k -> new HashSet<>());
        for (Object value : (List<?>) Validate.field(entry, "values")) {
          keys.add((String) value);
        }
      }
    }
    return index;
  }

  private static Object compileSegment(Object value) {
    if (!Validate.isSegment(value)) {
      return MALFORMED_SEGMENT;
    }
    return new ReadySegment(
        (Map<?, ?>) value,
        keysByKind(Validate.field(value, "included"), Validate.field(value, "includedContexts")),
        keysByKind(Validate.field(value, "excluded"), Validate.field(value, "excludedContexts")));
  }

  /**
   * The first reference to a segment that is missing or malformed, in rule, clause, value order.
   */
  private static ErrorCode segmentProblem(Map<?, ?> flag, Map<String, Object> segments) {
    for (Object rule : (List<?>) flag.get("rules")) {
      for (Object clause : (List<?>) Validate.field(rule, "clauses")) {
        if (!"segmentMatch".equals(Validate.field(clause, "op"))) {
          continue;
        }
        for (Object key : (List<?>) Validate.field(clause, "values")) {
          Object segment = segments.get(key);
          if (segment == null) {
            return ErrorCode.SEGMENT_NOT_FOUND;
          }
          if (segment == MALFORMED_SEGMENT) {
            return ErrorCode.MALFORMED_SEGMENT;
          }
        }
      }
    }
    return null;
  }

  private static Map<String, Map<String, TargetHit>> targetIndex(Map<?, ?> flag) {
    Map<String, Map<String, TargetHit>> index = new HashMap<>();
    List<?> targets = (List<?>) flag.get("targets");
    for (int order = 0; order < targets.size(); order++) {
      Object target = targets.get(order);
      Object kind = Validate.field(target, "contextKind");
      Map<String, TargetHit> keys =
          index.computeIfAbsent(
              kind == null ? Contexts.DEFAULT_KIND : (String) kind, k -> new HashMap<>());
      int variation = ((Double) Validate.field(target, "variation")).intValue();
      for (Object contextKey : (List<?>) Validate.field(target, "contextKeys")) {
        keys.putIfAbsent((String) contextKey, new TargetHit(order, variation));
      }
    }
    return index;
  }

  private static FlagEntry compileFlag(Object value, Map<String, Object> segments) {
    if (!Validate.isFlagHeader(value)) {
      return new FlagEntry(Status.MALFORMED, null, null, null);
    }
    Map<?, ?> flag = (Map<?, ?>) value;
    int variationCount = ((List<?>) flag.get("variations")).size();
    if (!Validate.isFlagBody(value, variationCount)) {
      return new FlagEntry(Status.BROKEN, flag, ErrorCode.MALFORMED_FLAG, null);
    }
    ErrorCode code = segmentProblem(flag, segments);
    if (code != null) {
      return new FlagEntry(Status.BROKEN, flag, code, null);
    }
    return new FlagEntry(Status.READY, flag, null, targetIndex(flag));
  }

  /**
   * The {@code version} of the flag {@code key}, or empty when there is no such flag or it is
   * malformed.
   */
  public Optional<Double> flagVersion(String key) {
    FlagEntry entry = flags.get(key);
    if (entry == null || entry.status() == Status.MALFORMED) {
      return Optional.empty();
    }
    return entry.flag().get("version") instanceof Double version
        ? Optional.of(version)
        : Optional.empty();
  }

  /** How many flags this ruleset holds. */
  public int flagCount() {
    return flags.size();
  }

  /**
   * Evaluates one flag for one context (a JSON tree: a {@code Map} with a {@code key} and optional
   * {@code kind} and {@code attributes}, or a multi context). It never throws for a bad flag, bad
   * context or missing flag: those are {@code ERROR} results.
   */
  public Result evaluate(String flagKey, Object context) {
    Map<String, KindContext> contexts = Contexts.normalise(context);
    if (contexts == null) {
      return Result.failure(ErrorCode.INVALID_CONTEXT);
    }
    return evaluateFlag(new Run(contexts), flagKey, 0);
  }

  private static Object variationValue(Map<?, ?> flag, int index) {
    return ((List<?>) flag.get("variations")).get(index);
  }

  private static Result served(Map<?, ?> flag, int index, Reason reason) {
    return new Result(variationValue(flag, index), OptionalInt.of(index), reason);
  }

  private static Result offResult(Map<?, ?> flag, Reason reason) {
    Object index = flag.get("offVariation");
    if (index == null) {
      return new Result(null, OptionalInt.empty(), reason);
    }
    return served(flag, ((Double) index).intValue(), reason);
  }

  private static int rolloutVariation(Object rollout, int position) {
    int chosen = 0;
    long cumulative = 0;
    for (Object item : (List<?>) Validate.field(rollout, "variations")) {
      chosen = ((Double) Validate.field(item, "variation")).intValue();
      cumulative += ((Double) Validate.field(item, "weight")).longValue();
      if (position < cumulative) {
        break;
      }
    }
    return chosen;
  }

  private static Result serveResult(
      Map<?, ?> flag, Object serve, Map<String, KindContext> contexts, Reason reason) {
    Object rollout = Validate.field(serve, "rollout");
    int index;
    if (rollout != null) {
      String kind = (String) Validate.field(rollout, "contextKind");
      int position =
          Contexts.kindBucket((String) flag.get("key"), (String) flag.get("salt"), kind, contexts);
      index = rolloutVariation(rollout, position);
    } else {
      index = ((Double) Validate.field(serve, "variation")).intValue();
    }
    return served(flag, index, reason);
  }

  private static boolean listsContext(
      Map<String, Set<String>> keys, Map<String, KindContext> contexts) {
    for (Map.Entry<String, KindContext> entry : contexts.entrySet()) {
      Set<String> set = keys.get(entry.getKey());
      if (set != null && set.contains(entry.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  private static boolean segmentRuleMatches(
      Object rule, ReadySegment ready, Map<String, KindContext> contexts) {
    for (Object clause : (List<?>) Validate.field(rule, "clauses")) {
      if (!Clauses.attributeClauseMatches(clause, contexts)) {
        return false;
      }
    }
    Object weight = Validate.field(rule, "weight");
    if (weight == null) {
      return true;
    }
    int position =
        Contexts.kindBucket(
            (String) ready.segment().get("key"),
            (String) ready.segment().get("salt"),
            (String) Validate.field(rule, "rolloutContextKind"),
            contexts);
    return position < (Double) weight;
  }

  private static boolean segmentMatches(ReadySegment ready, Map<String, KindContext> contexts) {
    if (listsContext(ready.excluded(), contexts)) {
      return false;
    }
    if (listsContext(ready.included(), contexts)) {
      return true;
    }
    for (Object rule : (List<?>) ready.segment().get("rules")) {
      if (segmentRuleMatches(rule, ready, contexts)) {
        return true;
      }
    }
    return false;
  }

  private boolean clauseMatches(Object clause, Run run) {
    if (!"segmentMatch".equals(Validate.field(clause, "op"))) {
      return Clauses.attributeClauseMatches(clause, run.contexts);
    }
    boolean matched = false;
    for (Object key : (List<?>) Validate.field(clause, "values")) {
      if (segments.get(key) instanceof ReadySegment ready && segmentMatches(ready, run.contexts)) {
        matched = true;
        break;
      }
    }
    return Boolean.TRUE.equals(Validate.field(clause, "negate")) ? !matched : matched;
  }

  private static boolean prerequisiteMet(Object prerequisite, Result result) {
    ReasonKind kind = result.reason().kind();
    boolean reachedServe =
        kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH;
    return reachedServe
        && result.variationIndex().isPresent()
        && result.variationIndex().getAsInt() == (Double) Validate.field(prerequisite, "variation");
  }

  private Result checkPrerequisites(Map<?, ?> flag, Run run, int depth) {
    for (Object prerequisite : (List<?>) flag.get("prerequisites")) {
      String key = (String) Validate.field(prerequisite, "flagKey");
      Result result = evaluateFlag(run, key, depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(flag, Reason.prerequisiteFailed(key));
      }
    }
    return null;
  }

  private static Integer targetedVariation(
      Map<String, Map<String, TargetHit>> targets, Map<String, KindContext> contexts) {
    TargetHit first = null;
    for (Map.Entry<String, KindContext> entry : contexts.entrySet()) {
      Map<String, TargetHit> keys = targets.get(entry.getKey());
      TargetHit hit = keys == null ? null : keys.get(entry.getValue().key());
      if (hit != null && (first == null || hit.order() < first.order())) {
        first = hit;
      }
    }
    return first == null ? null : first.variation();
  }

  private Result evaluateEntry(FlagEntry entry, Run run, int depth) {
    if (entry.status() == Status.MALFORMED) {
      return Result.failure(ErrorCode.MALFORMED_FLAG);
    }
    Map<?, ?> flag = entry.flag();
    if (!((Boolean) flag.get("on"))) {
      return offResult(flag, Reason.of(ReasonKind.OFF));
    }
    if (entry.status() == Status.BROKEN) {
      return Result.failure(entry.code());
    }
    Result blocked = checkPrerequisites(flag, run, depth);
    if (blocked != null) {
      return blocked;
    }
    Integer targeted = targetedVariation(entry.targets(), run.contexts);
    if (targeted != null) {
      return served(flag, targeted, Reason.of(ReasonKind.TARGET_MATCH));
    }
    List<?> rules = (List<?>) flag.get("rules");
    for (int ruleIndex = 0; ruleIndex < rules.size(); ruleIndex++) {
      Object rule = rules.get(ruleIndex);
      boolean all = true;
      for (Object clause : (List<?>) Validate.field(rule, "clauses")) {
        if (!clauseMatches(clause, run)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(flag, rule, run.contexts, Reason.ruleMatch(ruleIndex));
      }
    }
    return serveResult(
        flag, flag.get("fallthrough"), run.contexts, Reason.of(ReasonKind.FALLTHROUGH));
  }

  private Result evaluateFlag(Run run, String key, int depth) {
    Result completed = run.completed.get(key);
    if (completed != null) {
      return completed;
    }
    if (run.active.contains(key)) {
      return Result.failure(ErrorCode.PREREQUISITE_CYCLE);
    }
    if (depth > MAX_PREREQUISITE_DEPTH) {
      return Result.failure(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED);
    }
    FlagEntry entry = flags.get(key);
    if (entry == null) {
      return Result.failure(ErrorCode.FLAG_NOT_FOUND);
    }
    run.active.add(key);
    Result result = evaluateEntry(entry, run, depth);
    run.active.remove(key);
    run.completed.put(key, result);
    return result;
  }
}
