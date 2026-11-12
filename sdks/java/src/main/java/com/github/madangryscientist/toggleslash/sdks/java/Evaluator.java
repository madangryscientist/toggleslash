package com.github.madangryscientist.toggleslash.sdks.java;

import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Evaluates a flag for a context, to docs/evaluation.md. {@link #evaluate} never throws for a bad
 * ruleset or context: every failure, including a prerequisite cycle, is an {@code ERROR} result.
 */
final class Evaluator {
  /** Prerequisite links from the requested flag; docs/evaluation.md pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private Evaluator() {}

  private static final class Run {
    final Ruleset ruleset;
    final Map<String, ContextKinds.KindContext> contexts;
    final Set<String> active = new HashSet<>();
    final Map<String, EvalResult> completed = new HashMap<>();

    Run(Ruleset ruleset, Map<String, ContextKinds.KindContext> contexts) {
      this.ruleset = ruleset;
      this.contexts = contexts;
    }
  }

  /**
   * Evaluates one flag.
   *
   * @param ruleset the loaded ruleset
   * @param flagKey the flag
   * @param context the context in the JSON model: single ({@code kind}, {@code key}, {@code
   *     attributes}; no kind is {@code user}) or multi; one that is not valid is {@code
   *     INVALID_CONTEXT}
   * @return the result
   */
  static EvalResult evaluate(Ruleset ruleset, String flagKey, Object context) {
    Map<String, ContextKinds.KindContext> contexts = ContextKinds.normalise(context);
    if (contexts == null) {
      return EvalResult.failure(ErrorCode.INVALID_CONTEXT);
    }
    return evaluateFlag(new Run(ruleset, contexts), flagKey, 0);
  }

  private static EvalResult served(Ruleset.FlagHeader header, int index, Reason reason) {
    return new EvalResult(header.variations().get(index), index, reason);
  }

  private static EvalResult offResult(Ruleset.FlagHeader header, Reason reason) {
    return header.offVariation() != null
        ? served(header, header.offVariation(), reason)
        : new EvalResult(null, null, reason);
  }

  private static int rolloutVariation(List<Ruleset.WeightedVariation> rollout, int position) {
    int chosen = 0;
    double cumulative = 0;
    for (Ruleset.WeightedVariation entry : rollout) {
      chosen = entry.variation();
      cumulative += entry.weight();
      if (position < cumulative) {
        break;
      }
    }
    return chosen;
  }

  private static EvalResult serveResult(
      Ruleset.Flag flag, Ruleset.Serve serve, Run run, Reason reason) {
    int index =
        serve.rollout() != null
            ? rolloutVariation(
                serve.rollout(),
                ContextKinds.bucket(
                    flag.header().key(), flag.salt(), serve.rolloutKind(), run.contexts))
            : serve.variation();
    return served(flag.header(), index, reason);
  }

  private static boolean segmentRuleMatches(
      Ruleset.SegmentRule rule, Ruleset.Segment segment, Run run) {
    for (Ruleset.Clause clause : rule.clauses()) {
      if (!ClauseMatcher.attributeClauseMatches(clause, run.contexts)) {
        return false;
      }
    }
    return rule.weight() == null
        || ContextKinds.bucket(segment.key(), segment.salt(), rule.rolloutKind(), run.contexts)
            < rule.weight();
  }

  /**
   * Tells whether the context has, in any of its kinds, a key that {@code keys} lists for that
   * kind.
   */
  private static boolean lists(Map<String, Set<String>> keys, Run run) {
    for (Map.Entry<String, ContextKinds.KindContext> entry : run.contexts.entrySet()) {
      Set<String> listed = keys.get(entry.getKey());
      if (listed != null && listed.contains(entry.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  private static boolean segmentMatches(Ruleset.Segment segment, Run run) {
    if (lists(segment.excluded(), run)) {
      return false;
    }
    if (lists(segment.included(), run)) {
      return true;
    }
    for (Ruleset.SegmentRule rule : segment.rules()) {
      if (segmentRuleMatches(rule, segment, run)) {
        return true;
      }
    }
    return false;
  }

  /** The variation of the first target in array order that matches the context, or null. */
  private static Integer targetedVariation(Ruleset.Flag flag, Run run) {
    Ruleset.TargetHit first = null;
    for (Map.Entry<String, ContextKinds.KindContext> entry : run.contexts.entrySet()) {
      Map<String, Ruleset.TargetHit> keys = flag.targets().get(entry.getKey());
      if (keys == null) {
        continue;
      }
      Ruleset.TargetHit hit = keys.get(entry.getValue().key());
      if (hit != null && (first == null || hit.order() < first.order())) {
        first = hit;
      }
    }
    return first == null ? null : first.variation();
  }

  private static boolean clauseMatches(Ruleset.Clause clause, Run run) {
    if (clause.op() != Ruleset.Op.SEGMENT_MATCH) {
      return ClauseMatcher.attributeClauseMatches(clause, run.contexts);
    }
    boolean matched = false;
    for (String key : clause.segmentKeys()) {
      Ruleset.Segment segment = run.ruleset.segment(key);
      if (segment != null && segmentMatches(segment, run)) {
        matched = true;
        break;
      }
    }
    return clause.negate() != matched;
  }

  private static boolean prerequisiteMet(Ruleset.Prerequisite prerequisite, EvalResult result) {
    ReasonKind kind = result.reason().kind();
    return (kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH)
        && result.variationIndex() != null
        && result.variationIndex() == prerequisite.variation();
  }

  private static EvalResult checkPrerequisites(Ruleset.Flag flag, Run run, int depth) {
    for (Ruleset.Prerequisite prerequisite : flag.prerequisites()) {
      EvalResult result = evaluateFlag(run, prerequisite.flagKey(), depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(flag.header(), Reason.prerequisiteFailed(prerequisite.flagKey()));
      }
    }
    return null;
  }

  private static EvalResult evaluateEntry(Ruleset.FlagEntry entry, Run run, int depth) {
    if (entry.header() == null) {
      return EvalResult.failure(ErrorCode.MALFORMED_FLAG);
    }
    if (entry.flag() == null) {
      return entry.header().on()
          ? EvalResult.failure(entry.code())
          : offResult(entry.header(), Reason.OFF);
    }
    Ruleset.Flag flag = entry.flag();
    if (!flag.header().on()) {
      return offResult(flag.header(), Reason.OFF);
    }
    EvalResult blocked = checkPrerequisites(flag, run, depth);
    if (blocked != null) {
      return blocked;
    }
    Integer targeted = targetedVariation(flag, run);
    if (targeted != null) {
      return served(flag.header(), targeted, Reason.TARGET_MATCH);
    }
    for (int ruleIndex = 0; ruleIndex < flag.rules().size(); ruleIndex++) {
      Ruleset.Rule rule = flag.rules().get(ruleIndex);
      boolean all = true;
      for (Ruleset.Clause clause : rule.clauses()) {
        if (!clauseMatches(clause, run)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(flag, rule.serve(), run, Reason.ruleMatch(ruleIndex));
      }
    }
    return serveResult(flag, flag.fallthrough(), run, Reason.FALLTHROUGH);
  }

  /** Entering a flag checks, in this order: finished, in progress, too deep, missing. */
  private static EvalResult evaluateFlag(Run run, String key, int depth) {
    EvalResult completed = run.completed.get(key);
    if (completed != null) {
      return completed;
    }
    if (run.active.contains(key)) {
      return EvalResult.failure(ErrorCode.PREREQUISITE_CYCLE);
    }
    if (depth > MAX_PREREQUISITE_DEPTH) {
      return EvalResult.failure(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED);
    }
    Ruleset.FlagEntry entry = run.ruleset.flag(key);
    if (entry == null) {
      return EvalResult.failure(ErrorCode.FLAG_NOT_FOUND);
    }
    run.active.add(key);
    EvalResult result = evaluateEntry(entry, run, depth);
    run.active.remove(key);
    run.completed.put(key, result);
    return result;
  }
}
