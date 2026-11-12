package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Broken;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.FlagEntry;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.FlagHeader;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Malformed;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Op;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Ready;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.TargetHit;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.WeightedVariation;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Evaluates a flag for a context, to docs/evaluation.md. {@link #evaluate} returns an error result
 * for every failure the spec names, including a prerequisite cycle.
 */
public final class Evaluator {
  /** Prerequisite links from the requested flag; docs/evaluation.md pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private Evaluator() {}

  private static final class Run {
    final Ruleset ruleset;
    final Map<String, KindContext> contexts;
    final Set<String> active = new HashSet<>();
    final Map<String, Result> completed = new HashMap<>();

    Run(Ruleset ruleset, Map<String, KindContext> contexts) {
      this.ruleset = ruleset;
      this.contexts = contexts;
    }
  }

  /**
   * Evaluates one flag.
   *
   * @param ruleset the ruleset
   * @param flagKey the flag
   * @param context the context as JSON: single ({@code kind}, {@code key}, {@code attributes}; no
   *     kind is user) or multi ({@code kind: multi} and {@code contexts}); one that is not valid is
   *     INVALID_CONTEXT
   * @return the result
   */
  public static Result evaluate(Ruleset ruleset, String flagKey, Json context) {
    Map<String, KindContext> contexts = Contexts.normalise(context);
    if (contexts == null) {
      return Result.failure(ErrorCode.INVALID_CONTEXT);
    }
    return evaluateFlag(new Run(ruleset, contexts), flagKey, 0);
  }

  private static Result served(FlagHeader header, int index, Reason reason) {
    return new Result(header.variations().get(index), index, reason);
  }

  private static Result offResult(FlagHeader header, Reason reason) {
    return header.offVariation() != null
        ? served(header, header.offVariation(), reason)
        : new Result(Json.NULL, null, reason);
  }

  private static int rolloutVariation(List<WeightedVariation> rollout, int position) {
    int chosen = 0;
    double cumulative = 0;
    for (WeightedVariation entry : rollout) {
      chosen = entry.variation();
      cumulative += entry.weight();
      if (position < cumulative) {
        break;
      }
    }
    return chosen;
  }

  private static Result serveResult(Flag flag, Serve serve, Run run, Reason reason) {
    int index =
        serve.rollout() != null
            ? rolloutVariation(
                serve.rollout(),
                Contexts.bucket(
                    flag.header().key(), flag.salt(), serve.rolloutKind(), run.contexts))
            : serve.variation();
    return served(flag.header(), index, reason);
  }

  private static boolean segmentRuleMatches(SegmentRule rule, Segment segment, Run run) {
    for (Clause clause : rule.clauses()) {
      if (!ClauseMatcher.attributeClauseMatches(clause, run.contexts)) {
        return false;
      }
    }
    return rule.weight() == null
        || Contexts.bucket(segment.key(), segment.salt(), rule.rolloutKind(), run.contexts)
            < rule.weight();
  }

  /** Whether the context has, in any of its kinds, a key that the map lists for that kind. */
  private static boolean lists(Map<String, Set<String>> keys, Run run) {
    for (Map.Entry<String, KindContext> entry : run.contexts.entrySet()) {
      Set<String> listed = keys.get(entry.getKey());
      if (listed != null && listed.contains(entry.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  private static boolean segmentMatches(Segment segment, Run run) {
    if (lists(segment.excluded(), run)) {
      return false;
    }
    if (lists(segment.included(), run)) {
      return true;
    }
    for (SegmentRule rule : segment.rules()) {
      if (segmentRuleMatches(rule, segment, run)) {
        return true;
      }
    }
    return false;
  }

  /** The variation of the first target in array order that matches the context, or null. */
  private static Integer targetedVariation(Flag flag, Run run) {
    TargetHit first = null;
    for (Map.Entry<String, KindContext> entry : run.contexts.entrySet()) {
      Map<String, TargetHit> keys = flag.targets().get(entry.getKey());
      TargetHit hit = keys == null ? null : keys.get(entry.getValue().key());
      if (hit != null && (first == null || hit.order() < first.order())) {
        first = hit;
      }
    }
    return first == null ? null : first.variation();
  }

  private static boolean clauseMatches(Clause clause, Run run) {
    if (clause.op() != Op.SEGMENT_MATCH) {
      return ClauseMatcher.attributeClauseMatches(clause, run.contexts);
    }
    boolean matched = false;
    for (String key : clause.segmentKeys()) {
      Segment segment = run.ruleset.segment(key);
      if (segment != null && segmentMatches(segment, run)) {
        matched = true;
        break;
      }
    }
    return clause.negate() != matched;
  }

  private static boolean prerequisiteMet(Prerequisite prerequisite, Result result) {
    ReasonKind kind = result.reason().kind();
    boolean served =
        kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH;
    return served
        && result.variationIndex() != null
        && result.variationIndex() == prerequisite.variation();
  }

  private static Result checkPrerequisites(Flag flag, Run run, int depth) {
    for (Prerequisite prerequisite : flag.prerequisites()) {
      Result result = evaluateFlag(run, prerequisite.flagKey(), depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(flag.header(), Reason.prerequisiteFailed(prerequisite.flagKey()));
      }
    }
    return null;
  }

  private static Result evaluateEntry(FlagEntry entry, Run run, int depth) {
    if (entry instanceof Malformed) {
      return Result.failure(ErrorCode.MALFORMED_FLAG);
    }
    if (entry instanceof Broken broken) {
      return broken.header().on()
          ? Result.failure(broken.code())
          : offResult(broken.header(), Reason.OFF);
    }
    Flag flag = ((Ready) entry).flag();
    if (!flag.header().on()) {
      return offResult(flag.header(), Reason.OFF);
    }
    Result blocked = checkPrerequisites(flag, run, depth);
    if (blocked != null) {
      return blocked;
    }
    Integer targeted = targetedVariation(flag, run);
    if (targeted != null) {
      return served(flag.header(), targeted, Reason.TARGET_MATCH);
    }
    for (int ruleIndex = 0; ruleIndex < flag.rules().size(); ruleIndex++) {
      Rule rule = flag.rules().get(ruleIndex);
      boolean all = true;
      for (Clause clause : rule.clauses()) {
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
    FlagEntry entry = run.ruleset.flag(key);
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
