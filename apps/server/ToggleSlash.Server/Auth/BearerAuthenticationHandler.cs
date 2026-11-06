using System.Security.Claims;
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using ToggleSlash.Server.Audit;
using ToggleSlash.Server.Data;
using ToggleSlash.Server.Limits;
using Microsoft.AspNetCore.Authentication;
using Microsoft.AspNetCore.Authorization;
using Microsoft.EntityFrameworkCore;
using Microsoft.Extensions.Options;

namespace ToggleSlash.Server.Auth;

/// <summary>
/// Resolves <c>Authorization: Bearer &lt;token&gt;</c> to the admin, to an SDK key's environment and kind, or to a
/// scoped admin token's project and permission. One scheme for all of them, so a valid credential on the wrong route is a 403 and only a bad one is a 401.
/// Nothing here logs the token, and a failure message never quotes it.
/// </summary>
public sealed class BearerAuthenticationHandler(
    IOptionsMonitor<AuthenticationSchemeOptions> options,
    ILoggerFactory logger,
    UrlEncoder encoder,
    IConfiguration configuration,
    ToggleSlashDbContext database,
    RequestLimiter limiter) : AuthenticationHandler<AuthenticationSchemeOptions>(options, logger, encoder)
{
    /// <summary>Set when this request's IP is locked out: the challenge is then a 429, not a 401.</summary>
    private long? lockedOutForSeconds;

    protected override async Task<AuthenticateResult> HandleAuthenticateAsync()
    {
        var header = Request.Headers.Authorization;
        // The middleware runs this on every request: a route with no policy must not depend on the database.
        if (header.Count == 0 || Context.GetEndpoint()?.Metadata.GetMetadata<IAuthorizeData>() is null)
        {
            return AuthenticateResult.NoResult();
        }

        // Answered 503 whatever was sent (see ServerAuth), so nothing is checked and nothing counts as a failure.
        if (ServerAuth.AdminIsOffFor(configuration, Context))
        {
            return AuthenticateResult.NoResult();
        }

        // Before any check, so that a right guess from a locked-out IP looks the same as a wrong one.
        var ip = RateLimiting.ClientAddress(Context);
        if (limiter.IsLockedOut(ip, out var retryAfter))
        {
            lockedOutForSeconds = retryAfter;
            return AuthenticateResult.Fail("This address has sent too many wrong credentials.");
        }

        if (header.ToString().Split(' ') is not [var scheme, { Length: > 0 } token] || !scheme.Equals("Bearer", StringComparison.OrdinalIgnoreCase))
        {
            limiter.RecordAuthFailure(ip);
            return AuthenticateResult.Fail("The Authorization header is not a bearer token.");
        }

        if (IsAdminToken(token))
        {
            return Success(new Claim(ServerAuth.PrincipalClaim, ServerAuth.AdminPolicy));
        }

        var hash = ApiKeySecret.HashOf(token);
        var key = await database.ApiKeys.AsNoTracking()
            .SingleOrDefaultAsync(k => k.Hash == hash && k.RevokedAt == null, Context.RequestAborted);
        if (key is not null)
        {
            return Success(
                new Claim(ServerAuth.PrincipalClaim, key.Kind == ApiKeyKind.Server ? ServerAuth.ServerKeyPolicy : ServerAuth.ClientKeyPolicy),
                new Claim(ServerAuth.EnvironmentClaim, key.EnvironmentId.ToString()),
                new Claim(ServerAuth.KeyClaim, key.Id.ToString()));
        }

        var scoped = await database.AdminTokens.AsNoTracking()
            .Where(t => t.Hash == hash)
            .Join(database.Projects, t => t.ProjectId, p => p.Id, (t, p) => new { t.Id, t.Permission, t.ExpiresAt, t.RevokedAt, Project = p.Key })
            .SingleOrDefaultAsync(Context.RequestAborted);
        if (scoped is null || scoped.RevokedAt is not null || scoped.ExpiresAt <= DateTime.UtcNow)
        {
            limiter.RecordAuthFailure(ip);
            return AuthenticateResult.Fail("The token is not a valid admin token or a live SDK key.");
        }

        database.AuditActor = AuditLog.ScopedToken(scoped.Id);
        return Success(
            new Claim(ServerAuth.PrincipalClaim, ServerAuth.ScopedTokenPrincipal),
            new Claim(ServerAuth.TokenClaim, scoped.Id.ToString()),
            new Claim(ServerAuth.TokenProjectClaim, scoped.Project),
            new Claim(ServerAuth.TokenPermissionClaim, ServerAuth.PermissionWord(scoped.Permission)));
    }

    protected override Task HandleChallengeAsync(AuthenticationProperties properties)
    {
        if (lockedOutForSeconds is { } seconds)
        {
            return RateLimiting.Write(Context, seconds, "This address has sent too many wrong credentials.");
        }
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
