using System.Text.Json;
using System.Text.Json.Serialization;
using FeatureSlash.Evaluation;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Usage;
using Microsoft.Net.Http.Headers;

namespace FeatureSlash.Server.Sdk;

/// <summary>The read API an SDK talks to, under <c>/sdk/v1</c>. The environment is the one the SDK key belongs to.</summary>
public static class SdkEndpoints
{
    private const string RulesetPath = "/sdk/v1/ruleset";
    private const string EvaluatePath = "/sdk/v1/evaluate";
    private const string StreamPath = "/sdk/v1/stream";

    /// <summary>The CORS policy of the routes a browser calls with a client key: <c>evaluate</c> and <c>events</c>.</summary>
    internal const string ClientCors = "sdk-client";

    /// <summary>
    /// <c>context</c> is a context of <c>docs/evaluation.md</c>: <c>{ "key": "user-1", "attributes": { ... } }</c>.
    /// Send <c>flagKey</c> to evaluate that one flag; leave it out for every flag that is not archived.
    /// </summary>
    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record EvaluateRequest(JsonElement? Context, string? FlagKey = null);

    /// <summary>
    /// The reason, as <c>docs/evaluation.md</c> spells it. It names no rule beyond the index of the one that
    /// matched, and no target, segment or context.
    /// </summary>
    public sealed record ReasonResponse(
        string Kind,
        [property: JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] int? RuleIndex,
        [property: JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] string? FlagKey,
        [property: JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] string? Code);

    /// <summary><c>variationIndex</c> and <c>value</c> are <c>null</c> when nothing is served (an error, or no off variation).</summary>
    public sealed record EvaluationResponse(JsonElement Value, int? VariationIndex, ReasonResponse Reason);

    /// <summary>Flag key to its result, in key order.</summary>
    public sealed record EvaluateResponse(Dictionary<string, EvaluationResponse> Flags);

    public static IServiceCollection AddSdkApi(this IServiceCollection services)
    {
        services.AddScoped<RulesetReader>();
        services.AddSingleton<RulesetNotifier>();
        services.AddSingleton<StreamHeartbeat>();
        // A browser calls evaluate with a client key, which is public by design. The key goes in a header and
        // no cookie is involved, so any origin is safe to allow; a credentialed policy would not be.
        services.AddCors(options => options.AddPolicy(ClientCors, policy => policy
            .AllowAnyOrigin()
            .WithMethods(HttpMethods.Post)
            .WithHeaders(HeaderNames.Authorization, HeaderNames.ContentType)
            .SetPreflightMaxAge(TimeSpan.FromMinutes(10))));
        return services;
    }

    public static void MapSdkEndpoints(this IEndpointRouteBuilder routes)
    {
        routes.MapGet(RulesetPath, Ruleset)
            .RequireAuthorization(ServerAuth.ServerKeyPolicy)
            .WithTags("SDK")
            .WithName("getRuleset")
            .Produces<RulesetResponse>()
            .Produces(StatusCodes.Status304NotModified)
            .Produces<ApiError>(StatusCodes.Status401Unauthorized)
            .Produces<ApiError>(StatusCodes.Status403Forbidden);
        routes.MapGet(StreamPath, RulesetStream.Run)
            .RequireAuthorization(ServerAuth.ServerKeyPolicy)
            .WithTags("SDK")
            .WithName("streamRuleset")
            .WithSummary("Server-Sent Events: a `connected` event, then a `changed` event per change, each carrying the ruleset's ETag; a comment every 25 seconds.")
            .Produces<string>(StatusCodes.Status200OK, "text/event-stream")
            .Produces<ApiError>(StatusCodes.Status401Unauthorized)
            .Produces<ApiError>(StatusCodes.Status403Forbidden);
        // CORS on this route and events only: the ruleset holds salts and every rule, and no browser should read it.
        routes.MapPost(EvaluatePath, Evaluate)
            .RequireAuthorization(ServerAuth.SdkKeyPolicy)
            .RequireCors(ClientCors)
            .WithTags("SDK")
            .WithName("evaluate")
            .Produces<EvaluateResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status401Unauthorized)
            .Produces<ApiError>(StatusCodes.Status403Forbidden)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    /// <summary>
    /// 200 with the ruleset, or 304 when <c>If-None-Match</c> already names it. The tag is strong, so a
    /// weak validator (<c>W/"..."</c>) from a proxy still matches, and so does <c>*</c>.
    /// </summary>
    private static async Task<IResult> Ruleset(HttpContext http, RulesetReader reader)
    {
        var snapshot = await reader.Read(ServerAuth.EnvironmentId(http.User), http.RequestAborted);

        // Revalidate on every use: the cache may keep the body, but must ask before reusing it.
        http.Response.Headers.CacheControl = "private, no-cache";
        http.Response.Headers.ETag = snapshot.ETag;
        var tag = new EntityTagHeaderValue(snapshot.ETag);
        var sent = http.Request.GetTypedHeaders().IfNoneMatch;
        return sent.Any(candidate => candidate == EntityTagHeaderValue.Any || candidate.Compare(tag, useStrongComparison: false))
            ? Results.StatusCode(StatusCodes.Status304NotModified)
            : Results.Bytes(snapshot.Body, "application/json");
    }

    private static async Task<IResult> Evaluate(EvaluateRequest request, HttpContext http, RulesetReader reader, UsageRecorder usage)
    {
        if (request.Context is not { } context)
        {
            return Failures.BadRequest("A \"context\" is required: { \"key\": \"user-1\", \"attributes\": { } }.");
        }
        var environmentId = ServerAuth.EnvironmentId(http.User);
        var snapshot = await reader.Read(environmentId, http.RequestAborted);

        var ruleset = snapshot.Load();
        // The spec checks the context before it looks anything up, so any key tells a bad context from a good one.
        if (Evaluator.Evaluate(ruleset, "", context).Reason.Code == ErrorCode.InvalidContext)
        {
            return Failures.BadRequest("The context needs a non-empty string \"key\", and \"attributes\", if sent, must be an object.");
        }

        IReadOnlyList<string> keys = snapshot.FlagKeys;
        if (request.FlagKey is not null)
        {
            if (!keys.Contains(request.FlagKey))
            {
                return Failures.FlagNotFound();
            }
            keys = [request.FlagKey];
        }

        var flags = new Dictionary<string, EvaluationResponse>(keys.Count);
        var served = new List<UsageEntry>(keys.Count);
        var today = DateOnly.FromDateTime(DateTime.UtcNow);
        foreach (var key in keys)
        {
            var result = Evaluator.Evaluate(ruleset, key, context);
            flags[key] = new EvaluationResponse(
                result.Value,
                result.VariationIndex,
                new ReasonResponse(result.Reason.KindName, result.Reason.RuleIndex, result.Reason.FlagKey, result.Reason.CodeName));
            // An evaluation that served nothing (an error) has no variation to count.
            if (result.VariationIndex is { } variation)
            {
                served.Add(new UsageEntry(key, variation, 1, today));
            }
        }
        await usage.RecordEvaluations(environmentId, served, http.RequestAborted);
        return Results.Ok(new EvaluateResponse(flags));
    }
}
