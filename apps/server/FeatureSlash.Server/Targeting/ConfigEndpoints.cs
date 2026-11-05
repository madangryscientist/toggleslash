using System.Globalization;
using System.Text.Json;
using System.Text.Json.Serialization;
using FeatureSlash.Server.Audit;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Data;
using FeatureSlash.Server.Sdk;
using Microsoft.AspNetCore.Mvc;
using Microsoft.EntityFrameworkCore;
using Microsoft.EntityFrameworkCore.Storage;

namespace FeatureSlash.Server.Targeting;

public static class ConfigEndpoints
{
    /// <summary>
    /// A flag's settings in one environment. <c>version</c> starts at 1 and goes up by one on every save,
    /// and is also the <c>ETag</c> (<c>"3"</c>).
    /// </summary>
    public sealed record ConfigResponse(
        bool On,
        int? OffVariation,
        JsonElement Targets,
        JsonElement Prerequisites,
        JsonElement Rules,
        JsonElement Fallthrough,
        long Version);

    /// <summary>
    /// The whole config: a PUT replaces it, so leave out <c>offVariation</c> to serve <c>null</c> when off.
    /// <c>on</c>, <c>targets</c>, <c>prerequisites</c>, <c>rules</c> and <c>fallthrough</c> are required (the arrays may be empty).
    /// The version being replaced goes in <c>version</c> or in <c>If-Match</c>; one is required.
    /// </summary>
    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record PutConfigRequest(
        bool? On,
        int? OffVariation,
        JsonElement? Targets,
        JsonElement? Prerequisites,
        JsonElement? Rules,
        JsonElement? Fallthrough,
        long? Version = null);

    /// <summary>Only <c>on</c> changes. <c>version</c> (or <c>If-Match</c>) is optional here; given, it must be current.</summary>
    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record ToggleRequest(bool? On, long? Version = null);

    public static void MapConfigEndpoints(this IEndpointRouteBuilder routes)
    {
        var configs = routes.MapGroup("/api/v1/projects/{project}/flags/{flag}/environments/{env}")
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Flag targeting");

        configs.MapGet("", Read)
            .WithName("getFlagConfig")
            .Produces<ConfigResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        configs.MapPut("", Put)
            .WithName("putFlagConfig")
            .Produces<ConfigResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
        configs.MapPost("toggle", Toggle)
            .WithName("toggleFlag")
            .Produces<ConfigResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
    }

    private static async Task<IResult> Read(string project, string flag, string env, HttpContext http, FeatureSlashDbContext database)
    {
        var found = await Find(database, project, flag, env);
        if (found.Problem is not null)
        {
            return found.Problem;
        }
        return Respond(http, found.Config!);
    }

    private static async Task<IResult> Put(
        string project,
        string flag,
        string env,
        PutConfigRequest request,
        [FromHeader(Name = "If-Match")] string? ifMatch,
        HttpContext http,
        FeatureSlashDbContext database,
        TargetingLock targetingLock,
        RulesetNotifier notifier)
    {
        using var held = await targetingLock.Enter();
        var found = await Find(database, project, flag, env);
        if (found.Problem is not null)
        {
            return found.Problem;
        }
        var (expected, versionProblem) = ExpectedVersion(ifMatch, request.Version);
        if (versionProblem is not null)
        {
            return Failures.BadRequest(versionProblem);
        }
        if (expected is null)
        {
            return Failures.BadRequest("A PUT needs the version it replaces: send it in the If-Match header or a \"version\" field.");
        }

        var variationCount = JsonSerializer.Deserialize<List<JsonElement>>(found.Flag!.Variations)!.Count;
        var validator = new TargetingValidator(variationCount);
        var problem = TargetingValidator.Guard(() => RequiredShape(request)
            ?? OffVariationProblem(request.OffVariation, variationCount)
            ?? validator.Targets(request.Targets)
            ?? validator.Prerequisite(request.Prerequisites)
            ?? validator.Rules(request.Rules)
            ?? validator.Fallthrough(request.Fallthrough))
            ?? await ReferenceProblem(database, found, validator);
        if (problem is not null)
        {
            return Failures.BadRequest(problem);
        }

        var config = found.Config!;
        var targets = Serialize(request.Targets);
        var prerequisites = Serialize(request.Prerequisites);
        var rules = Serialize(request.Rules);
        var fallthrough = JsonSerializer.Serialize(request.Fallthrough!.Value);
        // The compare-and-set and its event commit together. The targeting lock keeps `config` (read in Find)
        // the row's true "before" whenever the compare-and-set succeeds.
        await using var transaction = await database.Database.BeginTransactionAsync();
        var saved = await database.FlagEnvironmentConfigs
            .Where(c => c.Id == config.Id && c.Version == expected)
            .ExecuteUpdateAsync(set => set
                .SetProperty(c => c.On, request.On!.Value)
                .SetProperty(c => c.OffVariation, request.OffVariation)
                .SetProperty(c => c.Targets, targets)
                .SetProperty(c => c.Prerequisites, prerequisites)
                .SetProperty(c => c.Rules, rules)
                .SetProperty(c => c.Fallthrough, fallthrough)
                .SetProperty(c => c.Version, c => c.Version + 1));
        return await Saved(database, http, found, saved, expected.Value, AuditAction.Updated, transaction, notifier);
    }

