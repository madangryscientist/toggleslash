using System.Security.Claims;
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using FeatureSlash.Server.Data;
using Microsoft.AspNetCore.Authentication;
using Microsoft.AspNetCore.Authorization;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Options;

namespace FeatureSlash.Server.Auth;

/// <summary>
/// Resolves <c>Authorization: Bearer &lt;token&gt;</c> to the admin or to an SDK key's environment and kind.
/// One scheme for both, so a valid credential on the wrong route is a 403 and only a bad one is a 401.
/// Nothing here logs the token, and a failure message never quotes it.
/// </summary>
public sealed class BearerAuthenticationHandler(
    IOptionsMonitor<AuthenticationSchemeOptions> options,
    ILoggerFactory logger,
    UrlEncoder encoder,
    IConfiguration configuration,
    FeatureSlashDbContext database) : AuthenticationHandler<AuthenticationSchemeOptions>(options, logger, encoder)
{
    protected override async Task<AuthenticateResult> HandleAuthenticateAsync()
    {
        var header = Request.Headers.Authorization;
        // The middleware runs this on every request: a route with no policy must not depend on the database.
        if (header.Count == 0 || Context.GetEndpoint()?.Metadata.GetMetadata<IAuthorizeData>() is null)
        {
            return AuthenticateResult.NoResult();
        }

        if (header.ToString().Split(' ') is not [var scheme, { Length: > 0 } token] || !scheme.Equals("Bearer", StringComparison.OrdinalIgnoreCase))
        {
            return AuthenticateResult.Fail("The Authorization header is not a bearer token.");
        }

        if (IsAdminToken(token))
        {
            return Success(new Claim(ServerAuth.PrincipalClaim, ServerAuth.AdminPolicy));
        }

        var hash = ApiKeySecret.HashOf(token);
        var key = await database.ApiKeys.AsNoTracking()
            .SingleOrDefaultAsync(k => k.Hash == hash && k.RevokedAt == null, Context.RequestAborted);
        if (key is null)
        {
            return AuthenticateResult.Fail("The token is not a valid admin token or a live SDK key.");
        }

        return Success(
            new Claim(ServerAuth.PrincipalClaim, key.Kind == ApiKeyKind.Server ? ServerAuth.ServerKeyPolicy : ServerAuth.ClientKeyPolicy),
            new Claim(ServerAuth.EnvironmentClaim, key.EnvironmentId.ToString()));
    }

    protected override Task HandleChallengeAsync(AuthenticationProperties properties)
    {
        Response.Headers.WWWAuthenticate = "Bearer";
        return ApiError.Write(Context, StatusCodes.Status401Unauthorized, "unauthorized", "Send a valid token as Authorization: Bearer <token>.");
    }

    protected override Task HandleForbiddenAsync(AuthenticationProperties properties) =>
        ApiError.Write(Context, StatusCodes.Status403Forbidden, "forbidden", "This token is valid but may not use this route.");

    /// <summary>Compares hashes, so the comparison takes the same time whatever the token's length.</summary>
    private bool IsAdminToken(string token) =>
        ServerAuth.AdminToken(configuration) is { } admin
        && CryptographicOperations.FixedTimeEquals(
            SHA256.HashData(Encoding.UTF8.GetBytes(token)),
            SHA256.HashData(Encoding.UTF8.GetBytes(admin)));

    private AuthenticateResult Success(params Claim[] claims) =>
        AuthenticateResult.Success(new AuthenticationTicket(
            new ClaimsPrincipal(new ClaimsIdentity(claims, Scheme.Name)),
            Scheme.Name));
}
