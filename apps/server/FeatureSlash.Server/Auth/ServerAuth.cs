using Microsoft.AspNetCore.Authentication;
using Microsoft.AspNetCore.Authorization;
using Microsoft.AspNetCore.Authorization.Policy;

namespace FeatureSlash.Server.Auth;

public static class ServerAuth
{
    public const string AdminTokenSetting = "FEATURESLASH_ADMIN_TOKEN";

    public const string AdminPolicy = "admin";
    public const string ServerKeyPolicy = "server-key";
    public const string ClientKeyPolicy = "client-key";

    /// <summary>Either kind of SDK key, and nothing else: not the admin token.</summary>
    public const string SdkKeyPolicy = "sdk-key";

    /// <summary>Who the bearer is: one of the three policy names.</summary>
    public const string PrincipalClaim = "featureslash:principal";

    /// <summary>The environment an SDK key belongs to: <c>environments.id</c>.</summary>
    public const string EnvironmentClaim = "featureslash:environment";

    private const string Scheme = "bearer";

    /// <summary>The environment an authenticated SDK key belongs to.</summary>
    public static long EnvironmentId(System.Security.Claims.ClaimsPrincipal user) =>
        long.Parse(user.FindFirst(EnvironmentClaim)!.Value, System.Globalization.CultureInfo.InvariantCulture);

    /// <summary>Null when the variable is unset or blank. The server never makes one up.</summary>
    public static string? AdminToken(IConfiguration configuration) =>
        string.IsNullOrWhiteSpace(configuration[AdminTokenSetting]) ? null : configuration[AdminTokenSetting];

    public static IServiceCollection AddServerAuth(this IServiceCollection services)
    {
        services.AddAuthentication(Scheme)
            .AddScheme<AuthenticationSchemeOptions, BearerAuthenticationHandler>(Scheme, null);
        services.AddSingleton<IAuthorizationMiddlewareResultHandler, AdminUnsetResultHandler>();
        services.AddAuthorizationBuilder()
            .AddPolicy(AdminPolicy, policy => policy.RequireClaim(PrincipalClaim, AdminPolicy))
            .AddPolicy(ServerKeyPolicy, policy => policy.RequireClaim(PrincipalClaim, ServerKeyPolicy))
            .AddPolicy(ClientKeyPolicy, policy => policy.RequireClaim(PrincipalClaim, ClientKeyPolicy))
            .AddPolicy(SdkKeyPolicy, policy => policy.RequireClaim(PrincipalClaim, ServerKeyPolicy, ClientKeyPolicy));
        return services;
    }

    /// <summary>With no admin token configured, every admin route is a 503, whatever the request sent.</summary>
    private sealed class AdminUnsetResultHandler(IConfiguration configuration) : IAuthorizationMiddlewareResultHandler
    {
        private readonly AuthorizationMiddlewareResultHandler standard = new();

        public Task HandleAsync(RequestDelegate next, HttpContext context, AuthorizationPolicy policy, PolicyAuthorizationResult authorizeResult) =>
            AdminToken(configuration) is null && context.GetEndpoint()?.Metadata.GetOrderedMetadata<IAuthorizeData>().Any(d => d.Policy == AdminPolicy) == true
                ? ApiError.Write(context, StatusCodes.Status503ServiceUnavailable, "admin_token_unset", $"The admin API is off: {AdminTokenSetting} is not set on the server.")
                : standard.HandleAsync(next, context, policy, authorizeResult);
    }
}
