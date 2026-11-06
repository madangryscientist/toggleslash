using Microsoft.AspNetCore.WebUtilities;

namespace ToggleSlash.Server.Auth;

/// <summary>The one shape of every error body: <c>{"error":{"code":"...","message":"..."}}</c>.</summary>
public sealed record ApiError(ApiError.Detail Error)
{
    public sealed record Detail(string Code, string Message);

    public static IResult Result(int status, string code, string message) =>
        Results.Json(new ApiError(new Detail(code, message)), statusCode: status);

    public static Task Write(HttpContext context, int status, string code, string message)
    {
        context.Response.StatusCode = status;
        return context.Response.WriteAsJsonAsync(new ApiError(new Detail(code, message)));
    }

    /// <summary>For a status the framework set with no body: an unknown route, an unreadable request.</summary>
    public static Task WriteForStatus(HttpContext context)
    {
        var status = context.Response.StatusCode;
        var reason = ReasonPhrases.GetReasonPhrase(status);
        return Write(context, status, reason.Replace(' ', '_').ToLowerInvariant(), reason);
    }
}
