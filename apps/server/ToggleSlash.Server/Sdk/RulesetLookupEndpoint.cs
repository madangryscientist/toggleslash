using System.Text.Json;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Catalogue;
using ToggleSlash.Server.Data;
using ToggleSlash.Server.Targeting;
using Microsoft.EntityFrameworkCore;

namespace ToggleSlash.Server.Sdk;

/// <summary>
/// "What were the flags under this hash?" Anything holding the ruleset's <c>ETag</c> can ask, and this answers from the bytes
/// kept when that ruleset was first served. No SDK key reaches it and it has no CORS, because it carries rules and
/// targets. The kept bytes hold salts; these shapes have no field for one, so no answer can carry one. A read, so a
/// scoped token of the project, read or write, may use it.
/// </summary>
public static class RulesetLookupEndpoint
{
    /// <param name="Etag">The <c>ETag</c> this ruleset was served with, quotes included.</param>
    /// <param name="FirstServedAt">When <c>GET /sdk/v1/ruleset</c> first answered 200 with these bytes in this environment.</param>
    public sealed record RulesetLookupResponse(string Etag, DateTime FirstServedAt, RulesetLookupFlag[] Flags, RulesetLookupSegment[] Segments);

    public sealed record RulesetLookupFlag(
        string Key,
        JsonElement Variations,
        bool On,
        int? OffVariation,
        JsonElement Targets,
        JsonElement Prerequisites,
        JsonElement Rules,
        JsonElement Fallthrough,
        long Version);

    public sealed record RulesetLookupSegment(string Key, string[] Included, string[] Excluded, JsonElement Rules);

    public static void MapRulesetLookupEndpoint(this IEndpointRouteBuilder routes)
    {
        routes.MapGet("/api/v1/projects/{project}/environments/{env}/rulesets/{hash}", Lookup)
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Diagnostics")
            .WithName("getRulesetByHash")
            .WithSummary("A ruleset this environment served, by the lowercase hex of its ETag. `?flag=` narrows it to that flag and the segments its rules name.")
            .Produces<RulesetLookupResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    private static async Task<IResult> Lookup(
        string project, string env, string hash, HttpContext http, ToggleSlashDbContext database, CancellationToken cancellation, string? flag = null)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, env, cancellation) is not { } environment)
        {
            return Failures.EnvironmentNotFound();
        }
        var stored = await database.RulesetSnapshots.AsNoTracking()
            .SingleOrDefaultAsync(s => s.EnvironmentId == environment.Id && s.Hash == hash, cancellation);
        if (stored is null)
        {
            return Failures.RulesetNotFound();
        }

        var ruleset = RulesetSnapshot.Parse(stored.Body);
        var flags = ruleset.Flags;
        var segments = ruleset.Segments;
        if (flag is not null)
        {
            flags = [.. flags.Where(f => f.Key == flag)];
            if (flags.Length == 0)
            {
                return Failures.FlagNotInRuleset();
            }
            var named = SegmentEndpoints.ReferencedSegments(flags[0].Rules.GetRawText());
            segments = [.. segments.Where(s => named.Contains(s.Key))];
        }

        return Results.Json(new RulesetLookupResponse(
            $"\"{stored.Hash}\"",
            stored.FirstServedAt,
            [.. flags.Select(f => new RulesetLookupFlag(f.Key, f.Variations, f.On, f.OffVariation, f.Targets, f.Prerequisites, f.Rules, f.Fallthrough, f.Version))],
            [.. segments.Select(s => new RulesetLookupSegment(s.Key, s.Included, s.Excluded, s.Rules))]));
    }
}
