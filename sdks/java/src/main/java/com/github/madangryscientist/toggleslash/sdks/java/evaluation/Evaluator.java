package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Evaluates one flag for one context, to {@code docs/evaluation.md}. It never throws: every
 * failure, including a prerequisite cycle, is an {@code ERROR} result.
 */
public final class Evaluator {
  /** Prerequisite links from the requested flag; the spec pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private Evaluator() {}

  private static final class Run {
    final Ruleset ruleset;
    final Contexts contexts;
    final Set<String> active = new HashSet<>();
    final Map<String, Result> completed = new HashMap<>();

    Run(Ruleset ruleset, Contexts contexts) {
      this.ruleset = ruleset;
      this.contexts = contexts;
    }
  }

  private static Result failure(ErrorCode code) {
    return new Result(null, null, Reason.error(code));
  }

  private static Result served(List<Object> variations, int index, Reason reason) {
    return new Result(variations.get(index), index, reason);
  }

  private static Result offResult(Ruleset.Header header, Reason reason) {
    Integer index = header.offVariation();
    if (index == null) {
      return new Result(null, null, reason);
    }
    return served(header.variations(), index, reason);
  }

  private static int rolloutVariation(Ruleset.Rollout rollout, int position) {
    int chosen = 0;
    long cumulative = 0;
    for (Ruleset.Weighted weighted : rollout.variations()) {
      chosen = weighted.variation();
      cumulative += weighted.weight();
      if (position < cumulative) {
        break;
      }
    }
    return chosen;
  }

  private static Result serveResult(
      Ruleset.Flag flag, Ruleset.Serve serve, Contexts contexts, Reason reason) {
    int index;
    if (serve.rollout() != null) {
      int position =
          contexts.kindBucket(flag.header.key(), flag.salt, serve.rollout().contextKind());
      index = rolloutVariation(serve.rollout(), position);
    } else {
      index = serve.variation();
    }
    return served(flag.header.variations(), index, reason);
  }

  private static boolean listsContext(Map<String, Set<String>> keys, Contexts contexts) {
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.entries()) {
      Set<String> set = keys.get(entry.getKey());
      if (set != null && set.contains(entry.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  private static boolean segmentRuleMatches(
      Ruleset.SegmentRule rule, Ruleset.ReadySegment segment, Contexts contexts) {
    for (Ruleset.Clause clause : rule.clauses()) {
      if (!Clauses.attributeClauseMatches(clause, contexts)) {
        return false;
      }
    }
    if (rule.weight() == null) {
      return true;
    }
    return contexts.kindBucket(segment.key, segment.salt, rule.rolloutContextKind())
        < rule.weight();
  }

  private static boolean segmentMatches(Ruleset.ReadySegment segment, Contexts contexts) {
    if (listsContext(segment.excluded, contexts)) {
      return false;
    }
    if (listsContext(segment.included, contexts)) {
      return true;
    }
    for (Ruleset.SegmentRule rule : segment.rules) {
      if (segmentRuleMatches(rule, segment, contexts)) {
        return true;
      }
    }
    return false;
  }

  private static boolean clauseMatches(Ruleset.Clause clause, Run run) {
    if (clause.op != Ruleset.Op.SEGMENT_MATCH) {
      return Clauses.attributeClauseMatches(clause, run.contexts);
    }
    boolean matched = false;
    for (String key : clause.segmentKeys) {
      Ruleset.ReadySegment segment = run.ruleset.segments.get(key);
      if (segment != null
          && !Ruleset.isMalformed(segment)
          && segmentMatches(segment, run.contexts)) {
        matched = true;
        break;
      }
    }
    return clause.negate ? !matched : matched;
  }

  private static boolean prerequisiteMet(Ruleset.Prerequisite prerequisite, Result result) {
    ReasonKind kind = result.reason().kind();
    boolean reachedServe =
        kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH;
    return reachedServe
        && result.variationIndex() != null
        && result.variationIndex().doubleValue() == prerequisite.variation();
  }

  private static Result checkPrerequisites(Ruleset.Flag flag, Run run, int depth) {
    for (Ruleset.Prerequisite prerequisite : flag.prerequisites) {
      Result result = evaluateFlag(run, prerequisite.flagKey(), depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(flag.header, Reason.prerequisiteFailed(prerequisite.flagKey()));
      }
    }
    return null;
  }

  private static Integer targetedVariation(Ruleset.Flag flag, Contexts contexts) {
    int[] first = null;
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.entries()) {
      Map<String, int[]> keys = flag.targetIndex.get(entry.getKey());
      int[] hit = keys == null ? null : keys.get(entry.getValue().key());
      if (hit != null && (first == null || hit[0] < first[0])) {
        first = hit;
      }
    }
    return first == null ? null : first[1];
  }

  private static Result evaluateEntry(Ruleset.Entry entry, Run run, int depth) {
    if (entry.isMalformed()) {
      return failure(ErrorCode.MALFORMED_FLAG);
    }
    if (!entry.header.on()) {
      return offResult(entry.header, Reason.OFF);
    }
    if (entry.isBroken()) {
      return failure(entry.brokenCode);
    }
    Ruleset.Flag flag = entry.flag;
    Result blocked = checkPrerequisites(flag, run, depth);
    if (blocked != null) {
      return blocked;
    }
    Integer targeted = targetedVariation(flag, run.contexts);
    if (targeted != null) {
      return served(flag.header.variations(), targeted, Reason.TARGET_MATCH);
    }
    for (int ruleIndex = 0; ruleIndex < flag.rules.size(); ruleIndex++) {
      Ruleset.Rule rule = flag.rules.get(ruleIndex);
      boolean all = true;
      for (Ruleset.Clause clause : rule.clauses()) {
        if (!clauseMatches(clause, run)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(flag, rule.serve(), run.contexts, Reason.rule(ruleIndex));
      }
    }
    return serveResult(flag, flag.fallthrough, run.contexts, Reason.FALLTHROUGH);
  }

  private static Result evaluateFlag(Run run, String key, int depth) {
    Result completed = run.completed.get(key);
    if (completed != null) {
      return completed;
    }
    if (run.active.contains(key)) {
      return failure(ErrorCode.PREREQUISITE_CYCLE);
    }
    if (depth > MAX_PREREQUISITE_DEPTH) {
      return failure(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED);
    }
    Ruleset.Entry entry = run.ruleset.flags.get(key);
    if (entry == null) {
      return failure(ErrorCode.FLAG_NOT_FOUND);
    }
    run.active.add(key);
    Result result = evaluateEntry(entry, run, depth);
    run.active.remove(key);
    run.completed.put(key, result);
    return result;
  }

  /**
   * Evaluates the flag for the context.
   *
   * @param ruleset the validated ruleset
   * @param flagKey the flag
   * @param context a context as the JSON parser hands it over (a map), or null
   * @return the result, never null
   */
  public static Result evaluate(Ruleset ruleset, String flagKey, Object context) {
    Contexts contexts = Contexts.normalise(context);
    if (contexts == null) {
      return failure(ErrorCode.INVALID_CONTEXT);
    }
    return evaluateFlag(new Run(ruleset, contexts), flagKey, 0);
  }
}
