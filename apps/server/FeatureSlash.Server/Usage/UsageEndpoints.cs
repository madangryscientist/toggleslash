using System.Globalization;
using System.Text.Json;
using System.Text.Json.Serialization;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Catalogue;
using FeatureSlash.Server.Data;
using FeatureSlash.Server.Limits;
using FeatureSlash.Server.Sdk;
using Microsoft.AspNetCore.Http.Json;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Options;

namespace FeatureSlash.Server.Usage;

/// <summary>
/// Usage counts: how often each variation of a flag was served, per environment per day. Counts only. The
/// server never stores a context, a context key or any attribute of an end user, here or anywhere.
/// </summary>
public static class UsageEndpoints
{
    /// <summary>Entries in one <c>POST /sdk/v1/events</c>. More is a 413: send two batches.</summary>
    private const int MaxEvents = 1000;

    /// <summary>Bytes in the body of one <c>POST /sdk/v1/events</c>. More is a 413, and the rest of the body is not read.</summary>
    private const int MaxBodyBytes = 128 * 1024;

    /// <summary>The largest <c>count</c> one entry may carry.</summary>
    private const long MaxCount = 1_000_000_000;

    /// <summary>The widest window the usage read answers, in days.</summary>
    private const int MaxWindowDays = 366;

    private const int DefaultWindowDays = 30;
    private const string DayFormat = "yyyy-MM-dd";

    /// <param name="Flag">Flag key.</param>
    /// <param name="VariationIndex">Index into the flag's variations.</param>
    /// <param name="Count">Times it was served, 1 to 1,000,000,000.</param>
    /// <param name="Day">The UTC day, <c>yyyy-MM-dd</c>. No later than tomorrow (UTC), so a skewed clock cannot park a flag in the future.</param>
    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record UsageEvent(string? Flag, int? VariationIndex, long? Count, string? Day);

    /// <summary>At most 1000 events and 128 KiB. The environment is the SDK key's own; the request cannot name one.</summary>
    [JsonUnmappedMemberHandling(JsonUnmappedMemberHandling.Disallow)]
    public sealed record EventsRequest(List<UsageEvent>? Events);

    /// <param name="Accepted">Events counted.</param>
    /// <param name="UnknownFlags">Events ignored because the key's project has no such flag.</param>
    public sealed record EventsResponse(int Accepted, int UnknownFlags);

    public sealed record UsageVariation(int VariationIndex, long Count);

    public sealed record UsageDay(DateOnly Day, long Count, UsageVariation[] Variations);

    /// <param name="Total">Evaluations in the window.</param>
    /// <param name="Days">Days with any evaluations, oldest first.</param>
    public sealed record UsageEnvironment(string Environment, long Total, UsageDay[] Days);

    /// <param name="Since">First day of the window, inclusive.</param>
    /// <param name="Until">Last day of the window, inclusive.</param>
    public sealed record UsageResponse(string Flag, DateOnly Since, DateOnly Until, UsageEnvironment[] Environments);

    public static IServiceCollection AddUsage(this IServiceCollection services) =>
        services.AddScoped<UsageRecorder>();

