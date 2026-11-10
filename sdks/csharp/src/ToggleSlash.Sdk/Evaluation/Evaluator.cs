using System.Text.Json;

namespace ToggleSlash.Sdk.Evaluation;

/// <summary>
/// Evaluates a flag for a context, to docs/evaluation.md. <see cref="Evaluate"/> never throws: every
/// failure, including a prerequisite cycle, is an <c>ERROR</c> result.
/// </summary>
internal static class Evaluator
{
    /// <summary>Prerequisite links from the requested flag; docs/evaluation.md pins the limit.</summary>
    private const int MaxPrerequisiteDepth = 32;

    private sealed class Run(Ruleset ruleset, IReadOnlyDictionary<string, KindContext> contexts)
    {
        public Ruleset Ruleset { get; } = ruleset;
        public IReadOnlyDictionary<string, KindContext> Contexts { get; } = contexts;
        public HashSet<string> Active { get; } = new(StringComparer.Ordinal);
        public Dictionary<string, EvaluationResult> Completed { get; } = new(StringComparer.Ordinal);
    }

    /// <summary>
    /// Evaluates one flag. <paramref name="context"/> is the context as JSON, single
    /// (<c>{ "kind": "org", "key": "acme", "attributes": { ... } }</c>, no <c>kind</c> is <c>user</c>) or multi
    /// (<c>{ "kind": "multi", "contexts": { "user": { "key": ... }, ... } }</c>); one that is not valid is
    /// <c>INVALID_CONTEXT</c>.
    /// </summary>
    public static EvaluationResult Evaluate(Ruleset ruleset, string flagKey, JsonElement context)
    {
        if (ContextKinds.Normalise(context) is not { } contexts) return EvaluationResult.Failure(ErrorCode.InvalidContext);
        return EvaluateFlag(new Run(ruleset, contexts), flagKey, 0);
    }

    private static EvaluationResult Served(FlagHeader header, int index, Reason reason) =>
        new(header.Variations[index], index, reason);

    private static EvaluationResult OffResult(FlagHeader header, Reason reason) =>
        header.OffVariation is { } index
            ? Served(header, index, reason)
            : new EvaluationResult(EvaluationResult.JsonNull, null, reason);

    private static int RolloutVariation(IReadOnlyList<WeightedVariation> rollout, int position)
    {
        var chosen = 0;
        double cumulative = 0;
        foreach (var entry in rollout)
        {
            chosen = entry.Variation;
            cumulative += entry.Weight;
            if (position < cumulative) break;
        }
        return chosen;
    }

    private static EvaluationResult ServeResult(Flag flag, Serve serve, Run run, Reason reason)
    {
        var index = serve.Rollout is { } rollout
            ? RolloutVariation(rollout, ContextKinds.Bucket(flag.Header.Key, flag.Salt, serve.RolloutKind, run.Contexts))
            : serve.Variation!.Value;
        return Served(flag.Header, index, reason);
    }

    private static bool SegmentRuleMatches(SegmentRule rule, Segment segment, Run run)
    {
        if (!rule.Clauses.All(clause => ClauseMatcher.AttributeClauseMatches(clause, run.Contexts))) return false;
        return rule.Weight is not { } weight || ContextKinds.Bucket(segment.Key, segment.Salt, rule.RolloutKind, run.Contexts) < weight;
    }

    /// <summary>Does the context have, in any of its kinds, a key that <paramref name="keys"/> lists for that kind?</summary>
    private static bool Lists(IReadOnlyDictionary<string, HashSet<string>> keys, Run run) =>
        run.Contexts.Any(entry => keys.TryGetValue(entry.Key, out var listed) && listed.Contains(entry.Value.Key));

    private static bool SegmentMatches(Segment segment, Run run)
    {
        if (Lists(segment.Excluded, run)) return false;
        if (Lists(segment.Included, run)) return true;
        return segment.Rules.Any(rule => SegmentRuleMatches(rule, segment, run));
    }

