using System.Text.Json;

namespace ToggleSlash.Evaluation;

/// <summary>
/// Evaluates a flag for a context, to docs/evaluation.md. <see cref="Evaluate"/> never throws: every
/// failure, including a prerequisite cycle, is an <c>ERROR</c> result.
/// </summary>
public static class Evaluator
{
    /// <summary>Prerequisite links from the requested flag; docs/evaluation.md pins the limit.</summary>
    private const int MaxPrerequisiteDepth = 32;

    private sealed class Run(Ruleset ruleset, JsonElement context, string contextKey)
    {
        public Ruleset Ruleset { get; } = ruleset;
        public JsonElement Context { get; } = context;
        public string ContextKey { get; } = contextKey;
        public HashSet<string> Active { get; } = new(StringComparer.Ordinal);
        public Dictionary<string, EvaluationResult> Completed { get; } = new(StringComparer.Ordinal);
    }

    /// <summary>
    /// Evaluates one flag. <paramref name="context"/> is the context as JSON,
    /// <c>{ "key": "user-1", "attributes": { ... } }</c>; one that is not valid is <c>INVALID_CONTEXT</c>.
    /// </summary>
    public static EvaluationResult Evaluate(Ruleset ruleset, string flagKey, JsonElement context)
    {
        if (!TryContextKey(context, out var contextKey)) return EvaluationResult.Failure(ErrorCode.InvalidContext);
        return EvaluateFlag(new Run(ruleset, context, contextKey), flagKey, 0);
    }

    private static bool TryContextKey(JsonElement context, out string key)
    {
        key = "";
        if (context.ValueKind != JsonValueKind.Object) return false;
        if (!context.TryGetProperty("key", out var raw) || raw.ValueKind != JsonValueKind.String) return false;
        key = JsonStrings.Read(raw);
        if (key.Length == 0) return false;
        return !context.TryGetProperty("attributes", out var attributes)
            || attributes.ValueKind is JsonValueKind.Null or JsonValueKind.Object;
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

    private static EvaluationResult ServeResult(Flag flag, Serve serve, string contextKey, Reason reason)
    {
        var index = serve.Rollout is { } rollout
            ? RolloutVariation(rollout, Bucketing.Bucket(flag.Header.Key, flag.Salt, contextKey))
            : serve.Variation!.Value;
        return Served(flag.Header, index, reason);
    }

    private static bool SegmentRuleMatches(SegmentRule rule, Segment segment, Run run)
    {
        if (!rule.Clauses.All(clause => ClauseMatcher.AttributeClauseMatches(clause, run.Context))) return false;
        return rule.Weight is not { } weight || Bucketing.Bucket(segment.Key, segment.Salt, run.ContextKey) < weight;
    }

    private static bool SegmentMatches(Segment segment, Run run)
    {
        if (segment.Excluded.Contains(run.ContextKey)) return false;
        if (segment.Included.Contains(run.ContextKey)) return true;
        return segment.Rules.Any(rule => SegmentRuleMatches(rule, segment, run));
    }

    private static bool ClauseMatches(Clause clause, Run run)
    {
        if (clause.Op != Op.SegmentMatch) return ClauseMatcher.AttributeClauseMatches(clause, run.Context);
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
                if (flag.Targets.TryGetValue(run.ContextKey, out var targeted))
                {
                    return Served(flag.Header, targeted, new Reason(ReasonKind.TargetMatch));
                }
                for (var ruleIndex = 0; ruleIndex < flag.Rules.Count; ruleIndex++)
                {
                    var rule = flag.Rules[ruleIndex];
                    if (!rule.Clauses.All(clause => ClauseMatches(clause, run))) continue;
                    return ServeResult(flag, rule.Serve, run.ContextKey, new Reason(ReasonKind.RuleMatch, RuleIndex: ruleIndex));
                }
                return ServeResult(flag, flag.Fallthrough, run.ContextKey, new Reason(ReasonKind.Fallthrough));
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
