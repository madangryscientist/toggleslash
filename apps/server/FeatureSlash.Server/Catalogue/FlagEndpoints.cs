using System.Security.Cryptography;
using System.Text.Json;
using FeatureSlash.Server.Audit;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Data;
using FeatureSlash.Server.Sdk;
using FeatureSlash.Server.Targeting;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Catalogue;

public static class FlagEndpoints
{
    /// <param name="Variations">At least 2 JSON values of one type. Fixed once the flag exists.</param>
    /// <param name="OffVariation">Index served when the flag is off, in every environment. Defaults to 1.</param>
    /// <param name="FallthroughVariation">Index served when the flag is on and nothing else matches, in every environment. Defaults to 0.</param>
    public sealed record CreateFlagRequest(
        string? Key,
        string? Name,
        List<JsonElement>? Variations,
        string? Description = null,
        bool? Temporary = null,
        string? Owner = null,
        List<string>? Tags = null,
        int? OffVariation = null,
        int? FallthroughVariation = null);

    /// <summary>Metadata only. A field left out stays as it is; an <c>owner</c> of <c>""</c> clears it.</summary>
    public sealed record UpdateFlagRequest(
        string? Name = null,
        string? Description = null,
        bool? Temporary = null,
        string? Owner = null,
        List<string>? Tags = null);

    public sealed record FlagResponse(
        string Key,
        string Name,
        string Description,
        List<JsonElement> Variations,
        bool Temporary,
        string? Owner,
        List<string> Tags,
        DateTime CreatedAt,
        DateTime? ArchivedAt);

