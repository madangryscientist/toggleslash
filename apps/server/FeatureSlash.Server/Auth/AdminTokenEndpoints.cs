using System.Globalization;
using FeatureSlash.Server.Audit;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Auth;

/// <summary>Scoped admin tokens. Root token only, so a token can never mint a token or outlive its expiry.</summary>
public static class AdminTokenEndpoints
{
    private const int MaxExpiresInDays = 365;

    public sealed record CreateTokenRequest(string? Name, string? Permission, int? ExpiresInDays);

    /// <summary>What a list returns: everything about a token except anything that could become one.</summary>
    public sealed record TokenSummary(long Id, string Name, string Permission, string Prefix, DateTime CreatedAt, DateTime ExpiresAt, DateTime? RevokedAt);

    public static void MapAdminTokenEndpoints(this IEndpointRouteBuilder routes)
    {
        var tokens = routes.MapGroup("/api/v1/projects/{project}/tokens")
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithMetadata(new RootOnlyRoute())
            .WithTags("Tokens");

        tokens.MapPost("", Create)
            .WithName("createAdminToken")
            .Produces(StatusCodes.Status201Created)
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        tokens.MapGet("", List)
            .WithName("listAdminTokens")
            .Produces<TokenSummary[]>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        tokens.MapDelete("{id:long}", Revoke)
            .WithName("revokeAdminToken")
            .Produces(StatusCodes.Status204NoContent)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    private static async Task<IResult> Create(string project, CreateTokenRequest request, FeatureSlashDbContext database, CancellationToken cancellation)
    {
        AdminTokenPermission? permission = request.Permission switch
        {
            ServerAuth.ReadPermission => AdminTokenPermission.Read,
            ServerAuth.WritePermission => AdminTokenPermission.Write,
            _ => null,
        };
        if (permission is null)
        {
            return Failures.BadRequest("permission must be \"read\" or \"write\".");
        }
        if (Validation.Name(request.Name) is { } problem)
        {
            return Failures.BadRequest(problem);
        }
        if (request.ExpiresInDays is not (>= 1 and <= MaxExpiresInDays))
        {
            return Failures.BadRequest($"expiresInDays is required: a whole number from 1 to {MaxExpiresInDays}.");
        }
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }

        var secret = ApiKeySecret.GenerateAdminToken();
        var now = DateTime.UtcNow;
        var token = new AdminToken
        {
            ProjectId = owner.Id,
            Name = request.Name!.Trim(),
            Permission = permission.Value,
            Hash = secret.Hash,
            Prefix = secret.Prefix,
            CreatedAt = now,
            ExpiresAt = now.AddDays(request.ExpiresInDays.Value),
        };
        // Two saves in one transaction: the event names the token by its id, which exists only once the token is saved.
        await using var transaction = await database.Database.BeginTransactionAsync(cancellation);
        database.AdminTokens.Add(token);
        await database.SaveChangesAsync(cancellation);
        database.Record(
            AuditEntity.AdminToken, token.Id.ToString(CultureInfo.InvariantCulture), AuditAction.Created, new AuditScope(project), null, Summarise(token));
        await database.SaveChangesAsync(cancellation);
        await transaction.CommitAsync(cancellation);

        // The only place the plain token ever appears. Anonymous, so no named type carries it anywhere else.
        return Results.Json(
            new { token.Id, token.Name, Permission = ServerAuth.PermissionWord(token.Permission), token.Prefix, token.CreatedAt, token.ExpiresAt, token.RevokedAt, Token = secret.Plaintext },
            statusCode: StatusCodes.Status201Created);
    }

    /// <summary>Every token of the project, expired and revoked ones included.</summary>
    private static async Task<IResult> List(string project, FeatureSlashDbContext database, CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }

        var tokens = await database.AdminTokens.AsNoTracking()
            .Where(t => t.ProjectId == owner.Id)
            .OrderBy(t => t.Id)
            .ToListAsync(cancellation);
        return Results.Json(tokens.Select(Summarise));
    }

    /// <summary>Revoking twice is fine and keeps the first time. A token of another project is not found here.</summary>
    private static async Task<IResult> Revoke(string project, long id, FeatureSlashDbContext database, CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }

        var token = await database.AdminTokens.SingleOrDefaultAsync(t => t.Id == id && t.ProjectId == owner.Id, cancellation);
        if (token is null)
        {
            return ApiError.Result(StatusCodes.Status404NotFound, "not_found", "No such token in this project.");
        }

        var before = Summarise(token);
        token.RevokedAt ??= DateTime.UtcNow;
        database.Record(
            AuditEntity.AdminToken, token.Id.ToString(CultureInfo.InvariantCulture), AuditAction.Revoked, new AuditScope(project), before, Summarise(token));
        await database.SaveChangesAsync(cancellation);
        return Results.NoContent();
    }

    /// <summary>The audit event's snapshot of a token is the same summary a list returns: no hash, no plain token.</summary>
    private static TokenSummary Summarise(AdminToken t) =>
        new(t.Id, t.Name, ServerAuth.PermissionWord(t.Permission), t.Prefix, t.CreatedAt, t.ExpiresAt, t.RevokedAt);
}
