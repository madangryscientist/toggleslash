using ToggleSlash.Server.Audit;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Data;
using ToggleSlash.Server.Targeting;
using Microsoft.EntityFrameworkCore;

namespace ToggleSlash.Server.Catalogue;

public static class EnvironmentEndpoints
{
    public sealed record CreateEnvironmentRequest(string? Key, string? Name);

    /// <summary>Only the name can change: an environment's key is what audit rows, clients and URLs address it by.</summary>
    public sealed record RenameEnvironmentRequest(string? Name);

    public sealed record EnvironmentResponse(string Key, string Name, DateTime CreatedAt);

    public static void MapEnvironmentEndpoints(this IEndpointRouteBuilder routes)
    {
        var environments = routes.MapGroup("/api/v1/projects/{project}/environments")
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Environments");

        environments.MapPost("", Create)
            .WithName("createEnvironment")
            .Produces<EnvironmentResponse>(StatusCodes.Status201Created)
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
        environments.MapGet("", List)
            .WithName("listEnvironments")
            .Produces<EnvironmentResponse[]>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        environments.MapGet("{env}", Read)
            .WithName("getEnvironment")
            .Produces<EnvironmentResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        environments.MapPatch("{env}", Rename)
            .WithName("renameEnvironment")
            .Produces<EnvironmentResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        environments.MapDelete("{env}", Delete)
            .WithName("deleteEnvironment")
            .Produces(StatusCodes.Status204NoContent)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
    }

    /// <summary>
    /// A new environment gets an off config row for every flag the project already has, archived ones
    /// included, so "every flag has a config in every environment" holds whichever was created first.
    /// </summary>
    private static async Task<IResult> Create(string project, CreateEnvironmentRequest request, ToggleSlashDbContext database, CancellationToken cancellation)
    {
        if ((Validation.Key(request.Key, "key") ?? Validation.Name(request.Name)) is { } problem)
        {
            return Failures.BadRequest(problem);
        }
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, request.Key!, cancellation) is not null)
        {
            return Taken(request.Key!);
        }

        var environment = new ProjectEnvironment
        {
            ProjectId = owner.Id,
            Key = request.Key!,
            Name = request.Name!.Trim(),
            CreatedAt = DateTime.UtcNow,
        };
        await using var transaction = await database.Database.BeginTransactionAsync(cancellation);
        database.Environments.Add(environment);
        try
        {
            await database.SaveChangesAsync(cancellation);
            var flagIds = await database.Flags.Where(f => f.ProjectId == owner.Id).Select(f => f.Id).ToListAsync(cancellation);
            database.FlagEnvironmentConfigs.AddRange(flagIds.Select(id =>
                FlagDefaults.NewConfig(id, environment.Id, FlagDefaults.OffVariation, FlagDefaults.FallthroughVariation)));
            // One event for the write, however many default configs it brought with it.
            database.Record(
                AuditEntity.Environment, environment.Key, AuditAction.Created, new AuditScope(owner.Key, environment.Key), null, Describe(environment));
            await database.SaveChangesAsync(cancellation);
            await transaction.CommitAsync(cancellation);
        }
        catch (DbUpdateException exception) when (Failures.IsUniqueViolation(exception))
        {
            return Taken(request.Key!);
        }
        return Results.Created($"/api/v1/projects/{owner.Key}/environments/{environment.Key}", Describe(environment));
    }

    private static async Task<IResult> List(string project, ToggleSlashDbContext database, CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        var environments = await database.Environments.AsNoTracking()
            .Where(e => e.ProjectId == owner.Id)
            .OrderBy(e => e.Id)
            .ToListAsync(cancellation);
        return Results.Json(environments.Select(Describe));
    }

    private static async Task<IResult> Read(string project, string env, ToggleSlashDbContext database, CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        return await database.Environment(owner.Id, env, cancellation) is { } found
            ? Results.Json(Describe(found))
            : Failures.EnvironmentNotFound();
    }

    /// <summary>
    /// Changes the name and nothing else. A ruleset does not carry the environment's name, so its ETag stays
    /// and no stream is told. The targeting lock is held so the event's before is the row's true before.
    /// </summary>
    private static async Task<IResult> Rename(string project, string env, RenameEnvironmentRequest request, ToggleSlashDbContext database, TargetingLock targetingLock, CancellationToken cancellation)
    {
        if (Validation.Name(request.Name) is { } problem)
        {
            return Failures.BadRequest(problem);
        }
        using var held = await targetingLock.Enter(cancellation);
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, env, cancellation) is not { } found)
        {
            return Failures.EnvironmentNotFound();
        }

        var before = Describe(found);
        var row = await database.Environments.SingleAsync(e => e.Id == found.Id, cancellation);
        row.Name = request.Name!.Trim();
        var renamed = Describe(row);
        database.Record(AuditEntity.Environment, row.Key, AuditAction.Updated, new AuditScope(owner.Key, row.Key), before, renamed);
        await database.SaveChangesAsync(cancellation);
        return Results.Json(renamed);
    }

    /// <summary>
    /// Refused while any SDK key of the environment is unrevoked: a live key means something may be running against
    /// it. Everything with a foreign key to the row goes with it (the database cascades), and the audit trail stays,
    /// because it addresses the environment by key. The check and the delete share one transaction, under the
    /// targeting lock. A stream still open ends at its next wake-up, when its key is no longer live, as after a revoke.
    /// </summary>
    private static async Task<IResult> Delete(string project, string env, ToggleSlashDbContext database, TargetingLock targetingLock, CancellationToken cancellation)
    {
        using var held = await targetingLock.Enter(cancellation);
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, env, cancellation) is not { } found)
        {
            return Failures.EnvironmentNotFound();
        }

        await using var transaction = await database.Database.BeginTransactionAsync(cancellation);
        var live = await database.ApiKeys.CountAsync(k => k.EnvironmentId == found.Id && k.RevokedAt == null, cancellation);
        if (live > 0)
        {
            return Failures.Conflict(
                $"Environment \"{found.Key}\" still has {live} SDK key(s) that are not revoked. Revoke them first.");
        }
        await database.Environments.Where(e => e.Id == found.Id).ExecuteDeleteAsync(cancellation);
        database.Record(
            AuditEntity.Environment, found.Key, AuditAction.Deleted, new AuditScope(owner.Key, found.Key), Describe(found), null);
        await database.SaveChangesAsync(cancellation);
        await transaction.CommitAsync(cancellation);
        return Results.NoContent();
    }

    private static IResult Taken(string key) =>
        Failures.Conflict($"An environment with key \"{key}\" already exists in this project.");

    private static EnvironmentResponse Describe(ProjectEnvironment environment) =>
        new(environment.Key, environment.Name, environment.CreatedAt);
}
