using Microsoft.AspNetCore.Cors.Infrastructure;
using Microsoft.AspNetCore.Routing;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Options;

/// <summary>
/// AGENTS.md invariant 7: the ruleset holds salts and every rule, so no browser may read it. Exactly three routes carry
/// a CORS policy (evaluate, events and the change signal), and a fourth one fails here.
/// </summary>
public sealed class CorsTests : IDisposable
{
    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    [Fact]
    public void Exactly_evaluate_events_and_changes_carry_a_cors_policy()
    {
        var endpoints = server.Server.Services.GetRequiredService<EndpointDataSource>().Endpoints.OfType<RouteEndpoint>().ToList();

        var withCors = endpoints
            .Where(e => e.Metadata.GetMetadata<IEnableCorsAttribute>() is not null || e.Metadata.GetMetadata<ICorsPolicyMetadata>() is not null)
            .Select(e => $"{string.Join('|', e.Metadata.GetMetadata<IHttpMethodMetadata>()?.HttpMethods ?? ["*"])} {e.RoutePattern.RawText}")
            .Order()
            .ToList();

        // A walk that found nothing would pass the next line by accident.
        Assert.True(endpoints.Count > 30, $"only {endpoints.Count} endpoints found");
        Assert.Equal(["GET /sdk/v1/changes", "POST /sdk/v1/evaluate", "POST /sdk/v1/events"], withCors);
    }

    [Fact]
    public void There_is_no_default_policy_that_would_put_cors_on_every_route()
    {
        var options = server.Server.Services.GetRequiredService<IOptions<Microsoft.AspNetCore.Cors.Infrastructure.CorsOptions>>().Value;

        Assert.Null(options.GetPolicy(options.DefaultPolicyName));
    }
}
