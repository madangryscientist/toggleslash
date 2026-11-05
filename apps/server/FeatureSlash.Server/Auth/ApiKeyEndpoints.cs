using System.Globalization;
using FeatureSlash.Server.Audit;
using FeatureSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Auth;

public static class ApiKeyEndpoints
{
    public sealed record CreateKeyRequest(string? Kind, string? Name);

    /// <summary>What a list returns: everything about a key except anything that could become one.</summary>
    public sealed record KeySummary(long Id, string Kind, string Name, string Prefix, DateTime CreatedAt, DateTime? RevokedAt);

    public static void MapApiKeyEndpoints(this IEndpointRouteBuilder routes)
    {
        var keys = routes.MapGroup("/api/v1/projects/{project}/environments/{env}/keys")
            .RequireAuthorization(ServerAuth.AdminPolicy);
        keys.MapPost("", Create);
        keys.MapGet("", List);
        keys.MapDelete("{id:long}", Revoke);
    }

    private static async Task<IResult> Create(string project, string env, CreateKeyRequest request, FeatureSlashDbContext database)
    {
        ApiKeyKind? kind = request.Kind switch
        {
            "server" => ApiKeyKind.Server,
            "client" => ApiKeyKind.Client,
            _ => null,
        };
        if (kind is null)
        {
            return ApiError.Result(StatusCodes.Status400BadRequest, "bad_request", "kind must be \"server\" or \"client\".");
        }
        if (string.IsNullOrWhiteSpace(request.Name))
        {
            return ApiError.Result(StatusCodes.Status400BadRequest, "bad_request", "name is required.");
        }
        if (await FindEnvironment(database, project, env) is not { } environment)
        {
            return EnvironmentNotFound();
        }

        var secret = ApiKeySecret.Generate(kind.Value);
        var key = new ApiKey
        {
            EnvironmentId = environment.Id,
            Kind = kind.Value,
            Name = request.Name,
            Hash = secret.Hash,
            Prefix = secret.Prefix,
            CreatedAt = DateTime.UtcNow,
        };
        // Two saves in one transaction: the event names the key by its id, which exists only once the key is saved.
        await using var transaction = await database.Database.BeginTransactionAsync();
        database.ApiKeys.Add(key);
        await database.SaveChangesAsync();
        database.Record(
            AuditEntity.ApiKey, key.Id.ToString(CultureInfo.InvariantCulture), AuditAction.Created, ScopeOf(project, env), null, Summarise(key));
        await database.SaveChangesAsync();
        await transaction.CommitAsync();

        // The only place the plain key ever appears. Anonymous, so no named type carries it anywhere else.
        return Results.Json(
            new { key.Id, Kind = KindWord(key.Kind), key.Name, key.Prefix, key.CreatedAt, key.RevokedAt, Key = secret.Plaintext },
            statusCode: StatusCodes.Status201Created);
    }

    private static async Task<IResult> List(string project, string env, FeatureSlashDbContext database)
    {
        if (await FindEnvironment(database, project, env) is not { } environment)
        {
            return EnvironmentNotFound();
        }

        var keys = await database.ApiKeys.AsNoTracking()
            .Where(k => k.EnvironmentId == environment.Id)
            .OrderBy(k => k.Id)
            .ToListAsync();
        return Results.Json(keys.Select(Summarise));
    }

    /// <summary>Revoking twice is fine and keeps the first time. A key of another environment is not found here.</summary>
    private static async Task<IResult> Revoke(string project, string env, long id, FeatureSlashDbContext database)
    {
        if (await FindEnvironment(database, project, env) is not { } environment)
        {
            return EnvironmentNotFound();
        }

        var key = await database.ApiKeys.SingleOrDefaultAsync(k => k.Id == id && k.EnvironmentId == environment.Id);
        if (key is null)
        {
            return ApiError.Result(StatusCodes.Status404NotFound, "not_found", "No such key in this environment.");
        }

        var before = Summarise(key);
        key.RevokedAt ??= DateTime.UtcNow;
        database.Record(
            AuditEntity.ApiKey, key.Id.ToString(CultureInfo.InvariantCulture), AuditAction.Revoked, ScopeOf(project, env), before, Summarise(key));
        await database.SaveChangesAsync();
        return Results.NoContent();
    }

    private static Task<ProjectEnvironment?> FindEnvironment(FeatureSlashDbContext database, string project, string env) =>
        database.Environments.AsNoTracking().SingleOrDefaultAsync(e =>
            e.Key == env && database.Projects.Any(p => p.Id == e.ProjectId && p.Key == project));

    private static IResult EnvironmentNotFound() =>
        ApiError.Result(StatusCodes.Status404NotFound, "not_found", "No such project or environment.");

    private static AuditScope ScopeOf(string project, string env) => new(project, env);

    /// <summary>The audit event's snapshot of a key is the same summary a list returns: no hash, no plain key.</summary>
    private static KeySummary Summarise(ApiKey k) => new(k.Id, KindWord(k.Kind), k.Name, k.Prefix, k.CreatedAt, k.RevokedAt);

    private static string KindWord(ApiKeyKind kind) => kind == ApiKeyKind.Server ? "server" : "client";
}
