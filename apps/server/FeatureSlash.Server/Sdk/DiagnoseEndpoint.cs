using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Data;
using static FeatureSlash.Server.Sdk.SdkEndpoints;

namespace FeatureSlash.Server.Sdk;

/// <summary>
/// "Why is this flag on for this user?" for an admin, without an SDK key. It answers what
/// <c>POST /sdk/v1/evaluate</c> answers, for any environment, and records nothing: no usage count, no audit
/// event, and the context is neither stored nor logged. No CORS, so no browser reads it.
/// </summary>
public static class DiagnoseEndpoint
{
    public static void MapDiagnoseEndpoint(this IEndpointRouteBuilder routes)
    {
        routes.MapPost("/api/v1/projects/{project}/environments/{env}/evaluate", Diagnose)
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithMetadata(new ReadOnlyRoute())
            .WithTags("Diagnostics")
            .WithName("diagnoseEvaluation")
            .Produces<EvaluateResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    private static async Task<IResult> Diagnose(
        string project, string env, EvaluateRequest request, HttpContext http, FeatureSlashDbContext database, RulesetReader reader)
    {
        if (await database.Project(project) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Environment(owner.Id, env) is not { } environment)
        {
            return Failures.EnvironmentNotFound();
        }
        var outcome = await EvaluateAll(request, () => reader.Read(environment.Id, http.RequestAborted));
        return outcome.Failure ?? Results.Ok(outcome.Response);
    }
}
