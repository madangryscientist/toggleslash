using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Catalogue;
using ToggleSlash.Server.Data;
using Microsoft.EntityFrameworkCore;

namespace ToggleSlash.Server.Targeting;

/// <summary>One read of every flag's state in one environment, so a list of flags does not read them one by one.</summary>
public static class FlagStateEndpoints
{
    /// <param name="Flag">The flag's key.</param>
    /// <param name="On">Whether the flag is on in this environment.</param>
    /// <param name="Version">The config's version, the one <c>GET .../flags/{flag}/environments/{env}</c> returns.</param>
    /// <param name="LastEvaluated">
    /// The latest UTC day <em>this environment</em> counted an evaluation of the flag; <c>null</c> if it never has.
    /// (The flag routes' <c>lastEvaluated</c> is the latest day in any environment.)
    /// </param>
    public sealed record FlagState(string Flag, bool On, long Version, DateOnly? LastEvaluated);

    public static void MapFlagStateEndpoints(this IEndpointRouteBuilder routes) =>
        routes.MapGet("/api/v1/projects/{project}/environments/{env}/flag-states", List)
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Flag targeting")
            .WithName("listFlagStates")
            .Produces<List<FlagState>>()
            .Produces<ApiError>(StatusCodes.Status404NotFound);

    /// <summary>
    /// The project's non-archived flags by flag id. Four queries however many flags there are: the project, the
    /// environment, the flags with their configs, and the latest usage day per flag. A flag with no config in the
    /// environment has no state to report and is left out.
    /// </summary>
    private static async Task<IResult> List(string project, string env, ToggleSlashDbContext database, CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, env, cancellation) is not { } environment)
        {
            return Failures.EnvironmentNotFound();
        }

        var states = await (
            from flag in database.Flags.AsNoTracking()
            join config in database.FlagEnvironmentConfigs.AsNoTracking() on flag.Id equals config.FlagId
            where flag.ProjectId == owner.Id && flag.ArchivedAt == null && config.EnvironmentId == environment.Id
            orderby flag.Id
            select new { flag.Id, flag.Key, config.On, config.Version })
            .ToListAsync(cancellation);
        var last = await database.FlagUsageDaily.AsNoTracking()
            .Where(u => u.EnvironmentId == environment.Id)
            .GroupBy(u => u.FlagId)
            .Select(g => new { FlagId = g.Key, Day = g.Max(u => u.Day) })
            .ToDictionaryAsync(g => g.FlagId, g => g.Day, cancellation);

        return Results.Json(states.Select(s => new FlagState(s.Key, s.On, s.Version, last.TryGetValue(s.Id, out var day) ? day : null)));
    }
}