    /// <summary>
    /// Flips <c>on</c> and nothing else, in one statement. Without a version it applies to whatever the
    /// config is now, which is right for a switch: no other field is read or written, so it cannot undo
    /// someone else's edit. It still bumps <c>version</c>, so a PUT that was read before it is stale.
    /// </summary>
    private static async Task<IResult> Toggle(
        string project,
        string flag,
        string env,
        ToggleRequest request,
        [FromHeader(Name = "If-Match")] string? ifMatch,
        HttpContext http,
        FeatureSlashDbContext database,
        TargetingLock targetingLock,
        RulesetNotifier notifier)
    {
        if (request.On is not { } on)
        {
            return Failures.BadRequest("on is required, true or false.");
        }
        var (expected, versionProblem) = ExpectedVersion(ifMatch, request.Version);
        if (versionProblem is not null)
        {
            return Failures.BadRequest(versionProblem);
        }
        using var held = await targetingLock.Enter();
        var found = await Find(database, project, flag, env);
        if (found.Problem is not null)
        {
            return found.Problem;
        }

        var config = found.Config!;
        await using var transaction = await database.Database.BeginTransactionAsync();
        var saved = await database.FlagEnvironmentConfigs
            .Where(c => c.Id == config.Id && (expected == null || c.Version == expected))
            .ExecuteUpdateAsync(set => set
                .SetProperty(c => c.On, on)
                .SetProperty(c => c.Version, c => c.Version + 1));
        return await Saved(database, http, found, saved, expected ?? 0, AuditAction.Toggled, transaction, notifier);
    }

    /// <summary>
    /// A stale version changed no row, so it records nothing and the transaction is dropped. Otherwise the
    /// event is added and committed with the update.
    /// </summary>
    private static async Task<IResult> Saved(
        FeatureSlashDbContext database,
        HttpContext http,
        Found found,
        int rows,
        long expected,
        string action,
        IDbContextTransaction transaction,
        RulesetNotifier notifier)
    {
        var current = await database.FlagEnvironmentConfigs.AsNoTracking().SingleAsync(c => c.Id == found.Config!.Id);
        if (rows == 0)
        {
            return Failures.Conflict($"The config is at version {current.Version}, not {expected}: it changed since it was read. GET it again and retry.");
        }

        database.Record(
            AuditEntity.FlagConfig,
            $"{found.Flag!.Key}/{found.Environment!.Key}",
            action,
            new AuditScope(found.Project!.Key, found.Environment.Key, found.Flag.Key),
            Describe(found.Config!),
            Describe(current));
        await database.SaveChangesAsync();
        await transaction.CommitAsync();
        notifier.Changed(found.Environment.Id);
        return Respond(http, current);
    }

    private sealed record Found(IResult? Problem, Project? Project, ProjectEnvironment? Environment, Flag? Flag, FlagEnvironmentConfig? Config);

    private static async Task<Found> Find(FeatureSlashDbContext database, string project, string flag, string env)
    {
        if (await database.Project(project) is not { } owner)
        {
            return new Found(Failures.ProjectNotFound(), null, null, null, null);
        }
        if (await database.Environment(owner.Id, env) is not { } environment)
        {
            return new Found(Failures.EnvironmentNotFound(), null, null, null, null);
        }
        if (await database.Flags.AsNoTracking().SingleOrDefaultAsync(f => f.ProjectId == owner.Id && f.Key == flag) is not { } found)
        {
            return new Found(Failures.FlagNotFound(), null, null, null, null);
        }
        var config = await database.FlagEnvironmentConfigs.AsNoTracking()
            .SingleOrDefaultAsync(c => c.FlagId == found.Id && c.EnvironmentId == environment.Id);
        return config is null
            ? new Found(ApiError.Result(StatusCodes.Status404NotFound, "not_found", "No config for this flag in this environment."), null, null, null, null)
            : new Found(null, owner, environment, found, config);
    }

    /// <summary>The version from <c>If-Match</c> (<c>"3"</c>, <c>W/"3"</c> or <c>3</c>) and/or the body; both must agree.</summary>
    internal static (long? Version, string? Problem) ExpectedVersion(string? ifMatch, long? body)
    {
        long? header = null;
        if (ifMatch is not null)
        {
            var text = ifMatch.Trim();
            text = text.StartsWith("W/", StringComparison.Ordinal) ? text[2..] : text;
            if (!long.TryParse(text.Trim('"'), NumberStyles.None, CultureInfo.InvariantCulture, out var parsed))
            {
                return (null, "If-Match must be the version being replaced, such as \"3\".");
            }
            header = parsed;
        }
        if (body is < 0)
        {
            return (null, "version must be 0 or more.");
        }
        return header is not null && body is not null && header != body
            ? (null, "If-Match and the version field name different versions.")
            : (header ?? body, null);
    }

