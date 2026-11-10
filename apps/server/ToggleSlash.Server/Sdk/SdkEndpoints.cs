using System.Text.Json;
using System.Text.Json.Serialization;
using ToggleSlash.Evaluation;
using ToggleSlash.Server.Auth;
using ToggleSlash.Server.Catalogue;
using ToggleSlash.Server.Limits;
using ToggleSlash.Server.Usage;
using Microsoft.Net.Http.Headers;

namespace ToggleSlash.Server.Sdk;

/// <summary>The read API an SDK talks to, under <c>/sdk/v1</c>. The environment is the one the SDK key belongs to.</summary>
public static class SdkEndpoints
{
    private const string RulesetPath = "/sdk/v1/ruleset";
    private const string EvaluatePath = "/sdk/v1/evaluate";
    private const string StreamPath = "/sdk/v1/stream";

    /// <summary>The CORS policy of the routes a browser calls with a client key: <c>evaluate</c> and <c>events</c>.</summary>
    internal const string ClientCors = "sdk-client";

    /// <summary>The two shapes of a context, for a 400 that refuses one.</summary>
    internal const string ContextShapes =
        "It must be { \"kind\"?, \"key\", \"attributes\"? } or { \"kind\": \"multi\", \"contexts\": { \"<kind>\": { \"key\", \"attributes\"? } } }, where a kind matches [A-Za-z0-9._-]+ and is not \"kind\" or \"multi\"; see docs/evaluation.md.";

    /// <summary>
    /// <c>context</c> is a context of <c>docs/evaluation.md</c>, one of two shapes: a single context,
    /// <c>{ "kind": "user", "key": "user-1", "attributes": { ... } }</c> (<c>kind</c> optional, absent is <c>user</c>), or a multi-context,
    /// <c>{ "kind": "multi", "contexts": { "user": { "key": "user-1" }, "org": { "key": "acme", "attributes": { ... } } } }</c>.
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
            // Without this a browser's script cannot read how long a 429 asks it to wait.
            .WithExposedHeaders(HeaderNames.RetryAfter)
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
            .Produces<ApiError>(StatusCodes.Status403Forbidden)
            // A stream is open for hours and says nothing between changes; no request timeout may end it.
            .DisableRequestTimeout();
        // CORS on this route and events only: the ruleset holds salts and every rule, and no browser should read it.
        routes.MapPost(EvaluatePath, Evaluate)
            .RequireAuthorization(ServerAuth.SdkKeyPolicy)
            .RequireCors(ClientCors)
            .LimitedPerKey(KeyLimit.Evaluate)
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
        var environmentId = ServerAuth.EnvironmentId(http.User);
        var snapshot = await reader.Read(environmentId, http.RequestAborted);

        // Revalidate on every use: the cache may keep the body, but must ask before reusing it.
        http.Response.Headers.CacheControl = "private, no-cache";
        http.Response.Headers.ETag = snapshot.ETag;
        var tag = new EntityTagHeaderValue(snapshot.ETag);
        var sent = http.Request.GetTypedHeaders().IfNoneMatch;
        if (sent.Any(candidate => candidate == EntityTagHeaderValue.Any || candidate.Compare(tag, useStrongComparison: false)))
        {
            return Results.StatusCode(StatusCodes.Status304NotModified);
        }
        await reader.RecordServed(environmentId, snapshot, http.RequestAborted);
        return Results.Bytes(snapshot.Body, "application/json");
    }

    private static async Task<IResult> Evaluate(EvaluateRequest request, HttpContext http, RulesetReader reader, UsageRecorder usage)
    {
        var environmentId = ServerAuth.EnvironmentId(http.User);
        var outcome = await EvaluateAll(request, () => reader.Read(environmentId, http.RequestAborted));
        if (outcome.Failure is not null)
        {
            return outcome.Failure;
        }
        await usage.RecordEvaluations(environmentId, outcome.Served, http.RequestAborted);
        return Results.Ok(outcome.Response);
    }

    /// <summary>
    /// What <see cref="EvaluateAll"/> found: a <see cref="Failure"/> to send, or the <see cref="Response"/> and the
    /// <see cref="Served"/> evaluations a caller may count. Counting is the caller's choice, not this method's.
    /// </summary>
    internal sealed record EvaluationOutcome(IResult? Failure, EvaluateResponse? Response, IReadOnlyList<UsageEntry> Served);

    /// <summary>
    /// Validates the request and evaluates one flag, or every flag that is not archived, against the ruleset.
    /// Both <c>POST /sdk/v1/evaluate</c> and the admin diagnostic route call it. It records nothing: the ruleset is
    /// read only after the context is known to be present, so a bad request costs no read.
    /// </summary>
    internal static async Task<EvaluationOutcome> EvaluateAll(EvaluateRequest request, Func<Task<RulesetSnapshot>> readSnapshot)
    {
        if (request.Context is not { } context)
        {
            return Rejected(Failures.BadRequest($"A \"context\" is required. {ContextShapes}"));
        }
        var snapshot = await readSnapshot();

        var ruleset = snapshot.Load();
        // The spec checks the context before it looks anything up, so any key tells a bad context from a good one.
        if (Evaluator.Evaluate(ruleset, "", context).Reason.Code == ErrorCode.InvalidContext)
        {
            return Rejected(Failures.BadRequest($"The context is not valid. {ContextShapes}"));
        }

        IReadOnlyList<string> keys = snapshot.FlagKeys;
        if (request.FlagKey is not null)
        {
            if (!keys.Contains(request.FlagKey))
            {
                return Rejected(Failures.FlagNotFound());
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
        return new EvaluationOutcome(null, new EvaluateResponse(flags), served);
    }

    private static EvaluationOutcome Rejected(IResult failure) => new(failure, null, []);
}