    public static void MapUsageEndpoints(this IEndpointRouteBuilder routes)
    {
        routes.MapPost("/sdk/v1/events", Events)
            .RequireAuthorization(ServerAuth.SdkKeyPolicy)
            .RequireCors(SdkEndpoints.ClientCors)
            .LimitedPerKey(KeyLimit.Events)
            .WithTags("SDK")
            .WithName("postEvents")
            .WithSummary("Adds a batch of usage counts to the key's own environment. Counts only.")
            .Accepts<EventsRequest>("application/json")
            .Produces<EventsResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status401Unauthorized)
            .Produces<ApiError>(StatusCodes.Status403Forbidden)
            .Produces<ApiError>(StatusCodes.Status413PayloadTooLarge);
        routes.MapGet("/api/v1/projects/{project}/flags/{flag}/usage", Usage)
            .RequireAuthorization(ServerAuth.AdminPolicy)
            .WithTags("Flags")
            .WithName("getFlagUsage")
            .WithSummary("Daily evaluation counts per environment. `since` and `until` are `yyyy-MM-dd` (UTC, inclusive); the default is the last 30 days.")
            .Produces<UsageResponse>()
            .Produces<ApiError>(StatusCodes.Status400BadRequest)
            .Produces<ApiError>(StatusCodes.Status404NotFound);
    }

    /// <summary>
    /// The body is read here, not bound, so that an oversized one is refused (413) before it is buffered, and
    /// the same serializer options as the rest of the API apply (a number is a JSON number).
    /// </summary>
    private static async Task<IResult> Events(HttpContext http, UsageRecorder recorder, IOptions<JsonOptions> json)
    {
        var tooLarge = ApiError.Result(
            StatusCodes.Status413PayloadTooLarge, "payload_too_large", $"A batch is at most {MaxEvents} events and {MaxBodyBytes} bytes.");
        if (http.Request.ContentLength > MaxBodyBytes)
        {
            return tooLarge;
        }
        using var body = new MemoryStream();
        var chunk = new byte[8192];
        int read;
        while ((read = await http.Request.Body.ReadAsync(chunk, http.RequestAborted)) > 0)
        {
            if (body.Length + read > MaxBodyBytes)
            {
                return tooLarge;
            }
            body.Write(chunk, 0, read);
        }

        EventsRequest? request;
        try
        {
            request = JsonSerializer.Deserialize<EventsRequest>(body.ToArray(), json.Value.SerializerOptions);
        }
        catch (JsonException)
        {
            return Failures.BadRequest("The body must be JSON like { \"events\": [{ \"flag\": \"checkout\", \"variationIndex\": 0, \"count\": 3, \"day\": \"2026-11-05\" }] }, with nothing else in it.");
        }
        if (request?.Events is not { } events)
        {
            return Failures.BadRequest("\"events\" is required.");
        }
        if (events.Count > MaxEvents)
        {
            return tooLarge;
        }

        var tomorrow = DateOnly.FromDateTime(DateTime.UtcNow).AddDays(1);
        var entries = new List<UsageEntry>(events.Count);
        for (var i = 0; i < events.Count; i++)
        {
            // System.Text.Json lets a null element through a non-nullable list item type.
            if (events[i] is null)
            {
                return Failures.BadRequest($"events[{i}]: must be an object.");
            }
            if (Entry(events[i], tomorrow, out var entry) is { } problem)
            {
                return Failures.BadRequest($"events[{i}]: {problem}");
            }
            entries.Add(entry!);
        }

        var result = await recorder.Record(ServerAuth.EnvironmentId(http.User), entries, http.RequestAborted);
        return result.OutOfRange is { } bad
            ? Failures.BadRequest($"\"{bad.FlagKey}\" has no variation {bad.Variation}. Nothing in the batch was counted.")
            : Results.Ok(new EventsResponse(result.Accepted, result.UnknownFlags));
    }

    private static string? Entry(UsageEvent usage, DateOnly latest, out UsageEntry? entry)
    {
        entry = null;
        if (string.IsNullOrEmpty(usage.Flag))
        {
            return "flag is required.";
        }
        if (usage.VariationIndex is not (>= 0 and <= Validation.MaxVariations))
        {
            return "variationIndex must be an integer from 0 to 100.";
        }
        if (usage.Count is not (>= 1 and <= MaxCount))
        {
            return $"count must be an integer from 1 to {MaxCount}.";
        }
        if (!DateOnly.TryParseExact(usage.Day, DayFormat, CultureInfo.InvariantCulture, DateTimeStyles.None, out var day))
        {
            return "day must be a date like 2026-11-05.";
        }
        if (day > latest)
        {
            return "day is in the future.";
        }
        if (day < latest.AddDays(-1 - MaxWindowDays))
        {
            return $"day is more than {MaxWindowDays} days ago, which is further back than the usage read goes.";
        }
        entry = new UsageEntry(usage.Flag, usage.VariationIndex.Value, usage.Count.Value, day);
        return null;
    }

    private static async Task<IResult> Usage(string project, string flag, string? since, string? until, FeatureSlashDbContext database)
    {
        var last = DateOnly.FromDateTime(DateTime.UtcNow);
        if (until is not null && !DateOnly.TryParseExact(until, DayFormat, CultureInfo.InvariantCulture, DateTimeStyles.None, out last))
        {
            return Failures.BadRequest("until must be a date like 2026-11-05.");
        }
        var first = last < DateOnly.MinValue.AddDays(DefaultWindowDays) ? DateOnly.MinValue : last.AddDays(1 - DefaultWindowDays);
        if (since is not null && !DateOnly.TryParseExact(since, DayFormat, CultureInfo.InvariantCulture, DateTimeStyles.None, out first))
        {
            return Failures.BadRequest("since must be a date like 2026-11-05.");
        }
        if (first > last || last.DayNumber - first.DayNumber >= MaxWindowDays)
        {
            return Failures.BadRequest($"since must not be after until, and the window is at most {MaxWindowDays} days.");
        }
        if (await database.Project(project) is not { } owner)
        {
            return Failures.ProjectNotFound();
        }
        if (await database.Flags.AsNoTracking().SingleOrDefaultAsync(f => f.ProjectId == owner.Id && f.Key == flag) is not { } found)
        {
            return Failures.FlagNotFound();
        }

        var environments = await database.Environments.AsNoTracking().Where(e => e.ProjectId == owner.Id).OrderBy(e => e.Id).ToListAsync();
        var rows = await database.FlagUsageDaily.AsNoTracking()
            .Where(u => u.FlagId == found.Id && u.Day >= first && u.Day <= last)
            .OrderBy(u => u.Day).ThenBy(u => u.Variation)
            .ToListAsync();

        return Results.Json(new UsageResponse(
            found.Key,
            first,
            last,
            [.. environments.Select(environment =>
            {
                var mine = rows.Where(u => u.EnvironmentId == environment.Id).ToList();
                return new UsageEnvironment(
                    environment.Key,
                    mine.Sum(u => u.Count),
                    [.. mine.GroupBy(u => u.Day).Select(day => new UsageDay(
                        day.Key,
                        day.Sum(u => u.Count),
                        [.. day.Select(u => new UsageVariation(u.Variation, u.Count))]))]);
            })]));
    }
}