    private static string? RequiredShape(PutConfigRequest request)
    {
        static bool Missing(JsonElement? value) => value is null or { ValueKind: JsonValueKind.Null or JsonValueKind.Undefined };
        if (request.On is null)
        {
            return "on is required, true or false.";
        }
        foreach (var (name, value) in new[]
        {
            ("targets", request.Targets), ("prerequisites", request.Prerequisites), ("rules", request.Rules), ("fallthrough", request.Fallthrough),
        })
        {
            if (Missing(value))
            {
                return $"{name} is required: a PUT replaces the whole config (an empty array clears {name}).";
            }
        }
        return null;
    }

    private static string? OffVariationProblem(int? offVariation, int variationCount) =>
        offVariation is { } index && (index < 0 || index >= variationCount)
            ? $"offVariation must be an index into the flag's variations (0 to {variationCount - 1})."
            : null;

    /// <summary>
    /// What the config points at has to exist where the config lives: a prerequisite is a flag of this
    /// project, a segment is one of this environment, and the prerequisites cannot lead back to this flag.
    /// Storage does not stop a row pointing across projects or environments, so this does.
    /// </summary>
    private static async Task<string?> ReferenceProblem(FeatureSlashDbContext database, Found found, TargetingValidator validator)
    {
        var project = found.Project!;
        var environment = found.Environment!;
        var flag = found.Flag!;

        var flagKeys = validator.Prerequisites.Select(p => p.FlagKey).Distinct().ToList();
        var required = await database.Flags.AsNoTracking()
            .Where(f => f.ProjectId == project.Id && flagKeys.Contains(f.Key))
            .ToDictionaryAsync(f => f.Key);
        foreach (var prerequisite in validator.Prerequisites)
        {
            if (prerequisite.FlagKey == flag.Key)
            {
                return $"{prerequisite.Path}.flagKey: a flag cannot be its own prerequisite.";
            }
            if (!required.TryGetValue(prerequisite.FlagKey, out var target))
            {
                return $"{prerequisite.Path}.flagKey: there is no flag \"{prerequisite.FlagKey}\" in project \"{project.Key}\".";
            }
            var targetCount = JsonSerializer.Deserialize<List<JsonElement>>(target.Variations)!.Count;
            if (prerequisite.Variation >= targetCount)
            {
                return $"{prerequisite.Path}.variation must be an index into \"{target.Key}\"'s variations (0 to {targetCount - 1}).";
            }
        }

        var segmentKeys = validator.Segments.Select(s => s.Key).Distinct().ToList();
        var existing = await database.Segments.AsNoTracking()
            .Where(s => s.ProjectId == project.Id && s.EnvironmentId == environment.Id && segmentKeys.Contains(s.Key))
            .Select(s => s.Key)
            .ToListAsync();
        if (validator.Segments.FirstOrDefault(s => !existing.Contains(s.Key)) is { } missing)
        {
            return $"{missing.Path}: there is no segment \"{missing.Key}\" in environment \"{environment.Key}\" (segments belong to one environment).";
        }

        var stored = (await database.FlagEnvironmentConfigs.AsNoTracking()
                .Join(database.Flags, c => c.FlagId, f => f.Id, (c, f) => new { f.ProjectId, f.Key, c.EnvironmentId, c.Prerequisites })
                .Where(r => r.ProjectId == project.Id && r.EnvironmentId == environment.Id)
                .ToListAsync())
            .ToDictionary(r => r.Key, r => PrerequisiteGraph.Edges(r.Prerequisites), StringComparer.Ordinal);
        return PrerequisiteGraph.CycleThrough(flag.Key, validator.Prerequisites.Select(p => p.FlagKey).ToList(), stored) is { } cycle
            ? $"prerequisites would make a cycle: {string.Join(" -> ", cycle)}."
            : null;
    }

    private static string Serialize(JsonElement? array) => array is { ValueKind: JsonValueKind.Array } value ? JsonSerializer.Serialize(value) : "[]";

    private static IResult Respond(HttpContext http, FlagEnvironmentConfig config)
    {
        http.Response.Headers.ETag = $"\"{config.Version}\"";
        return Results.Json(Describe(config));
    }

    private static ConfigResponse Describe(FlagEnvironmentConfig config) => new(
        config.On,
        config.OffVariation,
        Parse(config.Targets),
        Parse(config.Prerequisites),
        Parse(config.Rules),
        Parse(config.Fallthrough),
        config.Version);

    private static JsonElement Parse(string json) => JsonSerializer.Deserialize<JsonElement>(json);
}
