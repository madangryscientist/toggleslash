using System.Security.Cryptography;
using System.Text.Json;
using System.Text.Json.Serialization;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Targeting;

public static class SegmentEndpoints
{
    /// <summary>A segment of one environment: the context keys always in or out, and rules that bring in the rest.</summary>
    public sealed record SegmentResponse(string Key, string[] Included, string[] Excluded, JsonElement Rules);

    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record CreateSegmentRequest(string? Key, string[]? Included, string[]? Excluded, JsonElement? Rules);

    /// <summary>A PUT replaces the segment's members and rules; send all three (an empty array clears one).</summary>
    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record PutSegmentRequest(string[]? Included, string[]? Excluded, JsonElement? Rules);

    public static void MapSegmentEndpoints(this IEndpointRouteBuilder routes)
    {
        var segments = routes.MapGroup("/api/v1/projects/{project}/environments/{env}/segments")
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Segments");

        segments.MapPost("", Create)
            .WithName("createSegment")
            .Produces<SegmentResponse>(StatusCodes.Status201Created)
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
        segments.MapGet("", List)
            .WithName("listSegments")
            .Produces<SegmentResponse[]>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        segments.MapGet("{segment}", Read)
            .WithName("getSegment")
            .Produces<SegmentResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        segments.MapPut("{segment}", Put)
            .WithName("putSegment")
            .Produces<SegmentResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        segments.MapDelete("{segment}", Delete)
            .WithName("deleteSegment")
            .Produces(StatusCodes.Status204NoContent)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
    }

    private static async Task<IResult> Create(string project, string env, CreateSegmentRequest request, FeatureSlashDbContext database, TargetingLock targetingLock)
    {
        var problem = Validation.Key(request.Key, "key") ?? Shape(request.Included, request.Excluded, request.Rules);
        if (problem is not null)
        {
            return Failures.BadRequest(problem);
        }
        using var held = await targetingLock.Enter();
        var (scope, missing) = await Scope(database, project, env);
        if (missing is not null)
        {
            return missing;
        }
        if (await Find(database, scope!, request.Key!) is not null)
        {
            return Taken(request.Key!, env);
        }

        var segment = new Segment
        {
            ProjectId = scope!.Project.Id,
            EnvironmentId = scope.Environment.Id,
            Key = request.Key!,
            Salt = Convert.ToHexStringLower(RandomNumberGenerator.GetBytes(16)),
            Included = JsonSerializer.Serialize(request.Included),
            Excluded = JsonSerializer.Serialize(request.Excluded),
            Rules = JsonSerializer.Serialize(request.Rules!.Value),
        };
        database.Segments.Add(segment);
        try
        {
            await database.SaveChangesAsync();
        }
        catch (DbUpdateException exception) when (Failures.IsUniqueViolation(exception))
        {
            return Taken(request.Key!, env);
        }
        return Results.Created(
            $"/api/v1/projects/{scope.Project.Key}/environments/{scope.Environment.Key}/segments/{segment.Key}",
            Describe(segment));
    }

    private static async Task<IResult> List(string project, string env, FeatureSlashDbContext database)
    {
        var (scope, missing) = await Scope(database, project, env);
        if (missing is not null)
        {
            return missing;
        }
        var segments = await database.Segments.AsNoTracking()
            .Where(s => s.EnvironmentId == scope!.Environment.Id)
            .OrderBy(s => s.Key)
            .ToListAsync();
        return Results.Json(segments.Select(Describe).ToArray());
    }

    private static async Task<IResult> Read(string project, string env, string segment, FeatureSlashDbContext database)
    {
        var (scope, missing) = await Scope(database, project, env);
        if (missing is not null)
        {
            return missing;
        }
        return await Find(database, scope!, segment) is { } found ? Results.Json(Describe(found)) : NoSuchSegment();
    }

    private static async Task<IResult> Put(string project, string env, string segment, PutSegmentRequest request, FeatureSlashDbContext database, TargetingLock targetingLock)
    {
        if (Shape(request.Included, request.Excluded, request.Rules) is { } problem)
        {
            return Failures.BadRequest(problem);
        }
        using var held = await targetingLock.Enter();
        var (scope, missing) = await Scope(database, project, env);
        if (missing is not null)
        {
            return missing;
        }
        if (await Find(database, scope!, segment) is not { } found)
        {
            return NoSuchSegment();
        }

        var included = JsonSerializer.Serialize(request.Included);
        var excluded = JsonSerializer.Serialize(request.Excluded);
        var rules = JsonSerializer.Serialize(request.Rules!.Value);
        await database.Segments.Where(s => s.Id == found.Id).ExecuteUpdateAsync(set => set
            .SetProperty(s => s.Included, included)
            .SetProperty(s => s.Excluded, excluded)
            .SetProperty(s => s.Rules, rules));
        return Results.Json(Describe(await database.Segments.AsNoTracking().SingleAsync(s => s.Id == found.Id)));
    }

