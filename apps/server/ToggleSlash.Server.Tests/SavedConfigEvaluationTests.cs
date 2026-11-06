using System.Net;
using System.Text.Json;
using System.Text.Json.Nodes;
using ToggleSlash.Evaluation;

/// <summary>
/// What the API saves is what <c>ToggleSlash.Evaluation</c> reads: the ruleset is built from the API's own
/// answers (plus the salts, which it never returns), as an SDK read will, and evaluated.
/// </summary>
public sealed class SavedConfigEvaluationTests : IDisposable
{
    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private static string Config(string flag, string env = "prod") => $"/api/v1/projects/shop/flags/{flag}/environments/{env}";

    private async Task Save(string flag, JsonObject config, long version = 1, string env = "prod")
    {
        config["version"] = version;
        using var client = server.Server.CreateClient();
        var response = await server.Put(Config(flag, env), JsonDocument.Parse(config.ToJsonString()).RootElement);
        await ApiServer.Expect(HttpStatusCode.OK, response);
    }

    private async Task<Ruleset> Ruleset(string env = "prod")
    {
        var flags = new JsonArray();
        foreach (var flag in (await ApiServer.Json(await server.Get("/api/v1/projects/shop/flags"))).EnumerateArray())
        {
            var key = flag.GetProperty("key").GetString()!;
            var node = JsonNode.Parse((await ApiServer.Json(await server.Get(Config(key, env)))).GetRawText())!.AsObject();
            node["key"] = key;
            node["variations"] = JsonNode.Parse(flag.GetProperty("variations").GetRawText());
            node["salt"] = server.Salt("flags", "shop", key);
            flags.Add(node);
        }
        var segments = new JsonArray();
        foreach (var segment in (await ApiServer.Json(await server.Get($"/api/v1/projects/shop/environments/{env}/segments"))).EnumerateArray())
        {
            var key = segment.GetProperty("key").GetString()!;
            var node = JsonNode.Parse(segment.GetRawText())!.AsObject();
            node["salt"] = server.Salt("segments", "shop", key);
            segments.Add(node);
        }
        var root = new JsonObject { ["flags"] = flags, ["segments"] = segments };
        return ToggleSlash.Evaluation.Ruleset.Load(JsonDocument.Parse(root.ToJsonString()).RootElement);
    }

    private static EvaluationResult Evaluate(Ruleset ruleset, string flag, string key, object? attributes = null) =>
        Evaluator.Evaluate(ruleset, flag, JsonSerializer.SerializeToElement(new Dictionary<string, object?> { ["key"] = key, ["attributes"] = attributes }));

