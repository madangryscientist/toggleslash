using System.Globalization;
using System.Text.Json;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Catalogue;
using ToggleSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace ToggleSlash.Server.Audit;

public static class AuditEndpoints
{
    private const int DefaultLimit = 50;
    private const int MaxLimit = 200;

    /// <param name="Before">The entity as the API showed it before the write; null when the write created it.</param>
    /// <param name="After">The entity as the API shows it after the write; null when the write deleted it.</param>
    public sealed record AuditEventResponse(
        long Id,
        DateTime OccurredAt,
        string Actor,
        string EntityType,
        string EntityId,
        string Action,
        string Project,
        string? Environment,
        string? Flag,
        JsonElement? Before,
        JsonElement? After);

    /// <param name="NextCursor">Send it back as <c>cursor</c> for the next page; null on the last page.</param>
    public sealed record AuditPage(AuditEventResponse[] Events, string? NextCursor);

    public static void MapAuditEndpoints(this IEndpointRouteBuilder routes)
    {
        var projects = routes.MapGroup("/api/v1/projects/{project}")
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Audit");

        projects.MapGet("audit", ProjectAudit)
            .WithName("listAuditEvents")
            .Produces<AuditPage>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
        projects.MapGet("flags/{flag}/history", FlagHistory)
            .WithName("getFlagHistory")
            .Produces<AuditPage>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    /// <summary>
    /// Newest first. <c>flag</c> and <c>environment</c> are keys and keep only events scoped to them (a flag
    /// event has no environment, so an <c>environment</c> filter leaves it out). <c>since</c> is an
    /// ISO 8601 time, inclusive. A <c>flag</c> that does not exist is an empty page, not a 404: events outlive
    /// what they describe.
    /// </summary>
    private static async Task<IResult> ProjectAudit(
        string project,
        string? flag,
        string? environment,
        string? since,
        int? limit,
        string? cursor,
        ToggleSlashDbContext database,
        CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is null)
        {
            return Failures.ProjectNotFound();
        }
        return await Page(database, project, flag, environment, since, limit, cursor, cancellation);
    }

    /// <summary>One flag's events, newest first: its metadata, archive state and every environment's config. Still answers after the flag is deleted.</summary>
    private static async Task<IResult> FlagHistory(
        string project,
        string flag,
        string? environment,
        string? since,
        int? limit,
        string? cursor,
        ToggleSlashDbContext database,
        CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        // A deleted flag keeps its trail, so its history still reads: only a key that never existed is a 404.
        if (!await database.Flags.AnyAsync(f => f.ProjectId == owner.Id && f.Key == flag, cancellation)
            && !await database.AuditEvents.AnyAsync(a => a.ProjectKey == project && a.FlagKey == flag, cancellation))
        {
            return Failures.FlagNotFound();
        }
        return await Page(database, project, flag, environment, since, limit, cursor, cancellation);
    }

    private static async Task<IResult> Page(
        ToggleSlashDbContext database,
        string project,
        string? flag,
        string? environment,
        string? since,
        int? limit,
        string? cursor,
        CancellationToken cancellation)
    {
        DateTime? sinceUtc = null;
        if (since is not null)
        {
            if (!DateTimeOffset.TryParse(since, CultureInfo.InvariantCulture, DateTimeStyles.AssumeUniversal, out var parsed))
            {
                return Failures.BadRequest("since must be an ISO 8601 time, such as 2026-11-05T09:00:00Z.");
            }
            sinceUtc = parsed.UtcDateTime;
        }
        if (limit is < 1 or > MaxLimit)
        {
            return Failures.BadRequest($"limit must be 1 to {MaxLimit}.");
        }
        long? before = null;
        if (cursor is not null)
        {
            if (!long.TryParse(cursor, NumberStyles.None, CultureInfo.InvariantCulture, out var id))
            {
                return Failures.BadRequest("cursor must be a nextCursor the previous page returned.");
            }
            before = id;
        }

        var query = database.AuditEvents.AsNoTracking().Where(a => a.ProjectKey == project);
        if (flag is not null)
        {
            query = query.Where(a => a.FlagKey == flag);
        }
        if (environment is not null)
        {
            query = query.Where(a => a.EnvironmentKey == environment);
        }
        if (sinceUtc is { } from)
        {
            query = query.Where(a => a.OccurredAt >= from);
        }
        if (before is { } last)
        {
            query = query.Where(a => a.Id < last);
        }

        var size = limit ?? DefaultLimit;
        // One more than a page, to know whether there is a next one without a count.
        var rows = await query.OrderByDescending(a => a.Id).Take(size + 1).ToListAsync(cancellation);
        var events = rows.Take(size).Select(Describe).ToArray();
        return Results.Json(new AuditPage(events, rows.Count > size ? events[^1].Id.ToString(CultureInfo.InvariantCulture) : null));
    }

    private static AuditEventResponse Describe(AuditEvent audit) => new(
        audit.Id,
        audit.OccurredAt,
        audit.Actor,
        audit.EntityType,
        audit.EntityId,
        audit.Action,
        audit.ProjectKey,
        audit.EnvironmentKey,
        audit.FlagKey,
        audit.Before is null ? null : JsonSerializer.Deserialize<JsonElement>(audit.Before),
        audit.After is null ? null : JsonSerializer.Deserialize<JsonElement>(audit.After));
}
