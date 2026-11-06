using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Catalogue;
using System.Text.Json;
using System.Text.Json.Serialization;
using ToggleSlash.Server.Data;
using Microsoft.EntityFrameworkCore;
using static ToggleSlash.Server.Sdk.SdkEndpoints;

namespace ToggleSlash.Server.Sdk;

/// <summary>
/// "Why is this flag on for this user?" for an admin, without an SDK key. It answers what
/// <c>POST /sdk/v1/evaluate</c> answers, for any environment, and records nothing: no usage count, no audit
/// event, and the context is neither stored nor logged. No CORS, so no browser reads it.
/// With <c>etag</c> it answers for the ruleset this environment served under that hash, from the bytes kept then,
/// through the same evaluator: "why was this flag on for this user, back then".
/// </summary>
public static class DiagnoseEndpoint
{
    /// <summary>
    /// What <see cref="EvaluateRequest"/> carries, and an optional <c>etag</c>: the hash of a ruleset this environment
    /// served (the lowercase hex of its <c>ETag</c>, no quotes), as <c>GET .../rulesets/{hash}</c> takes it. Left out,
    /// the current ruleset is used. The SDK's <c>evaluate</c> has no such field and still refuses it.
    /// </summary>
    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record DiagnoseRequest(JsonElement? Context, string? FlagKey = null, string? Etag = null);

    public static void MapDiagnoseEndpoint(this IEndpointRouteBuilder routes)
    {
        routes.MapPost("/api/v1/projects/{project}/environments/{env}/evaluate", Diagnose)
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithMetadata(new ReadOnlyRoute())
            .WithTags("Diagnostics")
            .WithName("diagnoseEvaluation")
            .Produces<EvaluateResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .WithSummary("Evaluates a context for any environment and records nothing. `etag` (a hash the environment served) evaluates against that ruleset instead of the current one.")
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    private static async Task<IResult> Diagnose(
        string project, string env, DiagnoseRequest request, HttpContext http, ToggleSlashDbContext database, RulesetReader reader, CancellationToken cancellation)
    {
        if (await database.Project(project, cancellation) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, env, cancellation) is not { } environment)
        {
            return Failures.EnvironmentNotFound();
        }
        var ask = new EvaluateRequest(request.Context, request.FlagKey);
        if (request.Etag is null)
        {
            var current = await EvaluateAll(ask, () => reader.Read(environment.Id, cancellation));
            return current.Failure ?? Results.Ok(current.Response);
        }
        var stored = await database.RulesetSnapshots.AsNoTracking()
            .SingleOrDefaultAsync(s => s.EnvironmentId == environment.Id && s.Hash == request.Etag, cancellation);
        if (stored is null)
        {
            return Failures.RulesetNotFound();
        }
        var outcome = await EvaluateAll(ask, () => Task.FromResult(RulesetSnapshot.FromStored(stored.Body)));
        return outcome.Failure ?? Results.Ok(outcome.Response);
    }
}
