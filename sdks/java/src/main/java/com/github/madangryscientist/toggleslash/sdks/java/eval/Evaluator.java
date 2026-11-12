package com.github.madangryscientist.toggleslash.sdks.java.eval;

import com.github.madangryscientist.toggleslash.sdks.java.ErrorCode;
import com.github.madangryscientist.toggleslash.sdks.java.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.ReasonKind;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Clause;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Entry;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Flag;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Header;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Prerequisite;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Rollout;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Segment;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.SegmentEntry;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.SegmentRule;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.Serve;
import com.github.madangryscientist.toggleslash.sdks.java.eval.Model.TargetHit;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Evaluates a flag for a context, to {@code docs/evaluation.md}. */
public final class Evaluator {
  /** Prerequisite links from the requested flag; docs/evaluation.md pins the limit. */
  private static final int MAX_PREREQUISITE_DEPTH = 32;

  private final Ruleset ruleset;
  private final Contexts contexts;
  private final Set<String> active = new HashSet<>();
  private final Map<String, Evaluation> completed = new HashMap<>();

  private Evaluator(Ruleset ruleset, Contexts contexts) {
    this.ruleset = ruleset;
    this.contexts = contexts;
  }

  /**
   * Evaluates one flag for one context. Every failure, including a prerequisite cycle, is an {@code
   * ERROR} result; an unexpected exception is the caller's to catch.
   *
   * @param ruleset the ruleset
   * @param flagKey the flag
   * @param context a map, or a value of any type (which is then not a valid context)
   * @return the result
   */
  public static Evaluation evaluate(Ruleset ruleset, String flagKey, Object context) {
    Contexts contexts = Contexts.normalise(context);
    if (contexts == null) {
      return failure(ErrorCode.INVALID_CONTEXT);
    }
    return new Evaluator(ruleset, contexts).evaluateFlag(flagKey, 0);
  }

  private static Evaluation failure(ErrorCode code) {
    return new Evaluation(null, null, Reason.error(code));
  }

  private static Evaluation served(List<Object> variations, int index, Reason reason) {
    return new Evaluation(variations.get(index), index, reason);
  }

  private static Evaluation offResult(Header header, Reason reason) {
    Integer index = header.offVariation();
    if (index == null) {
      return new Evaluation(null, null, reason);
    }
    return served(header.variations(), index, reason);
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

  private Evaluation serveResult(Flag flag, Serve serve, Reason reason) {
    int index =
        serve.rollout() != null
            ? rolloutVariation(
                serve.rollout(),
                contexts.kindBucket(
                    flag.header().key(), flag.salt(), serve.rollout().contextKind()))
            : serve.variation();
    return served(flag.header().variations(), index, reason);
  }

  private boolean listsContext(Map<String, Set<String>> keys) {
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.entries()) {
      Set<String> set = keys.get(entry.getKey());
      if (set != null && set.contains(entry.getValue().key())) {
        return true;
      }
    }
    return false;
  }

  private boolean segmentRuleMatches(SegmentRule rule, Segment segment) {
    for (Clause clause : rule.clauses()) {
      if (!Clauses.matches(clause, contexts)) {
        return false;
      }
    }
    if (rule.weight() == null) {
      return true;
    }
    return contexts.kindBucket(segment.key(), segment.salt(), rule.rolloutContextKind())
        < rule.weight();
  }

  private boolean segmentMatches(Segment segment) {
    if (listsContext(segment.excluded())) {
      return false;
    }
    if (listsContext(segment.included())) {
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
    if (clause.op() != Model.Op.SEGMENT_MATCH) {
      return Clauses.matches(clause, contexts);
    }
    boolean matched = false;
    for (Object key : clause.values()) {
      SegmentEntry entry = ruleset.segment((String) key);
      if (entry != null && entry.segment() != null && segmentMatches(entry.segment())) {
        matched = true;
        break;
      }
    }
    return clause.negate() ? !matched : matched;
  }

  private static boolean prerequisiteMet(Prerequisite prerequisite, Evaluation result) {
    ReasonKind kind = result.reason().kind();
    boolean reachedServe =
        kind == ReasonKind.TARGET_MATCH
            || kind == ReasonKind.RULE_MATCH
            || kind == ReasonKind.FALLTHROUGH;
    return reachedServe
        && result.variationIndex() != null
        && result.variationIndex() == prerequisite.variation();
  }

  private Evaluation checkPrerequisites(Flag flag, int depth) {
    for (Prerequisite prerequisite : flag.prerequisites()) {
      Evaluation result = evaluateFlag(prerequisite.flagKey(), depth + 1);
      if (result.reason().kind() == ReasonKind.ERROR) {
        return result;
      }
      if (!prerequisiteMet(prerequisite, result)) {
        return offResult(
            flag.header(),
            new Reason(ReasonKind.PREREQUISITE_FAILED, null, prerequisite.flagKey(), null));
      }
    }
    return null;
  }

  private Integer targetedVariation(Map<String, Map<String, TargetHit>> targets) {
    TargetHit first = null;
    for (Map.Entry<String, Contexts.KindContext> entry : contexts.entries()) {
      Map<String, TargetHit> byKey = targets.get(entry.getKey());
      TargetHit hit = byKey == null ? null : byKey.get(entry.getValue().key());
      if (hit != null && (first == null || hit.order() < first.order())) {
        first = hit;
      }
    }
    return first == null ? null : first.variation();
  }

  private Evaluation evaluateEntry(Entry entry, int depth) {
    if (entry.status() == Model.MALFORMED) {
      return failure(ErrorCode.MALFORMED_FLAG);
    }
    if (!entry.header().on()) {
      return offResult(entry.header(), Reason.of(ReasonKind.OFF));
    }
    if (entry.status() == Model.BROKEN) {
      return failure(entry.code());
    }
    Flag flag = entry.flag();
    Evaluation blocked = checkPrerequisites(flag, depth);
    if (blocked != null) {
      return blocked;
    }
    Integer targeted = targetedVariation(entry.targets());
    if (targeted != null) {
      return served(flag.header().variations(), targeted, Reason.of(ReasonKind.TARGET_MATCH));
    }
    for (int i = 0; i < flag.rules().size(); i++) {
      Model.Rule rule = flag.rules().get(i);
      boolean all = true;
      for (Clause clause : rule.clauses()) {
        if (!clauseMatches(clause)) {
          all = false;
          break;
        }
      }
      if (all) {
        return serveResult(flag, rule.serve(), new Reason(ReasonKind.RULE_MATCH, i, null, null));
      }
    }
    return serveResult(flag, flag.fallthrough(), Reason.of(ReasonKind.FALLTHROUGH));
  }

  private Evaluation evaluateFlag(String key, int depth) {
    Evaluation done = completed.get(key);
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
    Evaluation result;
    try {
      result = evaluateEntry(entry, depth);
    } finally {
      active.remove(key);
    }
    completed.put(key, result);
    return result;
  }
}
