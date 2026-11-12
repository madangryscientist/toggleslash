package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Flag evaluation, to docs/evaluation.md. It never throws for a ruleset or a context of any shape:
 * every failure, a prerequisite cycle included, is an {@code ERROR} result.
 */
public final class Evaluator {
  /** Prerequisite links from the requested flag; docs/evaluation.md pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private Evaluator() {}

  /** The state of one evaluation: the context, the flags in progress and the ones finished. */
  private static final class Run {
    final Ruleset ruleset;
    final Map<String, Contexts.KindContext> contexts;
    final Set<String> active = new HashSet<>();
    final Map<String, Result> completed = new HashMap<>();

    Run(Ruleset ruleset, Map<String, Contexts.KindContext> contexts) {
      this.ruleset = ruleset;
      this.contexts = contexts;
    }
  }

  /**
   * Evaluates one flag for one context.
   *
   * @param ruleset the ruleset, from {@link Ruleset#create(Object)}
   * @param flagKey the flag key
   * @param context the context as parsed JSON: an object with a {@code key}, or a multi-context
   * @return the value, the variation index and the reason
   */
  public static Result evaluate(Ruleset ruleset, String flagKey, Object context) {
    Map<String, Contexts.KindContext> contexts = Contexts.normalise(context);
    if (contexts == null) {
      return Result.failure(ErrorCode.INVALID_CONTEXT);
    }
    return evaluateFlag(new Run(ruleset, contexts), flagKey, 0);
  }

  private static Result served(List<?> variations, int index, Reason reason) {
    Object value = index >= 0 && index < variations.size() ? variations.get(index) : null;
    return new Result(value, index, reason);
  }

  private static Result offResult(Object flag, Reason reason) {
    Object index = Js.member(flag, "offVariation");
    if (index == null) {
      return new Result(null, null, reason);
    }
    return served(Js.array(Js.member(flag, "variations")), (int) Js.number(index), reason);
  }

  private static int rolloutVariation(Object rollout, int position) {
    int chosen = 0;
    double cumulative = 0;
    for (Object item : Js.array(Js.member(rollout, "variations"))) {
      chosen = (int) Js.number(Js.member(item, "variation"));
      cumulative += Js.number(Js.member(item, "weight"));
      if (position < cumulative) {
        break;
      }
    }
    return chosen;
  }

  private static Result serveResult(
      Object flag, Object serve, Map<String, Contexts.KindContext> contexts, Reason reason) {
    Object rollout = Js.member(serve, "rollout");
    int index;
    if (rollout != null) {
      Object kind = Js.member(rollout, "contextKind");
      int position =
          Contexts.kindBucket(
              (String) Js.member(flag, "key"),
              (String) Js.member(flag, "salt"),
              (String) kind,
              contexts);
      index = rolloutVariation(rollout, position);
    } else {
      index = (int) Js.number(Js.member(serve, "variation"));
    }
    return served(Js.array(Js.member(flag, "variations")), index, reason);
  }

  private static boolean segmentRuleMatches(
      Object rule, Ruleset.SegmentEntry ready, Map<String, Contexts.KindContext> contexts) {
    for (Object clause : Js.array(Js.member(rule, "clauses"))) {
      if (!Clauses.attributeClauseMatches(clause, contexts)) {
        return false;
      }
    }
    Object weight = Js.member(rule, "weight");
    if (weight == null) {
      return true;
    }
    int position =
        Contexts.kindBucket(
            (String) Js.member(ready.segment, "key"),
            (String) Js.member(ready.segment, "salt"),
            (String) Js.member(rule, "rolloutContextKind"),
            contexts);
    return position < Js.number(weight);
  }

  private static boolean segmentMatches(
      Ruleset.SegmentEntry ready, Map<String, Contexts.KindContext> contexts) {
    if (Ruleset.listsContext(ready.excluded, contexts)) {
      return false;
    }
    if (Ruleset.listsContext(ready.included, contexts)) {
      return true;
    }
    for (Object rule : Js.array(Js.member(ready.segment, "rules"))) {
      if (segmentRuleMatches(rule, ready, contexts)) {
        return true;
      }
    }
    return false;
  }

  private static boolean clauseMatches(Object clause, Run run) {
    if (!"segmentMatch".equals(Js.member(clause, "op"))) {
      return Clauses.attributeClauseMatches(clause, run.contexts);
    }
    boolean matched = false;
    for (Object key : Js.array(Js.member(clause, "values"))) {
      Ruleset.SegmentEntry ready = run.ruleset.segment((String) key);
      if (ready != null && !ready.malformed && segmentMatches(ready, run.contexts)) {
        matched = true;
        break;
      }
    }
    return Boolean.TRUE.equals(Js.member(clause, "negate")) ? !matched : matched;
  }

  private static boolean prerequisiteMet(Object prerequisite, Result result) {
    ReasonKind kind = result.reason().kind();
    boolean reachedServe =
        kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH;
    return reachedServe
        && result.variationIndex() != null
        && result.variationIndex() == (int) Js.number(Js.member(prerequisite, "variation"));
  }

  private static Result checkPrerequisites(Object flag, Run run, int depth) {
    for (Object prerequisite : Js.array(Js.member(flag, "prerequisites"))) {
      String key = (String) Js.member(prerequisite, "flagKey");
      Result result = evaluateFlag(run, key, depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(flag, new Reason(ReasonKind.PREREQUISITE_FAILED, null, key, null));
      }
    }
    return null;
  }

  private static Result evaluateEntry(Ruleset.FlagEntry entry, Run run, int depth) {
    if (entry.status == Ruleset.Status.MALFORMED) {
      return Result.failure(ErrorCode.MALFORMED_FLAG);
    }
    Object flag = entry.flag;
    if (!Boolean.TRUE.equals(Js.member(flag, "on"))) {
      return offResult(flag, Reason.of(ReasonKind.OFF));
    }
    if (entry.status == Ruleset.Status.BROKEN) {
      return Result.failure(entry.code);
    }
    Result blocked = checkPrerequisites(flag, run, depth);
    if (blocked != null) {
      return blocked;
    }
    List<?> variations = Js.array(Js.member(flag, "variations"));
    int targeted = Ruleset.targetedVariation(entry.targets, run.contexts);
    if (targeted >= 0) {
      return served(variations, targeted, Reason.of(ReasonKind.TARGET_MATCH));
    }
    List<?> rules = Js.array(Js.member(flag, "rules"));
    for (int index = 0; index < rules.size(); index++) {
      Object rule = rules.get(index);
      boolean all = true;
      for (Object clause : Js.array(Js.member(rule, "clauses"))) {
        if (!clauseMatches(clause, run)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(
            flag, rule, run.contexts, new Reason(ReasonKind.RULE_MATCH, index, null, null));
      }
    }
    return serveResult(
        flag, Js.member(flag, "fallthrough"), run.contexts, Reason.of(ReasonKind.FALLTHROUGH));
  }

  private static Result evaluateFlag(Run run, String key, int depth) {
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
    Ruleset.FlagEntry entry = run.ruleset.flag(key);
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
