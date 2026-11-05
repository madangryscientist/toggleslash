using FeatureSlash.Server.Audit;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace FeatureSlash.Server.Sdk;

/// <summary>
/// "What is this server serving for this environment right now?" It reports the server's side only: the tag and
/// flag count of the ruleset it would serve, and the last write that could have changed it. It cannot know what
/// any SDK has loaded. A read, so a scoped token of the project, read or write, may use it.
/// </summary>
public static class RuntimeEndpoint
{
    /// <param name="Etag">The <c>ETag</c> <c>GET /sdk/v1/ruleset</c> gives a server key in this environment, quotes included.</param>
    /// <param name="FlagCount">Flags in that ruleset; archived flags are not in it.</param>
    /// <param name="GeneratedAt">The server's time at the read.</param>
    /// <param name="LastChange">The newest write that could change the ruleset; <c>null</c> if there is none.</param>
    public sealed record RuntimeResponse(string Etag, int FlagCount, DateTime GeneratedAt, LastChangeResponse? LastChange);

    /// <param name="Actor"><c>admin</c> or <c>token:&lt;id&gt;</c>, as stored.</param>
    public sealed record LastChangeResponse(DateTime At, string Actor, string EntityType, string EntityId, string Action);

    public static void MapRuntimeEndpoint(this IEndpointRouteBuilder routes)
    {
        routes.MapGet("/api/v1/projects/{project}/environments/{env}/runtime", Runtime)
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Diagnostics")
            .WithName("getEnvironmentRuntime")
            .Produces<RuntimeResponse>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    private static async Task<IResult> Runtime(
        string project, string env, HttpContext http, FeatureSlashDbContext database, RulesetReader reader)
    {
        var cancellation = http.RequestAborted;
        if (await database.Project(project) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, env) is not { } environment)
        {
            return Failures.EnvironmentNotFound();
        }

        // The audit event first, then the ruleset, outside one transaction: a write between them can only make the
        // tag newer than lastChange, never leave a change that the tag does not show.
        var last = await LastChange(database, owner.Key, environment.Key, cancellation);
        var snapshot = await reader.Read(environment.Id, cancellation);
        return Results.Json(new RuntimeResponse(snapshot.ETag, snapshot.FlagKeys.Count, DateTime.UtcNow, last));
    }

    /// <summary>
    /// Config and segment events of this environment, and the flag events that add, archive or restore a flag in
    /// the project (they carry no environment). A flag <c>updated</c> event is not one: <c>PATCH</c> on a flag
    /// changes only its name, description, temporary, owner and tags, none of which the ruleset carries. An
    /// event whose <c>Before</c> equals its <c>After</c> changed nothing (archiving an archived flag). A toggle
    /// or save of a config that changes nothing still bumps the config's version, which the ruleset carries,
    /// so those snapshots differ and the event counts, rightly: the tag did change. A segment save that changes
    /// nothing also counts, though the ruleset carries no segment version and the tag does not move.
    /// </summary>
    private static async Task<LastChangeResponse?> LastChange(
        FeatureSlashDbContext database, string project, string environment, CancellationToken cancellation) =>
        await database.AuditEvents.AsNoTracking()
            .Where(a => a.ProjectKey == project && a.Before != a.After
                && (((a.EntityType == AuditEntity.FlagConfig || a.EntityType == AuditEntity.Segment) && a.EnvironmentKey == environment)
                    || (a.EntityType == AuditEntity.Flag
                        && (a.Action == AuditAction.Created || a.Action == AuditAction.Archived || a.Action == AuditAction.Restored))))
            .OrderByDescending(a => a.OccurredAt)
            .ThenByDescending(a => a.Id)
            .Select(a => new LastChangeResponse(a.OccurredAt, a.Actor, a.EntityType, a.EntityId, a.Action))
            .FirstOrDefaultAsync(cancellation);
}
