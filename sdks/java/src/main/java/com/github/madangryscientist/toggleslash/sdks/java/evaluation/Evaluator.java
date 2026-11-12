package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.ReasonKind;
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
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.Entry;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Ruleset.SegmentEntry;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Evaluates one flag for one context, to {@code docs/evaluation.md}. It does not throw for any
 * ruleset or context.
 */
public final class Evaluator {
  /** Prerequisite links from the requested flag; the spec pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private final Ruleset ruleset;
  private final Map<String, KindContext> contexts;
  private final Set<String> active = new HashSet<>();
  private final Map<String, Result> completed = new HashMap<>();

  private Evaluator(Ruleset ruleset, Map<String, KindContext> contexts) {
    this.ruleset = ruleset;
    this.contexts = contexts;
  }

  /**
   * Evaluates a flag for a context. A failure, including a prerequisite cycle, is an {@code ERROR}
   * result.
   *
   * @param ruleset the ruleset
   * @param flagKey the flag
   * @param context the context as a JSON value (a map)
   * @return the result
   */
  public static Result evaluate(Ruleset ruleset, String flagKey, Object context) {
    Map<String, KindContext> contexts = Contexts.normalise(context);
    if (contexts == null) {
      return failure(ErrorCode.INVALID_CONTEXT);
    }
    return new Evaluator(ruleset, contexts).evaluateFlag(flagKey, 0);
  }

  private static Result failure(ErrorCode code) {
    return new Result(null, null, Reason.error(code));
  }

  private static Result served(List<Object> variations, int index, Reason reason) {
    return new Result(variations.get(index), index, reason);
  }

  private static Result offResult(Header header, Reason reason) {
    if (header.offVariation() == null) {
      return new Result(null, null, reason);
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

  private Result serveResult(Flag flag, Serve serve, Reason reason) {
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
    for (Map.Entry<String, KindContext> e : contexts.entrySet()) {
      Set<String> listed = keys.get(e.getKey());
      if (listed != null && listed.contains(e.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  private boolean segmentRuleMatches(SegmentRule rule, Segment segment) {
    for (AttributeClause clause : rule.clauses()) {
      if (!Clauses.matches(clause, contexts)) {
        return false;
      }
    }
    if (rule.weight() == null) {
      return true;
    }
    return Contexts.kindBucket(segment.key(), segment.salt(), rule.rolloutContextKind(), contexts)
        < rule.weight();
  }

  private boolean segmentMatches(Segment segment) {
    if (listsContext(segment.excluded(), contexts)) {
      return false;
    }
    if (listsContext(segment.included(), contexts)) {
      return true;
    }
    for (SegmentRule rule : segment.rules()) {
      if (segmentRuleMatches(rule, segment)) {
        return true;
      }
    }
    return false;
  }

  private boolean clauseMatches(Clause clause) {
    if (clause instanceof AttributeClause attribute) {
      return Clauses.matches(attribute, contexts);
    }
    SegmentClause segmentClause = (SegmentClause) clause;
    boolean matched = false;
    for (String key : segmentClause.keys()) {
      SegmentEntry entry = ruleset.segment(key);
      if (entry != null && entry.segment() != null && segmentMatches(entry.segment())) {
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

  private Result checkPrerequisites(Flag flag, int depth) {
    for (Prerequisite prerequisite : flag.prerequisites()) {
      Result result = evaluateFlag(prerequisite.flagKey(), depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(flag.header(), Reason.prerequisiteFailed(prerequisite.flagKey()));
      }
    }
    return null;
  }

  private Integer targetedVariation(Flag flag) {
    int[] first = null;
    for (Map.Entry<String, KindContext> e : contexts.entrySet()) {
      Map<String, int[]> byKey = flag.targets().get(e.getKey());
      int[] hit = byKey == null ? null : byKey.get(e.getValue().key());
      if (hit != null && (first == null || hit[0] < first[0])) {
        first = hit;
      }
    }
    return first == null ? null : first[1];
  }

  private Result evaluateEntry(Entry entry, int depth) {
    if (entry.status() == Ruleset.Status.MALFORMED) {
      return failure(ErrorCode.MALFORMED_FLAG);
    }
    if (!entry.header().on()) {
      return offResult(entry.header(), Reason.off());
    }
    if (entry.status() == Ruleset.Status.BROKEN) {
      return failure(entry.code());
    }
    Flag flag = entry.flag();
    Result blocked = checkPrerequisites(flag, depth);
    if (blocked != null) {
      return blocked;
    }
    Integer targeted = targetedVariation(flag);
    if (targeted != null) {
      return served(flag.header().variations(), targeted, Reason.targetMatch());
    }
    for (int i = 0; i < flag.rules().size(); i++) {
      Rule rule = flag.rules().get(i);
      boolean all = true;
      for (Clause clause : rule.clauses()) {
        if (!clauseMatches(clause)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(flag, rule.serve(), Reason.ruleMatch(i));
      }
    }
    return serveResult(flag, flag.fallthrough(), Reason.fallthrough());
  }

  private Result evaluateFlag(String key, int depth) {
    Result done = completed.get(key);
    if (done != null) {
      return done;
    }
    if (active.contains(key)) {
      return failure(ErrorCode.PREREQUISITE_CYCLE);
    }
    if (depth > MAX_PREREQUISITE_DEPTH) {
      return failure(ErrorCode.PREREQUISITE_DEPTH_EXCEEDED);
    }
    Entry entry = ruleset.flag(key);
    if (entry == null) {
      return failure(ErrorCode.FLAG_NOT_FOUND);
    }
    active.add(key);
    Result result = evaluateEntry(entry, depth);
    active.remove(key);
    completed.put(key, result);
    return result;
  }
}
