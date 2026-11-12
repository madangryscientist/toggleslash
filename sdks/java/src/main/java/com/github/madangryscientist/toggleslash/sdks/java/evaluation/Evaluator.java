package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Contexts.KindContext;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.AttributeClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rollout;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Rule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentClause;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Model.TargetHit;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.FlagEntry;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.OptionalInt;
import java.util.Set;

/** Flag evaluation, to the evaluation spec. It is stateless and never throws. */
public final class Evaluator {

  /** Prerequisite links from the requested flag; the spec pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private Evaluator() {}

  private static final class Run {
    final Ruleset ruleset;
    final Map<String, KindContext> contexts;
    final Set<String> active = new HashSet<>();
    final Map<String, EvaluationResult> completed = new HashMap<>();

    Run(Ruleset ruleset, Map<String, KindContext> contexts) {
      this.ruleset = ruleset;
      this.contexts = contexts;
    }
  }

  /**
   * Evaluates one flag for one context. It never throws: every failure, including a prerequisite
   * cycle and a context object that misbehaves, is an {@code ERROR} result.
   *
   * @param context a {@code Map} in the context form of the spec: {@code kind}, {@code key}, {@code
   *     attributes}, or a multi context
   */
  public static EvaluationResult evaluate(Ruleset ruleset, String flagKey, Object context) {
    try {
      Map<String, KindContext> contexts = Contexts.normalise(context);
      if (contexts == null) {
        return EvaluationResult.failure(ErrorCode.INVALID_CONTEXT);
      }
      return evaluateFlag(new Run(ruleset, contexts), flagKey, 0);
    } catch (RuntimeException | StackOverflowError e) {
      return EvaluationResult.failure(ErrorCode.EVALUATION_FAILED);
    }
  }

  private static EvaluationResult served(List<Object> variations, int index, Reason reason) {
    return new EvaluationResult(variations.get(index), OptionalInt.of(index), reason);
  }

  private static EvaluationResult offResult(Header header, Reason reason) {
    if (header.offVariation() < 0) {
      return new EvaluationResult(null, OptionalInt.empty(), reason);
    }
    return served(header.variations(), header.offVariation(), reason);
  }

  private static int rolloutVariation(Rollout rollout, int position) {
    int chosen = 0;
    long cumulative = 0;
    for (int i = 0; i < rollout.variations().length; i++) {
      chosen = rollout.variations()[i];
      cumulative += rollout.weights()[i];
      if (position < cumulative) {
        break;
      }
    }
    return chosen;
  }

  private static EvaluationResult serveResult(
      Flag flag, Serve serve, Map<String, KindContext> contexts, Reason reason) {
    Rollout rollout = serve.rollout();
    int index =
        rollout == null
            ? serve.variation()
            : rolloutVariation(
                rollout,
                Contexts.kindBucket(
                    flag.header().key(), flag.salt(), rollout.contextKind(), contexts));
    return served(flag.header().variations(), index, reason);
  }

  private static boolean listsContext(
      Map<String, Set<String>> keys, Map<String, KindContext> contexts) {
    for (Map.Entry<String, KindContext> entry : contexts.entrySet()) {
      Set<String> listed = keys.get(entry.getKey());
      if (listed != null && listed.contains(entry.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  private static boolean segmentRuleMatches(
      SegmentRule rule, Segment segment, Map<String, KindContext> contexts) {
    for (AttributeClause clause : rule.clauses()) {
      if (!Clauses.matches(clause, contexts)) {
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
    if (listsContext(segment.excluded(), contexts)) {
      return false;
    }
    if (listsContext(segment.included(), contexts)) {
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
      return Clauses.matches(attribute, run.contexts);
    }
    SegmentClause segmentClause = (SegmentClause) clause;
    boolean matched = false;
    for (String key : segmentClause.values()) {
      Segment segment = run.ruleset.segment(key);
      if (segment != null && segmentMatches(segment, run.contexts)) {
        matched = true;
        break;
      }
    }
    return segmentClause.negate() != matched;
  }

  private static boolean prerequisiteMet(Prerequisite prerequisite, EvaluationResult result) {
    ReasonKind kind = result.reason().kind();
    boolean reachedServe =
        kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH;
    return reachedServe
        && result.variationIndex().isPresent()
        && result.variationIndex().getAsInt() == prerequisite.variation();
  }

  private static EvaluationResult checkPrerequisites(Header header, Flag flag, Run run, int depth) {
    for (Prerequisite prerequisite : flag.prerequisites()) {
      EvaluationResult result = evaluateFlag(run, prerequisite.flagKey(), depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(header, Reason.prerequisiteFailed(prerequisite.flagKey()));
      }
    }
    return null;
  }

  /** The variation of the first target in array order that matches the context, or -1. */
  private static int targetedVariation(
      Map<String, Map<String, TargetHit>> targets, Map<String, KindContext> contexts) {
    TargetHit first = null;
    for (Map.Entry<String, KindContext> entry : contexts.entrySet()) {
      Map<String, TargetHit> keys = targets.get(entry.getKey());
      TargetHit hit = keys == null ? null : keys.get(entry.getValue().key());
      if (hit != null && (first == null || hit.order() < first.order())) {
        first = hit;
      }
    }
    return first == null ? -1 : first.variation();
  }

  private static EvaluationResult evaluateEntry(FlagEntry entry, Run run, int depth) {
    if (entry.status() == Ruleset.Status.MALFORMED) {
      return EvaluationResult.failure(ErrorCode.MALFORMED_FLAG);
    }
    Header header = entry.header();
    if (!header.on()) {
      return offResult(header, Reason.off());
    }
    if (entry.status() == Ruleset.Status.BROKEN) {
      return EvaluationResult.failure(entry.code());
    }
    Flag flag = entry.flag();
    EvaluationResult blocked = checkPrerequisites(header, flag, run, depth);
    if (blocked != null) {
      return blocked;
    }
    int targeted = targetedVariation(flag.targets(), run.contexts);
    if (targeted >= 0) {
      return served(header.variations(), targeted, Reason.targetMatch());
    }
    for (int i = 0; i < flag.rules().size(); i++) {
      Rule rule = flag.rules().get(i);
      boolean all = true;
      for (Clause clause : rule.clauses()) {
        if (!clauseMatches(clause, run)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(flag, rule.serve(), run.contexts, Reason.ruleMatch(i));
      }
    }
    return serveResult(flag, flag.fallthrough(), run.contexts, Reason.fallthrough());
  }

  private static EvaluationResult evaluateFlag(Run run, String key, int depth) {
    EvaluationResult completed = run.completed.get(key);
    if (completed != null) {
      return completed;
    }
    if (run.active.contains(key)) {
      return EvaluationResult.failure(ErrorCode.PREREQUISITE_CYCLE);
    }
    if (depth > MAX_PREREQUISITE_DEPTH) {
      return EvaluationResult.failure(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED);
    }
    FlagEntry entry = run.ruleset.flag(key);
    if (entry == null) {
      return EvaluationResult.failure(ErrorCode.FLAG_NOT_FOUND);
    }
    run.active.add(key);
    EvaluationResult result = evaluateEntry(entry, run, depth);
    run.active.remove(key);
    run.completed.put(key, result);
    return result;
  }
}
