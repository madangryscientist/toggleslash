using System.Security.Cryptography;
using System.Text.Json;
using System.Text.Json.Serialization;
using ToggleSlash.Server.Audit;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Catalogue;
using ToggleSlash.Server.Data;
using ToggleSlash.Server.Sdk;
using Microsoft.AspNetCore.Mvc;
using Microsoft.EntityFrameworkCore;

namespace ToggleSlash.Server.Targeting;

public static class SegmentEndpoints
{
    /// <summary>
    /// A segment of one environment: the context keys always in or out, and rules that bring in the rest.
    /// <c>version</c> starts at 1 and goes up by one on every save, and is also the <c>ETag</c> of a single segment (<c>"3"</c>).
    /// </summary>
    public sealed record SegmentResponse(string Key, string[] Included, string[] Excluded, JsonElement Rules, long Version);

    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record CreateSegmentRequest(string? Key, string[]? Included, string[]? Excluded, JsonElement? Rules);

    /// <summary>
    /// A PUT replaces the segment's members and rules; send all three (an empty array clears one).
    /// The version being replaced goes in <c>version</c> or in <c>If-Match</c>; one is required.
    /// </summary>
    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record PutSegmentRequest(string[]? Included, string[]? Excluded, JsonElement? Rules, long? Version = null);

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
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
        segments.MapDelete("{segment}", Delete)
            .WithName("deleteSegment")
            .Produces(StatusCodes.Status204NoContent)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
    }

    private static async Task<IResult> Create(string project, string env, CreateSegmentRequest request, ToggleSlashDbContext database, TargetingLock targetingLock, RulesetNotifier notifier, CancellationToken cancellation)
    {
        var problem = Validation.Key(request.Key, "key") ?? Shape(request.Included, request.Excluded, request.Rules);
        if (problem is not null)
        {
            return Failures.BadRequest(problem);
        }
        using var held = await targetingLock.Enter(cancellation);
        var (scope, missing) = await Scope(database, project, env, cancellation);
        if (missing is not null)
        {
            return missing;
        }
        if (await Find(database, scope!, request.Key!, cancellation) is not null)
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
            Version = 1,
        };
        database.Segments.Add(segment);
        database.Record(
            AuditEntity.Segment, segment.Key, AuditAction.Created, new AuditScope(scope.Project.Key, scope.Environment.Key), null, Describe(segment));
        try
        {
            await database.SaveChangesAsync(cancellation);
        }
        catch (DbUpdateException exception) when (Failures.IsUniqueViolation(exception))
        {
            return Taken(request.Key!, env);
        }
        notifier.Changed(scope.Environment.Id);
        return Results.Created(
            $"/api/v1/projects/{scope.Project.Key}/environments/{scope.Environment.Key}/segments/{segment.Key}",
            Describe(segment));
    }

    private static async Task<IResult> List(string project, string env, ToggleSlashDbContext database, CancellationToken cancellation)
    {
        var (scope, missing) = await Scope(database, project, env, cancellation);
        if (missing is not null)
        {
            return missing;
        }
        var segments = await database.Segments.AsNoTracking()
            .Where(s => s.EnvironmentId == scope!.Environment.Id)
            .OrderBy(s => s.Key)
            .ToListAsync(cancellation);
        return Results.Json(segments.Select(Describe).ToArray());
    }

    private static async Task<IResult> Read(string project, string env, string segment, HttpContext http, ToggleSlashDbContext database, CancellationToken cancellation)
    {
        var (scope, missing) = await Scope(database, project, env, cancellation);
        if (missing is not null)
        {
            return missing;
        }
        return await Find(database, scope!, segment, cancellation) is { } found ? Respond(http, Describe(found)) : NoSuchSegment();
    }

    private static async Task<IResult> Put(
        string project,
        string env,
        string segment,
        PutSegmentRequest request,
        [FromHeader(Name = "If-Match")] string? ifMatch,
        HttpContext http,
        ToggleSlashDbContext database,
        TargetingLock targetingLock,
        RulesetNotifier notifier, CancellationToken cancellation)
    {
        if (Shape(request.Included, request.Excluded, request.Rules) is { } problem)
        {
            return Failures.BadRequest(problem);
        }
        using var held = await targetingLock.Enter(cancellation);
        var (scope, missing) = await Scope(database, project, env, cancellation);
        if (missing is not null)
        {
            return missing;
        }
        if (await Find(database, scope!, segment, cancellation) is not { } found)
        {
            return NoSuchSegment();
        }
        var (expected, versionProblem) = ConfigEndpoints.ExpectedVersion(ifMatch, request.Version);
        if (versionProblem is not null)
        {
            return Failures.BadRequest(versionProblem);
        }
        if (expected is null)
        {
            return Failures.BadRequest("A PUT needs the version it replaces: send it in the If-Match header or a \"version\" field.");
        }

        var included = JsonSerializer.Serialize(request.Included);
        var excluded = JsonSerializer.Serialize(request.Excluded);
        var rules = JsonSerializer.Serialize(request.Rules!.Value);
        // The compare-and-set and its event commit together. The targeting lock keeps `found` the row's true
        // "before" whenever the compare-and-set succeeds; a stale version changed no row, so it records nothing.
        await using var transaction = await database.Database.BeginTransactionAsync(cancellation);
        var rows = await database.Segments
            .Where(s => s.Id == found.Id && s.Version == expected)
            .ExecuteUpdateAsync(set => set
                .SetProperty(s => s.Included, included)
                .SetProperty(s => s.Excluded, excluded)
                .SetProperty(s => s.Rules, rules)
                .SetProperty(s => s.Version, s => s.Version + 1), cancellation);
        var saved = Describe(await database.Segments.AsNoTracking().SingleAsync(s => s.Id == found.Id, cancellation));
        if (rows == 0)
        {
            return Failures.VersionConflict($"The segment is at version {saved.Version}, not {expected}: it changed since it was read. GET it again and retry.");
        }
        database.Record(
            AuditEntity.Segment, found.Key, AuditAction.Updated, new AuditScope(scope!.Project.Key, scope.Environment.Key), Describe(found), saved);
        await database.SaveChangesAsync(cancellation);
        await transaction.CommitAsync(cancellation);
        notifier.Changed(scope.Environment.Id);
        return Respond(http, saved);
    }

    /// <summary>
    /// A segment a rule still names cannot go: the flag would evaluate to <c>SEGMENT_NOT_FOUND</c>. The
    /// 409 says which flags, so the caller knows what to edit first.
    /// </summary>
    private static async Task<IResult> Delete(string project, string env, string segment, ToggleSlashDbContext database, TargetingLock targetingLock, RulesetNotifier notifier, CancellationToken cancellation)
    {
        using var held = await targetingLock.Enter(cancellation);
        var (scope, missing) = await Scope(database, project, env, cancellation);
        if (missing is not null)
        {
            return missing;
        }
        if (await Find(database, scope!, segment, cancellation) is not { } found)
        {
            return NoSuchSegment();
        }

        var configs = await database.FlagEnvironmentConfigs.AsNoTracking()
            .Where(c => c.EnvironmentId == scope!.Environment.Id)
            .Join(database.Flags, c => c.FlagId, f => f.Id, (c, f) => new { f.Key, c.Rules })
            .ToListAsync(cancellation);
        var users = configs.Where(c => ReferencedSegments(c.Rules).Contains(found.Key)).Select(c => c.Key).Order(StringComparer.Ordinal).ToList();
        if (users.Count > 0)
        {
            return Failures.Conflict($"Segment \"{found.Key}\" is still used by the rules of: {string.Join(", ", users)}. Remove those rules first.");
        }

        await using var transaction = await database.Database.BeginTransactionAsync(cancellation);
        await database.Segments.Where(s => s.Id == found.Id).ExecuteDeleteAsync(cancellation);
        database.Record(
            AuditEntity.Segment, found.Key, AuditAction.Deleted, new AuditScope(scope!.Project.Key, scope.Environment.Key), Describe(found), null);
        await database.SaveChangesAsync(cancellation);
        await transaction.CommitAsync(cancellation);
        notifier.Changed(scope.Environment.Id);
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

    private static async Task<(Located? Scope, IResult? Missing)> Scope(ToggleSlashDbContext database, string project, string env, CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return (null, Failures.ProjectNotFound());
        }
        return await database.Environment(owner.Id, env, cancellation) is { } environment
            ? (new Located(owner, environment), null)
            : (null, Failures.EnvironmentNotFound());
    }

    private static Task<Segment?> Find(ToggleSlashDbContext database, Located scope, string key, CancellationToken cancellation) =>
        database.Segments.AsNoTracking().SingleOrDefaultAsync(s => s.EnvironmentId == scope.Environment.Id && s.Key == key, cancellation);

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

    private static IResult Respond(HttpContext http, SegmentResponse segment)
    {
        http.Response.Headers.ETag = $"\"{segment.Version}\"";
        return Results.Json(segment);
    }

    private static SegmentResponse Describe(Segment segment) => new(
        segment.Key,
        JsonSerializer.Deserialize<string[]>(segment.Included)!,
        JsonSerializer.Deserialize<string[]>(segment.Excluded)!,
        JsonSerializer.Deserialize<JsonElement>(segment.Rules),
        segment.Version);
}