    /// <summary>The variation of the first target in array order that matches the context, or null.</summary>
    private static int? TargetedVariation(Flag flag, Run run)
    {
        TargetHit? first = null;
        foreach (var (kind, context) in run.Contexts)
        {
            if (!flag.Targets.TryGetValue(kind, out var keys) || !keys.TryGetValue(context.Key, out var hit)) continue;
            if (first is not { } current || hit.Order < current.Order) first = hit;
        }
        return first?.Variation;
    }

    private static bool ClauseMatches(Clause clause, Run run)
    {
        if (clause.Op != Op.SegmentMatch) return ClauseMatcher.AttributeClauseMatches(clause, run.Contexts);
        var matched = clause.SegmentKeys.Any(key =>
            run.Ruleset.TryGetSegment(key, out var segment) && segment is not null && SegmentMatches(segment, run));
        return clause.Negate ? !matched : matched;
    }

    private static bool PrerequisiteMet(Prerequisite prerequisite, EvaluationResult result) =>
        result.Reason.Kind is ReasonKind.TargetMatch or ReasonKind.RuleMatch or ReasonKind.Fallthrough
        && result.VariationIndex is { } served
        && served == prerequisite.Variation;

    private static EvaluationResult? CheckPrerequisites(Flag flag, Run run, int depth)
    {
        foreach (var prerequisite in flag.Prerequisites)
        {
            var result = EvaluateFlag(run, prerequisite.FlagKey, depth + 1);
            if (result.Reason.Kind == ReasonKind.Error) return result;
            if (!PrerequisiteMet(prerequisite, result))
            {
                return OffResult(flag.Header, new Reason(ReasonKind.PrerequisiteFailed, FlagKey: prerequisite.FlagKey));
            }
        }
        return null;
    }

    private static EvaluationResult EvaluateEntry(FlagEntry entry, Run run, int depth)
    {
        switch (entry)
        {
            case MalformedEntry:
                return EvaluationResult.Failure(ErrorCode.MalformedFlag);
            case BrokenEntry broken:
                return broken.Header.On
                    ? EvaluationResult.Failure(broken.Code)
                    : OffResult(broken.Header, new Reason(ReasonKind.Off));
            case ReadyEntry { Flag: var flag }:
                if (!flag.Header.On) return OffResult(flag.Header, new Reason(ReasonKind.Off));
                if (CheckPrerequisites(flag, run, depth) is { } blocked) return blocked;
                if (TargetedVariation(flag, run) is { } targeted)
                {
                    return Served(flag.Header, targeted, new Reason(ReasonKind.TargetMatch));
                }
                for (var ruleIndex = 0; ruleIndex < flag.Rules.Count; ruleIndex++)
                {
                    var rule = flag.Rules[ruleIndex];
                    if (!rule.Clauses.All(clause => ClauseMatches(clause, run))) continue;
                    return ServeResult(flag, rule.Serve, run, new Reason(ReasonKind.RuleMatch, RuleIndex: ruleIndex));
                }
                return ServeResult(flag, flag.Fallthrough, run, new Reason(ReasonKind.Fallthrough));
            default:
                throw new InvalidOperationException("unreachable: FlagEntry is closed");
        }
    }

    /// <summary>Entering a flag checks, in this order: finished, in progress, too deep, missing.</summary>
    private static EvaluationResult EvaluateFlag(Run run, string key, int depth)
    {
        if (run.Completed.TryGetValue(key, out var completed)) return completed;
        if (run.Active.Contains(key)) return EvaluationResult.Failure(ErrorCode.PrerequisiteCycle);
        if (depth > MaxPrerequisiteDepth) return EvaluationResult.Failure(ErrorCode.PrerequisiteDepthExceeded);
        if (!run.Ruleset.TryGetFlag(key, out var entry)) return EvaluationResult.Failure(ErrorCode.FlagNotFound);
        run.Active.Add(key);
        var result = EvaluateEntry(entry, run, depth);
        run.Active.Remove(key);
        run.Completed[key] = result;
        return result;
    }
}
