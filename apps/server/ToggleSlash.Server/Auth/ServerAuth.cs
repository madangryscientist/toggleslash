using System.Security.Claims;
using ToggleSlash.Server.Data;
using Microsoft.AspNetCore.Authentication;
using Microsoft.AspNetCore.Authorization;
using Microsoft.AspNetCore.Authorization.Policy;

namespace ToggleSlash.Server.Auth;

public static class ServerAuth
{
    public const string AdminTokenSetting = "TOGGLESLASH_ADMIN_TOKEN";

    public const string AdminPolicy = "admin";
    public const string ServerKeyPolicy = "server-key";
    public const string ClientKeyPolicy = "client-key";

    /// <summary>Either kind of SDK key, and nothing else: not the admin token.</summary>
    public const string SdkKeyPolicy = "sdk-key";

    /// <summary>Who the bearer is: one of the three credential policy names, or <see cref="ScopedTokenPrincipal"/>.</summary>
    public const string PrincipalClaim = "toggleslash:principal";

    /// <summary>The environment an SDK key belongs to: <c>environments.id</c>.</summary>
    public const string EnvironmentClaim = "toggleslash:environment";

    /// <summary>The SDK key itself: <c>api_keys.id</c>, for a stream to ask whether it is still live.</summary>
    public const string KeyClaim = "toggleslash:key";

    /// <summary>A scoped admin token. Not a policy name: it reaches admin routes only through <see cref="MayUseAdminRoute"/>.</summary>
    public const string ScopedTokenPrincipal = "scoped-token";

    /// <summary>The scoped token itself: <c>admin_tokens.id</c>.</summary>
    public const string TokenClaim = "toggleslash:token";

    /// <summary>The key of the one project a scoped token reaches.</summary>
    public const string TokenProjectClaim = "toggleslash:token-project";

    /// <summary>A scoped token's permission: <see cref="ReadPermission"/> or <see cref="WritePermission"/>.</summary>
    public const string TokenPermissionClaim = "toggleslash:token-permission";

    public const string ReadPermission = "read";
    public const string WritePermission = "write";

    private const string ProjectRouteValue = "project";

    private const string Scheme = "bearer";

    /// <summary>The environment an authenticated SDK key belongs to.</summary>
    public static long EnvironmentId(ClaimsPrincipal user) =>
        long.Parse(user.FindFirst(EnvironmentClaim)!.Value, System.Globalization.CultureInfo.InvariantCulture);

    /// <summary>The authenticated SDK key's id.</summary>
    public static long KeyId(ClaimsPrincipal user) =>
        long.Parse(user.FindFirst(KeyClaim)!.Value, System.Globalization.CultureInfo.InvariantCulture);

    /// <summary>The project a scoped token is limited to; null for any other credential.</summary>
    public static string? TokenProject(ClaimsPrincipal user) => user.FindFirst(TokenProjectClaim)?.Value;

    /// <summary>Null when the variable is unset or blank. The server never makes one up.</summary>
    public static string? AdminToken(IConfiguration configuration) =>
        string.IsNullOrWhiteSpace(configuration[AdminTokenSetting]) ? null : configuration[AdminTokenSetting];

    /// <summary>This is an admin route, and no admin token is configured: the request gets a 503 whatever it sent.</summary>
    public static bool AdminIsOffFor(IConfiguration configuration, HttpContext context) =>
        AdminToken(configuration) is null
        && context.GetEndpoint()?.Metadata.GetOrderedMetadata<IAuthorizeData>().Any(d => d.Policy == AdminPolicy) == true;

    public static IServiceCollection AddServerAuth(this IServiceCollection services)
    {
        services.AddAuthentication(Scheme)
            .AddScheme<AuthenticationSchemeOptions, BearerAuthenticationHandler>(Scheme, null);
        services.AddSingleton<IAuthorizationMiddlewareResultHandler, AdminUnsetResultHandler>();
        services.AddAuthorizationBuilder()
            .AddPolicy(AdminPolicy, policy => policy.RequireAssertion(context => MayUseAdminRoute(context.User, context.Resource as HttpContext)))
            .AddPolicy(ServerKeyPolicy, policy => policy.RequireClaim(PrincipalClaim, ServerKeyPolicy))
            .AddPolicy(ClientKeyPolicy, policy => policy.RequireClaim(PrincipalClaim, ClientKeyPolicy))
            .AddPolicy(SdkKeyPolicy, policy => policy.RequireClaim(PrincipalClaim, ServerKeyPolicy, ClientKeyPolicy));
        return services;
    }

    public static string PermissionWord(AdminTokenPermission permission) =>
        permission == AdminTokenPermission.Write ? WritePermission : ReadPermission;

    /// <summary>
    /// The one place that says what may use an admin route. The root token may use all of them. A scoped token needs
    /// its own project in the route, and a route with no <c>{project}</c> is closed to it unless it is marked
    /// <see cref="OwnProjectListRoute"/>. A read token also needs a GET, a HEAD or a <see cref="ReadOnlyRoute"/>.
    /// A <see cref="RootOnlyRoute"/> is closed to every scoped token. A credential or a permission it does not know is a
    /// no. A new route under <c>{project}</c> is open by these rules until marked otherwise; the route table in
    /// AdminTokenTests is what makes someone decide.
    /// </summary>
    private static bool MayUseAdminRoute(ClaimsPrincipal user, HttpContext? http)
    {
        var principal = user.FindFirst(PrincipalClaim)?.Value;
        if (principal == AdminPolicy)
        {
            return true;
        }
        if (principal != ScopedTokenPrincipal || http?.GetEndpoint()?.Metadata is not { } metadata || metadata.GetMetadata<RootOnlyRoute>() is not null)
        {
            return false;
        }
        if (http.Request.RouteValues[ProjectRouteValue] is not string project)
        {
            return metadata.GetMetadata<OwnProjectListRoute>() is not null;
        }
        if (!string.Equals(project, TokenProject(user), StringComparison.Ordinal))
        {
            return false;
        }
        return user.FindFirst(TokenPermissionClaim)?.Value switch
        {
            WritePermission => true,
            ReadPermission => HttpMethods.IsGet(http.Request.Method) || HttpMethods.IsHead(http.Request.Method) || metadata.GetMetadata<ReadOnlyRoute>() is not null,
            _ => false,
        };
    }

    /// <summary>With no admin token configured, every admin route is a 503, whatever the request sent.</summary>
    private sealed class AdminUnsetResultHandler(IConfiguration configuration) : IAuthorizationMiddlewareResultHandler
    {
        private readonly AuthorizationMiddlewareResultHandler standard = new();

        public Task HandleAsync(RequestDelegate next, HttpContext context, AuthorizationPolicy policy, PolicyAuthorizationResult authorizeResult) =>
            AdminIsOffFor(configuration, context)
                ? ApiError.Write(context, StatusCodes.Status503ServiceUnavailable, "admin_token_unset", $"The admin API is off: {AdminTokenSetting} is not set on the server.")
                : standard.HandleAsync(next, context, policy, authorizeResult);
    }
}

/// <summary>Endpoint metadata: an admin route only the root token may use.</summary>
public sealed class RootOnlyRoute;

/// <summary>Endpoint metadata: an admin route that is not a GET and changes and records nothing, so a read token may use it.</summary>
public sealed class ReadOnlyRoute;

/// <summary>Endpoint metadata: an admin route with no <c>{project}</c> that shows a scoped token its own project and no other.</summary>
public sealed class OwnProjectListRoute;
