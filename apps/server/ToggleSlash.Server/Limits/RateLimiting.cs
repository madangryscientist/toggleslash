using System.Globalization;
using System.Net;
using System.Net.Sockets;
using ToggleSlash.Server.Auth;
using Microsoft.AspNetCore.HttpOverrides;
using Microsoft.Extensions.DependencyInjection.Extensions;
using Microsoft.Net.Http.Headers;

namespace ToggleSlash.Server.Limits;

/// <summary>How the limits meet a request: the client's IP, the per-key check and the one 429.</summary>
public static class RateLimiting
{
    private const string Code = "rate_limited";

    /// <summary>Clients with no address at all (a Unix socket, say) share one bucket instead of escaping the limit.</summary>
    private const string NoAddress = "none";

    public static IServiceCollection AddRateLimits(this IServiceCollection services)
    {
        services.TryAddSingleton(TimeProvider.System);
        services.AddSingleton(provider => RateLimitSettings.From(provider.GetRequiredService<IConfiguration>()));
        services.AddSingleton<RequestLimiter>();
        return services;
    }

    /// <summary>
    /// Believes <c>X-Forwarded-For</c> from the listed proxies only. With none listed the middleware is not installed
    /// at all, because given an empty list it trusts every address. It walks the chain from the right and stops at
    /// the first address that is not listed, so a client cannot prepend its way to another IP.
    /// </summary>
    public static void UseTrustedProxies(this WebApplication app)
    {
        var proxies = RateLimitSettings.TrustedProxies(app.Configuration);
        if (proxies.Count == 0)
        {
            return;
        }
        var options = new ForwardedHeadersOptions { ForwardedHeaders = ForwardedHeaders.XForwardedFor, ForwardLimit = null };
        options.KnownIPNetworks.Clear();
        options.KnownProxies.Clear();
        foreach (var proxy in proxies)
        {
            options.KnownProxies.Add(proxy);
        }
        app.UseForwardedHeaders(options);
    }

    /// <summary>After authorization, so the key is known, and before the route reads a body.</summary>
    public static void UseKeyRateLimits(this WebApplication app) => app.Use(async (context, next) =>
    {
        if (context.GetEndpoint()?.Metadata.GetMetadata<LimitedPerKeyAttribute>() is { } limited
            && !context.RequestServices.GetRequiredService<RequestLimiter>().TryTake(limited.Limit, ServerAuth.KeyId(context.User), out var retryAfter))
        {
            await Write(context, retryAfter, "This key has sent too many requests.");
            return;
        }
        await next(context);
    });

    /// <summary>Marks a route as limited per key.</summary>
    public static RouteHandlerBuilder LimitedPerKey(this RouteHandlerBuilder route, KeyLimit limit) => route.WithMetadata(new LimitedPerKeyAttribute(limit));

    /// <summary>The 429: the one error shape, with <c>Retry-After</c> in whole seconds.</summary>
    public static Task Write(HttpContext context, long retryAfterSeconds, string message)
    {
        context.Response.Headers.RetryAfter = retryAfterSeconds.ToString(CultureInfo.InvariantCulture);
        return ApiError.Write(context, StatusCodes.Status429TooManyRequests, Code, message);
    }

    /// <summary>The address a client is counted under: IPv4 as it is, IPv6 by its /64, which is what one customer gets.</summary>
    public static string ClientAddress(HttpContext context)
    {
        if (context.Connection.RemoteIpAddress is not { } address)
        {
            return NoAddress;
        }
        // A dual-stack socket shows an IPv4 client as ::ffff:a.b.c.d, which must not be grouped as IPv6.
        if (address.IsIPv4MappedToIPv6)
        {
            address = address.MapToIPv4();
        }
        if (address.AddressFamily != AddressFamily.InterNetworkV6)
        {
            return address.ToString();
        }
        var bytes = address.GetAddressBytes();
        Array.Clear(bytes, 8, 8);
        return new IPAddress(bytes).ToString();
    }
}

/// <summary>Endpoint metadata: this route takes from the key's bucket for <see cref="Limit"/>.</summary>
public sealed class LimitedPerKeyAttribute(KeyLimit limit) : Attribute
{
    public KeyLimit Limit { get; } = limit;
}
