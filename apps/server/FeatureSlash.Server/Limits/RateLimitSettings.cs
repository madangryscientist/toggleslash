using System.Globalization;
using System.Net;

namespace FeatureSlash.Server.Limits;

/// <summary>The limits this server runs with. There is no off switch: a setting can only change a number.</summary>
public sealed record RateLimitSettings(int EvaluatePerMinute, int EventsPerMinute, int AuthFailuresPerMinute, int StreamsPerKey)
{
    public const string EvaluateSetting = "FEATURESLASH_RATE_LIMIT_EVALUATE_PER_MINUTE";
    public const string EventsSetting = "FEATURESLASH_RATE_LIMIT_EVENTS_PER_MINUTE";
    public const string AuthFailuresSetting = "FEATURESLASH_RATE_LIMIT_AUTH_FAILURES_PER_MINUTE";
    public const string StreamsSetting = "FEATURESLASH_RATE_LIMIT_STREAMS_PER_KEY";
    public const string TrustedProxiesSetting = "FEATURESLASH_TRUSTED_PROXIES";

    /// <summary>A few hundred bytes each, so tens of megabytes at worst.</summary>
    public int MaxTrackedIps { get; init; } = 100_000;

    public int MaxTrackedKeys { get; init; } = 10_000;

    /// <summary>Unset takes the default. Anything but a positive integer stops the server, naming the variable.</summary>
    public static RateLimitSettings From(IConfiguration configuration) => new(
        PositiveInteger(configuration, EvaluateSetting, 6000),
        PositiveInteger(configuration, EventsSetting, 600),
        PositiveInteger(configuration, AuthFailuresSetting, 30),
        PositiveInteger(configuration, StreamsSetting, 100));

    /// <summary>
    /// The proxies whose <c>X-Forwarded-For</c> is believed. A value that is not an address stops the server: ignoring
    /// it would quietly put every client behind that proxy on one IP.
    /// </summary>
    public static IReadOnlyList<IPAddress> TrustedProxies(IConfiguration configuration)
    {
        var entries = (configuration[TrustedProxiesSetting] ?? "").Split(',', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries);
        return [.. entries.Select(entry => IPAddress.TryParse(entry, out var address)
            ? address
            : throw new InvalidOperationException($"{TrustedProxiesSetting} must be a comma-separated list of IP addresses."))];
    }

    private static int PositiveInteger(IConfiguration configuration, string setting, int fallback)
    {
        if (configuration[setting] is not { } text)
        {
            return fallback;
        }
        return int.TryParse(text, NumberStyles.None, CultureInfo.InvariantCulture, out var value) && value > 0
            ? value
            : throw new InvalidOperationException($"{setting} must be a positive integer.");
    }
}