    private async Task Seed(params string[] flags)
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        foreach (var flag in flags)
        {
            await server.SeedFlag("shop", flag);
        }
    }

    [Fact]
    public async Task A_new_flag_is_off_and_serves_its_off_variation()
    {
        await Seed("dark-mode");

        var result = Evaluate(await Ruleset(), "dark-mode", "user-1");

        Assert.Equal(ReasonKind.Off, result.Reason.Kind);
        Assert.Equal(1, result.VariationIndex);
        Assert.False(result.Value.GetBoolean());
    }

    [Fact]
    public async Task A_toggle_turns_the_flag_on_and_it_falls_through()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post(Config("dark-mode") + "/toggle", new { on = true }));

        var result = Evaluate(await Ruleset(), "dark-mode", "user-1");

        Assert.Equal(ReasonKind.Fallthrough, result.Reason.Kind);
        Assert.Equal(0, result.VariationIndex);
        Assert.True(result.Value.GetBoolean());
    }

    [Fact]
    public async Task Targets_rules_and_the_fallthrough_all_apply_in_the_documented_order()
    {
        await Seed("dark-mode");
        await Save("dark-mode", JsonNode.Parse("""
            {
              "on": true,
              "offVariation": 1,
              "targets": [{ "variation": 1, "contextKeys": ["vip"] }],
              "prerequisites": [],
              "rules": [{ "clauses": [{ "attribute": "country", "op": "in", "values": ["NL"] }], "variation": 0 }],
              "fallthrough": { "variation": 1 }
            }
            """)!.AsObject());
        var ruleset = await Ruleset();

        var target = Evaluate(ruleset, "dark-mode", "vip", new { country = "NL" });
        var rule = Evaluate(ruleset, "dark-mode", "someone", new { country = "NL" });
        var fallthrough = Evaluate(ruleset, "dark-mode", "someone", new { country = "DE" });

        Assert.Equal((ReasonKind.TargetMatch, 1), (target.Reason.Kind, target.VariationIndex));
        Assert.Equal((ReasonKind.RuleMatch, 0, 0), (rule.Reason.Kind, rule.Reason.RuleIndex, rule.VariationIndex));
        Assert.Equal((ReasonKind.Fallthrough, 1), (fallthrough.Reason.Kind, fallthrough.VariationIndex));
    }

    [Fact]
    public async Task A_rollout_serves_the_bucket_the_spec_computes()
    {
        await Seed("dark-mode");
        await Save("dark-mode", JsonNode.Parse("""
            { "on": true, "offVariation": 1, "targets": [], "prerequisites": [], "rules": [],
              "fallthrough": { "rollout": { "variations": [{ "variation": 0, "weight": 50000 }, { "variation": 1, "weight": 50000 }] } } }
            """)!.AsObject());
        var ruleset = await Ruleset();

        var served = Enumerable.Range(0, 400).Select(i => Evaluate(ruleset, "dark-mode", $"user-{i}").VariationIndex!.Value).ToList();

        Assert.InRange(served.Count(v => v == 0), 140, 260);
        Assert.Equal(served, Enumerable.Range(0, 400).Select(i => Evaluate(ruleset, "dark-mode", $"user-{i}").VariationIndex!.Value));
    }

    [Fact]
    public async Task A_prerequisite_that_is_not_met_stops_the_flag()
    {
        await Seed("base", "dependent");
        await Save("dependent", JsonNode.Parse("""
            { "on": true, "offVariation": 1, "targets": [], "prerequisites": [{ "flagKey": "base", "variation": 0 }], "rules": [], "fallthrough": { "variation": 0 } }
            """)!.AsObject());

        var blocked = Evaluate(await Ruleset(), "dependent", "user-1");
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post(Config("base") + "/toggle", new { on = true }));
        var met = Evaluate(await Ruleset(), "dependent", "user-1");

        Assert.Equal((ReasonKind.PrerequisiteFailed, "base", 1), (blocked.Reason.Kind, blocked.Reason.FlagKey, blocked.VariationIndex));
        Assert.Equal((ReasonKind.Fallthrough, 0), (met.Reason.Kind, met.VariationIndex));
    }

    [Fact]
    public async Task A_segment_match_uses_the_segment_the_api_stored()
    {
        await Seed("dark-mode");
        await ApiServer.Expect(HttpStatusCode.Created, await server.Post("/api/v1/projects/shop/environments/prod/segments", new
        {
            key = "beta",
            included = new[] { "alice" },
            excluded = new[] { "mallory" },
            rules = new[] { new { clauses = new[] { new { attribute = "plan", op = "in", values = new[] { "pro" } } } } },
        }));
        await Save("dark-mode", JsonNode.Parse("""
            { "on": true, "offVariation": 1, "targets": [], "prerequisites": [],
              "rules": [{ "clauses": [{ "op": "segmentMatch", "values": ["beta"] }], "variation": 0 }],
              "fallthrough": { "variation": 1 } }
            """)!.AsObject());
        var ruleset = await Ruleset();

        Assert.Equal(0, Evaluate(ruleset, "dark-mode", "alice").VariationIndex);
        Assert.Equal(0, Evaluate(ruleset, "dark-mode", "bob", new { plan = "pro" }).VariationIndex);
        Assert.Equal(1, Evaluate(ruleset, "dark-mode", "mallory", new { plan = "pro" }).VariationIndex);
        Assert.Equal(1, Evaluate(ruleset, "dark-mode", "bob", new { plan = "free" }).VariationIndex);
    }

    [Fact]
    public async Task Everything_the_API_accepts_loads_without_a_malformed_error()
    {
        await Seed("a", "b");
        await server.SeedFlag("shop", "multi", ["x", "y", "z"]);
        await Save("a", JsonNode.Parse("""
            { "on": true, "offVariation": null, "targets": [{ "variation": 0, "contextKeys": ["k"] }],
              "prerequisites": [{ "flagKey": "b", "variation": 1 }],
              "rules": [{ "clauses": [{ "attribute": "n", "op": "gte", "values": [3], "negate": true }, { "attribute": "s", "op": "startsWith", "values": ["x"] }],
                          "rollout": { "variations": [{ "variation": 0, "weight": 1 }, { "variation": 1, "weight": 99999 }] } }],
              "fallthrough": { "rollout": { "variations": [{ "variation": 0, "weight": 0 }, { "variation": 1, "weight": 100000 }] } } }
            """)!.AsObject());
        await Save("multi", JsonNode.Parse("""
            { "on": true, "offVariation": 2, "targets": [{ "variation": 2, "contextKeys": ["k"] }], "prerequisites": [], "rules": [], "fallthrough": { "variation": 2 } }
            """)!.AsObject());
        var ruleset = await Ruleset();

        foreach (var flag in new[] { "a", "b", "multi" })
        {
            var result = Evaluate(ruleset, flag, "k", new { n = 1, s = "xyz" });
            Assert.NotEqual(ReasonKind.Error, result.Reason.Kind);
        }
        Assert.Equal(2, Evaluate(ruleset, "multi", "other").VariationIndex);
    }
}
