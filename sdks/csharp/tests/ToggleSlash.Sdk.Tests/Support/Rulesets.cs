using System.Globalization;

namespace ToggleSlash.Sdk.Tests.Support;

internal static class Rulesets
{
    /// <summary>A flag that is off and serves one value (variation 0).</summary>
    public static string Flag(string key, string valueJson, int version = 1) =>
        $$"""{"key":"{{key}}","variations":[{{valueJson}}],"on":false,"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0},"salt":"s","version":{{version.ToString(CultureInfo.InvariantCulture)}}}""";

    public static string Ruleset(params string[] flags) => $$"""{"flags":[{{string.Join(',', flags)}}],"segments":[]}""";

    /// <summary>A standard backend: ruleset with a tag, 304 on a match, events accepted, no stream.</summary>
    public static Func<Recorded, Connection, CancellationToken, Task> Backend(Func<string> ruleset, Func<string> etag, Func<Recorded, Connection, CancellationToken, Task>? stream = null, Func<Recorded, Connection, CancellationToken, Task>? events = null) =>
        async (request, connection, stop) =>
        {
            switch (request.Path)
            {
                case "/sdk/v1/ruleset":
                    var tag = etag();
                    if (request.Header("If-None-Match") == tag)
                    {
                        await connection.SendAsync(304, "", ("ETag", tag));
                    }
                    else
                    {
                        await connection.SendAsync(200, ruleset(), ("ETag", tag), ("Content-Type", "application/json"));
                    }

                    break;
                case "/sdk/v1/stream" when stream is not null:
                    await stream(request, connection, stop);
                    break;
                case "/sdk/v1/events" when events is not null:
                    await events(request, connection, stop);
                    break;
                case "/sdk/v1/events":
                    await connection.SendAsync(202, "{}");
                    break;
                default:
                    await connection.SendAsync(404, "no");
                    break;
            }
        };
}