    public static void MapFlagEndpoints(this IEndpointRouteBuilder routes)
    {
        var flags = routes.MapGroup("/api/v1/projects/{project}/flags")
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Flags");

        flags.MapPost("", Create)
            .WithName("createFlag")
            .Produces<FlagResponse>(StatusCodes.Status201Created)
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
        flags.MapGet("", List)
            .WithName("listFlags")
            .Produces<FlagResponse[]>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        flags.MapGet("{flag}", Read)
            .WithName("getFlag")
            .Produces<FlagResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        flags.MapPatch("{flag}", Update)
            .WithName("updateFlag")
            .Produces<FlagResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        flags.MapPost("{flag}/archive", Archive)
            .WithName("archiveFlag")
            .Produces<FlagResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        flags.MapPost("{flag}/restore", Restore)
            .WithName("restoreFlag")
            .Produces<FlagResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    /// <summary>
    /// The flag and its config row in every environment of the project are written together, or not at
    /// all. Every config starts off, serving <c>OffVariation</c> when off and <c>FallthroughVariation</c> when on.
    /// </summary>
    private static async Task<IResult> Create(string project, CreateFlagRequest request, FeatureSlashDbContext database, RulesetNotifier notifier)
    {
        var problem = Validation.Key(request.Key, "key")
            ?? Validation.Name(request.Name)
            ?? Validation.Variations(request.Variations)
            ?? Validation.Tags(request.Tags)
            ?? TextProblem(request.Description, "description", Validation.MaxDescriptionLength)
            ?? TextProblem(request.Owner, "owner", Validation.MaxOwnerLength)
            ?? IndexProblem(request.OffVariation, "offVariation", request.Variations!.Count)
            ?? IndexProblem(request.FallthroughVariation, "fallthroughVariation", request.Variations!.Count);
        if (problem is not null)
        {
            return Failures.BadRequest(problem);
        }
        if (await database.Project(project) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Flags.AnyAsync(f => f.ProjectId == owner.Id && f.Key == request.Key))
        {
            return Taken(request.Key!);
        }

        var flag = new Flag
        {
            ProjectId = owner.Id,
            Key = request.Key!,
            Name = request.Name!.Trim(),
            Description = request.Description ?? "",
            Variations = JsonSerializer.Serialize(request.Variations),
            Salt = Convert.ToHexStringLower(RandomNumberGenerator.GetBytes(16)),
            Temporary = request.Temporary ?? false,
            Owner = string.IsNullOrWhiteSpace(request.Owner) ? null : request.Owner.Trim(),
            Tags = JsonSerializer.Serialize(Validation.DistinctTags(request.Tags ?? [])),
            CreatedAt = DateTime.UtcNow,
        };
        await using var transaction = await database.Database.BeginTransactionAsync();
        database.Flags.Add(flag);
        try
        {
            await database.SaveChangesAsync();
        }
        catch (DbUpdateException exception) when (Failures.IsUniqueViolation(exception))
        {
            return Taken(request.Key!);
        }
        var environmentIds = await database.Environments.Where(e => e.ProjectId == owner.Id).Select(e => e.Id).ToListAsync();
        database.FlagEnvironmentConfigs.AddRange(environmentIds.Select(id => FlagDefaults.NewConfig(
            flag.Id,
            id,
            request.OffVariation ?? FlagDefaults.OffVariation,
            request.FallthroughVariation ?? FlagDefaults.FallthroughVariation)));
        // One event for the write, however many default configs it brought with it.
        database.Record(AuditEntity.Flag, flag.Key, AuditAction.Created, new AuditScope(owner.Key, Flag: flag.Key), null, Describe(flag));
        await database.SaveChangesAsync();
        await transaction.CommitAsync();
        environmentIds.ForEach(notifier.Changed);

        return Results.Created($"/api/v1/projects/{owner.Key}/flags/{flag.Key}", Describe(flag));
    }

    /// <summary>Archived flags are hidden unless <c>archived</c> is <c>true</c> (only those) or <c>all</c>.</summary>
    private static async Task<IResult> List(string project, string? tag, string? archived, FeatureSlashDbContext database)
    {
        if (archived is not (null or "true" or "false" or "all"))
        {
            return Failures.BadRequest("archived must be \"true\", \"false\" or \"all\".");
        }
        if (await database.Project(project) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }

        var query = database.Flags.AsNoTracking().Where(f => f.ProjectId == owner.Id);
        query = archived switch
        {
            "true" => query.Where(f => f.ArchivedAt != null),
            "all" => query,
            _ => query.Where(f => f.ArchivedAt == null),
        };
        var flags = await query.OrderBy(f => f.Id).ToListAsync();
        // Tags are a JSON text column, so the tag filter reads them rather than asking SQL to.
        return Results.Json(flags.Where(f => tag is null || ReadStrings(f.Tags).Contains(tag)).Select(Describe));
    }

    private static async Task<IResult> Read(string project, string flag, FeatureSlashDbContext database)
    {
        var (owner, found) = await Find(database, project, flag, tracked: false);
        return found is not null ? Results.Json(Describe(found)) : NotFound(owner);
    }

    private static async Task<IResult> Update(string project, string flag, UpdateFlagRequest request, FeatureSlashDbContext database, TargetingLock targetingLock)
    {
        var problem = (request.Name is null ? null : Validation.Name(request.Name))
            ?? Validation.Tags(request.Tags)
            ?? TextProblem(request.Description, "description", Validation.MaxDescriptionLength)
            ?? TextProblem(request.Owner, "owner", Validation.MaxOwnerLength);
        if (problem is not null)
        {
            return Failures.BadRequest(problem);
        }
        // Under the lock so that the event's before and after are the row as it was and as it became.
        using var held = await targetingLock.Enter();
        var (owner, found) = await Find(database, project, flag, tracked: true);
        if (found is null)
        {
            return NotFound(owner);
        }

        var before = Describe(found);
        found.Name = request.Name?.Trim() ?? found.Name;
        found.Description = request.Description ?? found.Description;
        found.Temporary = request.Temporary ?? found.Temporary;
        if (request.Owner is not null)
        {
            found.Owner = string.IsNullOrWhiteSpace(request.Owner) ? null : request.Owner.Trim();
        }
        if (request.Tags is not null)
        {
            found.Tags = JsonSerializer.Serialize(Validation.DistinctTags(request.Tags));
        }
        database.Record(AuditEntity.Flag, found.Key, AuditAction.Updated, new AuditScope(owner!.Key, Flag: found.Key), before, Describe(found));
        await database.SaveChangesAsync();
        return Results.Json(Describe(found));
    }

    /// <summary>
    /// Archiving twice is fine and keeps the first time. Like a restore of a flag that is not archived, it
    /// changes nothing but is still a write that succeeded, so it still records one event (before equals after).
    /// An archived flag keeps its key and its configs.
    /// </summary>
    private static Task<IResult> Archive(string project, string flag, FeatureSlashDbContext database, TargetingLock targetingLock, RulesetNotifier notifier) =>
        SetArchived(project, flag, database, targetingLock, notifier, archived: true);

    private static Task<IResult> Restore(string project, string flag, FeatureSlashDbContext database, TargetingLock targetingLock, RulesetNotifier notifier) =>
        SetArchived(project, flag, database, targetingLock, notifier, archived: false);

    private static async Task<IResult> SetArchived(string project, string flag, FeatureSlashDbContext database, TargetingLock targetingLock, RulesetNotifier notifier, bool archived)
    {
        using var held = await targetingLock.Enter();
        var (owner, found) = await Find(database, project, flag, tracked: true);
        if (found is null)
        {
            return NotFound(owner);
        }

        var before = Describe(found);
        found.ArchivedAt = archived ? found.ArchivedAt ?? DateTime.UtcNow : null;
        database.Record(
            AuditEntity.Flag,
            found.Key,
            archived ? AuditAction.Archived : AuditAction.Restored,
            new AuditScope(owner!.Key, Flag: found.Key),
            before,
            Describe(found));
        // Read before the save: once the write has committed, nothing should be able to fail before the signal.
        var environmentIds = await database.Environments.Where(e => e.ProjectId == owner.Id).Select(e => e.Id).ToListAsync();
        await database.SaveChangesAsync();
        environmentIds.ForEach(notifier.Changed);
        return Results.Json(Describe(found));
    }

    private static async Task<(Project? Project, Flag? Flag)> Find(FeatureSlashDbContext database, string project, string flag, bool tracked)
    {
        var owner = await database.Project(project);
        if (owner is null)
        {
            return (null, null);
        }
        var flags = tracked ? database.Flags : database.Flags.AsNoTracking();
        return (owner, await flags.SingleOrDefaultAsync(f => f.ProjectId == owner.Id && f.Key == flag));
    }

    /// <summary>No project, or a project without that flag: the message says which.</summary>
    private static IResult NotFound(Project? owner) => owner is null ? Failures.ProjectNotFound() : Failures.FlagNotFound();

    private static IResult Taken(string key) =>
        Failures.Conflict($"A flag with key \"{key}\" already exists in this project (an archived flag keeps its key).");

    private static string? TextProblem(string? value, string field, int max) =>
        value is not null && value.Length > max ? $"{field} must be at most {max} characters." : null;

    private static string? IndexProblem(int? index, string field, int count) =>
        index is { } i && (i < 0 || i >= count) ? $"{field} must be an index into variations (0 to {count - 1})." : null;

    private static List<string> ReadStrings(string json) => JsonSerializer.Deserialize<List<string>>(json) ?? [];

    private static FlagResponse Describe(Flag flag) => new(
        flag.Key,
        flag.Name,
        flag.Description,
        JsonSerializer.Deserialize<List<JsonElement>>(flag.Variations) ?? [],
        flag.Temporary,
        flag.Owner,
        ReadStrings(flag.Tags),
        flag.CreatedAt,
        flag.ArchivedAt);
}
