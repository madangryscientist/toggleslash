package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rollout;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.FlagEntry;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.ReadySegment;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.SegmentEntry;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Status;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.TargetHit;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Evaluates one flag for one context, to the evaluation spec. */
public final class Evaluator {
  /** Prerequisite links from the requested flag; the spec pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private final Ruleset ruleset;
  private final Contexts contexts;
  private final Set<String> active = new HashSet<>();
  private final Map<String, Result> completed = new HashMap<>();

  private Evaluator(Ruleset ruleset, Contexts contexts) {
    this.ruleset = ruleset;
    this.contexts = contexts;
  }

  /**
   * Evaluates one flag for one context. Every failure the spec names, a prerequisite cycle
   * included, is an error result.
   *
   * @param ruleset the ruleset
   * @param flagKey the flag's key
   * @param context a context: a {@link Map} with an optional {@code kind}, a {@code key} and
   *     optional {@code attributes}, or a multi context
   * @return the result
   */
  public static Result evaluate(Ruleset ruleset, String flagKey, Object context) {
    Contexts normalised = Contexts.normalise(context);
    if (normalised == null) {
      return Result.failure(ErrorCode.INVALID_CONTEXT);
    }
    return new Evaluator(ruleset, normalised).evaluateFlag(flagKey, 0);
  }

  private static Result served(List<?> variations, int index, Reason reason) {
    return new Result(variations.get(index), index, reason);
  }

  private static Result offResult(Header header, Reason reason) {
    if (header.offVariation < 0) {
      return new Result(null, null, reason);
    }
    return served(header.variations, header.offVariation, reason);
  }

  private static int rolloutVariation(Rollout rollout, int position) {
    int chosen = 0;
    long cumulative = 0;
    for (int i = 0; i < rollout.variations.length; i++) {
      chosen = rollout.variations[i];
      cumulative += rollout.weights[i];
      if (position < cumulative) {
        break;
      }
    }
    return chosen;
  }

  private Result serveResult(Flag flag, Serve serve, Reason reason) {
    int index =
        serve.rollout == null
            ? serve.variation
            : rolloutVariation(
                serve.rollout,
                contexts.kindBucket(flag.header.key, flag.salt, serve.rollout.contextKind));
    return served(flag.header.variations, index, reason);
  }

  private boolean segmentRuleMatches(SegmentRule rule, ReadySegment ready) {
    for (AttributeClause clause : rule.clauses) {
      if (!Clauses.matches(clause, contexts)) {
        return false;
      }
    }
    if (rule.weight < 0) {
      return true;
    }
    return contexts.kindBucket(ready.segment.key, ready.segment.salt, rule.rolloutContextKind)
        < rule.weight;
  }

  private boolean segmentMatches(ReadySegment ready) {
    if (contexts.listedIn(ready.excluded)) {
      return false;
    }
    if (contexts.listedIn(ready.included)) {
      return true;
    }
    for (SegmentRule rule : ready.segment.rules) {
      if (segmentRuleMatches(rule, ready)) {
        return true;
      }
    }
    return false;
  }

  private boolean clauseMatches(Clause clause) {
    if (clause instanceof AttributeClause) {
      return Clauses.matches((AttributeClause) clause, contexts);
    }
    SegmentClause segmentClause = (SegmentClause) clause;
    boolean matched = false;
    for (String key : segmentClause.values) {
      SegmentEntry entry = ruleset.segments.get(key);
      if (entry != null && entry.ready != null && segmentMatches(entry.ready)) {
        matched = true;
        break;
      }
    }
    return segmentClause.negate ? !matched : matched;
  }

  private static boolean prerequisiteMet(Prerequisite prerequisite, Result result) {
    ReasonKind kind = result.reason().kind();
    boolean reachedServe =
        kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH;
    return reachedServe
        && result.variationIndex() != null
        && result.variationIndex() == prerequisite.variation;
  }

  private Result checkPrerequisites(Flag flag, int depth) {
    for (Prerequisite prerequisite : flag.prerequisites) {
      Result result = evaluateFlag(prerequisite.flagKey, depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(flag.header, Reason.prerequisiteFailed(prerequisite.flagKey));
      }
    }
    return null;
  }

  private static Integer targetedVariation(
      Map<String, Map<String, TargetHit>> targets, Contexts contexts) {
    TargetHit first = null;
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.byKind.entrySet()) {
      Map<String, TargetHit> byKey = targets.get(entry.getKey());
      TargetHit hit = byKey == null ? null : byKey.get(entry.getValue().key);
      if (hit != null && (first == null || hit.order < first.order)) {
        first = hit;
      }
    }
    return first == null ? null : first.variation;
  }

  private Result evaluateEntry(FlagEntry entry, int depth) {
    if (entry.status == Status.MALFORMED) {
      return Result.failure(ErrorCode.MALFORMED_FLAG);
    }
    if (!entry.header.on) {
      return offResult(entry.header, Reason.OFF);
    }
    if (entry.status == Status.BROKEN) {
      return Result.failure(entry.code);
    }
    Flag flag = entry.flag;
    Result blocked = checkPrerequisites(flag, depth);
    if (blocked != null) {
      return blocked;
    }
    Integer targeted = targetedVariation(entry.targets, contexts);
    if (targeted != null) {
      return served(flag.header.variations, targeted, Reason.TARGET_MATCH);
    }
    for (int ruleIndex = 0; ruleIndex < flag.rules.size(); ruleIndex++) {
      Rule rule = flag.rules.get(ruleIndex);
      boolean all = true;
      for (Clause clause : rule.clauses) {
        if (!clauseMatches(clause)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(flag, rule.serve, Reason.ruleMatch(ruleIndex));
      }
    }
    return serveResult(flag, flag.fallthrough, Reason.FALLTHROUGH);
  }

  private Result evaluateFlag(String key, int depth) {
    Result done = completed.get(key);
    if (done != null) {
      return done;
    }
    if (active.contains(key)) {
      return Result.failure(ErrorCode.PREREQUISITE_CYCLE);
    }
    if (depth > MAX_PREREQUISITE_DEPTH) {
      return Result.failure(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED);
    }
    FlagEntry entry = ruleset.flags.get(key);
    if (entry == null) {
      return Result.failure(ErrorCode.FLAG_NOT_FOUND);
    }
    active.add(key);
    Result result = evaluateEntry(entry, depth);
    active.remove(key);
    completed.put(key, result);
    return result;
  }
}
