package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.FlagEntry;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Rollout;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.SegmentClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Status;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Evaluates one flag for one context, to {@code docs/evaluation.md}. {@link #evaluate} returns an
 * {@code ERROR} result for every failure the spec names; it can still raise on a bug, and the
 * client turns that into {@code EVALUATION_FAILED}.
 */
public final class Evaluator {
  /** Prerequisite links from the requested flag; the spec pins the limit. */
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

  private static Result failure(ErrorCode code) {
    return new Result(null, null, Reason.error(code));
  }

  private static Result served(List<Object> variations, int index, Reason reason) {
    return new Result(index < variations.size() ? variations.get(index) : null, index, reason);
  }

  private static Result offResult(FlagEntry entry, Reason reason) {
    if (entry.offVariation < 0) {
      return new Result(null, null, reason);
    }
    return served(entry.variations, entry.offVariation, reason);
  }

  private static int rolloutVariation(Rollout rollout, int position) {
    int chosen = 0;
    int cumulative = 0;
    for (int i = 0; i < rollout.variations().length; i++) {
      chosen = rollout.variations()[i];
      cumulative += rollout.weights()[i];
      if (position < cumulative) {
        break;
      }
    }
    return chosen;
  }

  private static Result serveResult(
      Flag flag, Serve serve, Map<String, KindContext> contexts, Reason reason) {
    Rollout rollout = serve.rollout();
    int index =
        rollout != null
            ? rolloutVariation(
                rollout,
                Contexts.kindBucket(flag.key(), flag.salt(), rollout.contextKind(), contexts))
            : serve.variation();
    return served(flag.variations(), index, reason);
  }

  private static boolean segmentRuleMatches(
      SegmentRule rule, Segment segment, Map<String, KindContext> contexts) {
    for (AttributeClause clause : rule.clauses()) {
      if (!Clauses.attributeClauseMatches(clause, contexts)) {
        return false;
      }
    }
    if (rule.weight() < 0) {
      return true;
    }
    return Contexts.kindBucket(segment.key(), segment.salt(), rule.rolloutContextKind(), contexts)
        < rule.weight();
  }

  private static boolean segmentMatches(Segment segment, Map<String, KindContext> contexts) {
    if (Ruleset.listsContext(segment.excluded(), contexts)) {
      return false;
    }
    if (Ruleset.listsContext(segment.included(), contexts)) {
      return true;
    }
    for (SegmentRule rule : segment.rules()) {
      if (segmentRuleMatches(rule, segment, contexts)) {
        return true;
      }
    }
    return false;
  }

  private static boolean clauseMatches(Clause clause, Run run) {
    if (clause instanceof AttributeClause attribute) {
      return Clauses.attributeClauseMatches(attribute, run.contexts);
    }
    SegmentClause segmentClause = (SegmentClause) clause;
    boolean matched = false;
    for (String key : segmentClause.values()) {
      Segment segment = run.ruleset.segments.get(key);
      if (segment != null && segmentMatches(segment, run.contexts)) {
        matched = true;
        break;
      }
    }
    return segmentClause.negate() ? !matched : matched;
  }

  private static boolean prerequisiteMet(Prerequisite prerequisite, Result result) {
    ReasonKind kind = result.reason().kind();
    boolean reachedServe =
        kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH;
    return reachedServe
        && result.variationIndex() != null
        && result.variationIndex() == prerequisite.variation();
  }

  private static Result checkPrerequisites(FlagEntry entry, Run run, int depth) {
    for (Prerequisite prerequisite : entry.flag.prerequisites()) {
      Result result = evaluateFlag(run, prerequisite.flagKey(), depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(entry, Reason.prerequisiteFailed(prerequisite.flagKey()));
      }
    }
    return null;
  }

  private static Result evaluateEntry(FlagEntry entry, Run run, int depth) {
    if (entry.status == Status.MALFORMED) {
      return failure(ErrorCode.MALFORMED_FLAG);
    }
    if (!entry.on) {
      return offResult(entry, Reason.of(ReasonKind.OFF));
    }
    if (entry.status == Status.BROKEN) {
      return failure(entry.code);
    }
    Flag flag = entry.flag;
    Result blocked = checkPrerequisites(entry, run, depth);
    if (blocked != null) {
      return blocked;
    }
    int targeted = Ruleset.targetedVariation(entry.targets, run.contexts);
    if (targeted >= 0) {
      return served(flag.variations(), targeted, Reason.of(ReasonKind.TARGET_MATCH));
    }
    List<Rule> rules = flag.rules();
    for (int i = 0; i < rules.size(); i++) {
      Rule rule = rules.get(i);
      boolean all = true;
      for (Clause clause : rule.clauses()) {
        if (!clauseMatches(clause, run)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(flag, rule.serve(), run.contexts, Reason.rule(i));
      }
    }
    return serveResult(flag, flag.fallthrough(), run.contexts, Reason.of(ReasonKind.FALLTHROUGH));
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
    FlagEntry entry = run.ruleset.flags.get(key);
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
   * Evaluates one flag for one context.
   *
   * @param ruleset the ruleset
   * @param flagKey the flag
   * @param context the context as the application gave it
   * @return the result
   */
  public static Result evaluate(Ruleset ruleset, String flagKey, Object context) {
    Map<String, KindContext> contexts = Contexts.normalise(context);
    if (contexts == null) {
      return failure(ErrorCode.INVALID_CONTEXT);
    }
    return evaluateFlag(new Run(ruleset, contexts), flagKey, 0);
  }
}