    /// <summary>
    /// A segment a rule still names cannot go: the flag would evaluate to <c>SEGMENT_NOT_FOUND</c>. The
    /// 409 says which flags, so the caller knows what to edit first.
    /// </summary>
    private static async Task<IResult> Delete(string project, string env, string segment, FeatureSlashDbContext database, TargetingLock targetingLock)
    {
        using var held = await targetingLock.Enter();
        var (scope, missing) = await Scope(database, project, env);
        if (missing is not null)
        {
            return missing;
        }
        if (await Find(database, scope!, segment) is not { } found)
        {
            return NoSuchSegment();
        }

        var configs = await database.FlagEnvironmentConfigs.AsNoTracking()
            .Where(c => c.EnvironmentId == scope!.Environment.Id)
            .Join(database.Flags, c => c.FlagId, f => f.Id, (c, f) => new { f.Key, c.Rules })
            .ToListAsync();
        var users = configs.Where(c => ReferencedSegments(c.Rules).Contains(found.Key)).Select(c => c.Key).Order(StringComparer.Ordinal).ToList();
        if (users.Count > 0)
        {
            return Failures.Conflict($"Segment \"{found.Key}\" is still used by the rules of: {string.Join(", ", users)}. Remove those rules first.");
        }

        await database.Segments.Where(s => s.Id == found.Id).ExecuteDeleteAsync();
        return Results.NoContent();
    }

    /// <summary>The segment keys named by <c>segmentMatch</c> clauses in a config's stored rules.</summary>
    internal static HashSet<string> ReferencedSegments(string rulesJson)
    {
        var keys = new HashSet<string>(StringComparer.Ordinal);
        using var document = JsonDocument.Parse(rulesJson);
        foreach (var rule in document.RootElement.EnumerateArray())
        {
            if (!rule.TryGetProperty("clauses", out var clauses) || clauses.ValueKind != JsonValueKind.Array)
            {
                continue;
            }
            foreach (var clause in clauses.EnumerateArray())
            {
                if (clause.TryGetProperty("op", out var op) && op.GetString() == "segmentMatch"
                    && clause.TryGetProperty("values", out var values) && values.ValueKind == JsonValueKind.Array)
                {
                    foreach (var value in values.EnumerateArray())
                    {
                        if (value.ValueKind == JsonValueKind.String)
                        {
                            keys.Add(value.GetString()!);
                        }
                    }
                }
            }
        }
        return keys;
    }

    private sealed record Located(Project Project, ProjectEnvironment Environment);

    private static async Task<(Located? Scope, IResult? Missing)> Scope(FeatureSlashDbContext database, string project, string env)
    {
        if (await database.Project(project) is not { } owner)
        {
            return (null, Failures.ProjectNotFound());
        }
        return await database.Environment(owner.Id, env) is { } environment
            ? (new Located(owner, environment), null)
            : (null, Failures.EnvironmentNotFound());
    }

    private static Task<Segment?> Find(FeatureSlashDbContext database, Located scope, string key) =>
        database.Segments.AsNoTracking().SingleOrDefaultAsync(s => s.EnvironmentId == scope.Environment.Id && s.Key == key);

    private static string? Shape(string[]? included, string[]? excluded, JsonElement? rules)
    {
        if (included is null || excluded is null || rules is not { ValueKind: JsonValueKind.Array })
        {
            return "included, excluded and rules are required arrays (empty is fine).";
        }
        return TargetingValidator.Guard(() => TargetingValidator.ContextKeys(included, "included")
            ?? TargetingValidator.ContextKeys(excluded, "excluded")
            ?? new TargetingValidator(0).SegmentRules(rules));
    }

    private static IResult NoSuchSegment() => ApiError.Result(StatusCodes.Status404NotFound, "not_found", "No such segment.");

    private static IResult Taken(string key, string env) => Failures.Conflict($"Segment \"{key}\" already exists in environment \"{env}\".");

    private static SegmentResponse Describe(Segment segment) => new(
        segment.Key,
        JsonSerializer.Deserialize<string[]>(segment.Included)!,
        JsonSerializer.Deserialize<string[]>(segment.Excluded)!,
        JsonSerializer.Deserialize<JsonElement>(segment.Rules));
}
