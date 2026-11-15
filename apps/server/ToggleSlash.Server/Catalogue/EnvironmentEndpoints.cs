using System.Security.Cryptography;
using ToggleSlash.Server.Audit;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Data;
using ToggleSlash.Server.Targeting;
using Microsoft.EntityFrameworkCore;

namespace ToggleSlash.Server.Catalogue;

public static class EnvironmentEndpoints
{
    public sealed record CreateEnvironmentRequest(string? Key, string? Name);

    /// <summary>
    /// The name and secure mode can change, and a field left out stays as it is. The key cannot: it is what audit rows,
    /// clients and URLs address the environment by. Secure mode can be turned on only once the environment has a signing key.
    /// </summary>
    public sealed record UpdateEnvironmentRequest(string? Name = null, bool? SecureMode = null);

    /// <summary>
    /// <c>secureMode</c>: a client-key evaluate must carry a signature over its context's keys. <c>signingKeyCreatedAt</c>
    /// is when the current signing key was made, or null when there is none; the key itself is never returned here.
    /// </summary>
    public sealed record EnvironmentResponse(string Key, string Name, DateTime CreatedAt, bool SecureMode, DateTime? SigningKeyCreatedAt);

    /// <summary>The private half of a new signing key, as a PKCS#8 PEM. This answer is the only time it is sent; the server keeps only the public half.</summary>
    public sealed record SigningKeyResponse(string PrivateKey, DateTime CreatedAt);

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
        environments.MapPatch("{env}", Update)
            .WithName("updateEnvironment")
            .Produces<EnvironmentResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound)
            .Produces<ApiError>(StatusCodes.Status409Conflict);
        environments.MapPost("{env}/signing-key", CreateSigningKey)
            .WithName("createSigningKey")
            .WithSummary("Makes a new ECDSA P-256 signing key for secure mode and answers its private half, once. It replaces any key the environment had, so signatures made with that one stop verifying at once.")
            .Produces<SigningKeyResponse>(StatusCodes.Status201Created)
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
    /// Changes the name, secure mode or both. Neither is in a ruleset, so its ETag stays and no stream is told. The
    /// targeting lock is held so the event's before is the row's true before.
    /// </summary>
    private static async Task<IResult> Update(string project, string env, UpdateEnvironmentRequest request, ToggleSlashDbContext database, TargetingLock targetingLock, CancellationToken cancellation)
    {
        if (request.Name is not null && Validation.Name(request.Name) is { } problem)
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
        if (request.SecureMode == true && found.SigningPublicKey is null)
        {
            return Failures.Conflict(
                $"Environment \"{found.Key}\" has no signing key, so nothing could sign for it. Make one first (POST .../environments/{found.Key}/signing-key), then turn secure mode on.");
        }

        var before = Describe(found);
        var row = await database.Environments.SingleAsync(e => e.Id == found.Id, cancellation);
        row.Name = request.Name?.Trim() ?? row.Name;
        row.SecureMode = request.SecureMode ?? row.SecureMode;
        var updated = Describe(row);
        database.Record(AuditEntity.Environment, row.Key, AuditAction.Updated, new AuditScope(owner.Key, row.Key), before, updated);
        await database.SaveChangesAsync(cancellation);
        return Results.Json(updated);
    }

    /// <summary>
    /// Makes a P-256 key pair, stores the public half (SubjectPublicKeyInfo DER) over any old one and answers the private
    /// half (PKCS#8 PEM). The private half is in this answer only: not in the row, the audit event or a log. Replacing the
    /// key is the rotation: a signature made with the old one fails from the moment this commits.
    /// </summary>
    private static async Task<IResult> CreateSigningKey(string project, string env, ToggleSlashDbContext database, TargetingLock targetingLock, CancellationToken cancellation)
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

        using var pair = ECDsa.Create(ECCurve.NamedCurves.nistP256);
        var before = Describe(found);
        var row = await database.Environments.SingleAsync(e => e.Id == found.Id, cancellation);
        row.SigningPublicKey = pair.ExportSubjectPublicKeyInfo();
        row.SigningKeyCreatedAt = DateTime.UtcNow;
        var after = Describe(row);
        database.Record(AuditEntity.Environment, row.Key, AuditAction.Updated, new AuditScope(owner.Key, row.Key), before, after);
        await database.SaveChangesAsync(cancellation);
        return Results.Json(new SigningKeyResponse(pair.ExportPkcs8PrivateKeyPem(), row.SigningKeyCreatedAt.Value), statusCode: StatusCodes.Status201Created);
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
        new(environment.Key, environment.Name, environment.CreatedAt, environment.SecureMode, environment.SigningKeyCreatedAt);
}
