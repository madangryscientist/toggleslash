using System.Net;
using FeatureSlash.Server.Auth;
using FeatureSlash.Server.Limits;
using Microsoft.AspNetCore.Authorization;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Routing;
using Microsoft.Extensions.DependencyInjection;

/// <summary>Scoped admin tokens: what one may reach, what it cannot, and what is kept of it.</summary>
public sealed class AdminTokenTests : IDisposable
{
    private const string Shop = "/api/v1/projects/shop";
    private const string Tokens = "/api/v1/projects/shop/tokens";

    /// <summary>What each kind of token may do on each admin route. A new admin route fails until it is added here.</summary>
    private static readonly Dictionary<string, (bool Read, bool Write)> RouteTable = new()
    {
        ["POST /api/v1/projects"] = (false, false),
        ["GET /api/v1/projects"] = (true, true),
        ["GET /api/v1/projects/{project}"] = (true, true),
        ["GET /api/v1/projects/{project}/audit"] = (true, true),
        ["GET /api/v1/projects/{project}/environments"] = (true, true),
        ["POST /api/v1/projects/{project}/environments"] = (false, true),
        ["GET /api/v1/projects/{project}/environments/{env}"] = (true, true),
        ["POST /api/v1/projects/{project}/environments/{env}/evaluate"] = (true, true),
        ["GET /api/v1/projects/{project}/environments/{env}/flag-states"] = (true, true),
        ["GET /api/v1/projects/{project}/environments/{env}/keys"] = (true, true),
        ["POST /api/v1/projects/{project}/environments/{env}/keys"] = (false, true),
        ["DELETE /api/v1/projects/{project}/environments/{env}/keys/{id:long}"] = (false, true),
        ["GET /api/v1/projects/{project}/environments/{env}/segments"] = (true, true),
        ["POST /api/v1/projects/{project}/environments/{env}/segments"] = (false, true),
        ["GET /api/v1/projects/{project}/environments/{env}/segments/{segment}"] = (true, true),
        ["PUT /api/v1/projects/{project}/environments/{env}/segments/{segment}"] = (false, true),
        ["DELETE /api/v1/projects/{project}/environments/{env}/segments/{segment}"] = (false, true),
        ["GET /api/v1/projects/{project}/flags"] = (true, true),
        ["POST /api/v1/projects/{project}/flags"] = (false, true),
        ["GET /api/v1/projects/{project}/flags/{flag}"] = (true, true),
        ["PATCH /api/v1/projects/{project}/flags/{flag}"] = (false, true),
        ["POST /api/v1/projects/{project}/flags/{flag}/archive"] = (false, true),
        ["POST /api/v1/projects/{project}/flags/{flag}/restore"] = (false, true),
        ["GET /api/v1/projects/{project}/flags/{flag}/environments/{env}"] = (true, true),
        ["PUT /api/v1/projects/{project}/flags/{flag}/environments/{env}"] = (false, true),
        ["POST /api/v1/projects/{project}/flags/{flag}/environments/{env}/toggle"] = (false, true),
        ["GET /api/v1/projects/{project}/flags/{flag}/history"] = (true, true),
        ["GET /api/v1/projects/{project}/flags/{flag}/usage"] = (true, true),
        ["GET /api/v1/projects/{project}/tokens"] = (false, false),
        ["POST /api/v1/projects/{project}/tokens"] = (false, false),
        ["DELETE /api/v1/projects/{project}/tokens/{id:long}"] = (false, false),
    };

    public static TheoryData<string> Routes => new(RouteTable.Keys);

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private static string Bearer(string token) => $"Bearer {token}";

    private static async Task<(long Id, string Token)> Mint(ApiServer on, string project, string permission)
    {
        var created = await on.Post($"/api/v1/projects/{project}/tokens", new { name = $"{permission} token", permission, expiresInDays = 30 });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        var body = await ApiServer.Json(created);
        return (body.GetProperty("id").GetInt64(), body.GetProperty("token").GetString()!);
    }

    private Task<(long Id, string Token)> Mint(string project, string permission) => Mint(server, project, permission);

    private async Task SeedShop()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedFlag("shop", "checkout");
    }

    /// <summary>A route in the table, as a real request: names nothing that exists below the project, so nothing is changed.</summary>
    private static (HttpMethod Method, string Path, object? Body) RequestFor(string route)
    {
        var parts = route.Split(' ');
        var path = parts[1]
            .Replace("{project}", "shop")
            .Replace("{env}", "prod")
            .Replace("{flag}", "absent")
            .Replace("{segment}", "absent")
            .Replace("{id:long}", "999999");
        return (new HttpMethod(parts[0]), path, parts[0] is "POST" or "PUT" or "PATCH" ? new { } : null);
    }

    private async Task ExpectAuthorization(string route, bool allowed, string token)
    {
        var (method, path, body) = RequestFor(route);
        var response = await server.Send(method, path, Bearer(token), body);
        if (allowed)
        {
            // Past authorization: the route's own answer, such as a 400 for the empty body or a 404 for the absent flag.
            Assert.True((int)response.StatusCode < 500 && response.StatusCode is not (HttpStatusCode.Unauthorized or HttpStatusCode.Forbidden), $"{route} answered {response.StatusCode}");
        }
        else
        {
            await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", response);
        }
    }

    [Fact]
    public void The_table_states_every_admin_route_the_server_has()
    {
        var endpoints = server.Server.Services.GetRequiredService<EndpointDataSource>().Endpoints.OfType<RouteEndpoint>().ToList();
        var served = endpoints
            .Where(e => e.Metadata.GetOrderedMetadata<IAuthorizeData>().Any(d => d.Policy == ServerAuth.AdminPolicy))
            .SelectMany(e => e.Metadata.GetMetadata<HttpMethodMetadata>()!.HttpMethods.Select(method => $"{method} {e.RoutePattern.RawText!.TrimEnd('/')}"))
            .Order()
            .ToList();

        Assert.Equal(RouteTable.Keys.Order().ToList(), served);
        Assert.All(
            endpoints.Where(e => e.RoutePattern.RawText!.StartsWith("/api/", StringComparison.Ordinal)),
            e => Assert.Contains(e.Metadata.GetOrderedMetadata<IAuthorizeData>(), d => d.Policy == ServerAuth.AdminPolicy));
    }

    [Theory]
    [MemberData(nameof(Routes))]
    public async Task Each_kind_of_token_answers_as_the_table_says(string route)
    {
        await SeedShop();
        await server.SeedProject("other");

        await ExpectAuthorization(route, RouteTable[route].Read, (await Mint("shop", "read")).Token);
        await ExpectAuthorization(route, RouteTable[route].Write, (await Mint("shop", "write")).Token);
        await ExpectAuthorization(route, route == "GET /api/v1/projects", (await Mint("other", "write")).Token);
    }

    [Fact]
    public async Task Another_project_and_no_project_answer_the_same_so_a_token_cannot_probe()
    {
        await SeedShop();
        await server.SeedProject("other");
        var (_, token) = await Mint("shop", "write");

        var exists = await server.Send(HttpMethod.Get, "/api/v1/projects/other", Bearer(token));
        var absent = await server.Send(HttpMethod.Get, "/api/v1/projects/absent", Bearer(token));
        var otherCase = await server.Send(HttpMethod.Get, "/api/v1/projects/SHOP", Bearer(token));

        await ApiServer.Expect(HttpStatusCode.Forbidden, exists);
        await ApiServer.Expect(HttpStatusCode.Forbidden, absent);
        await ApiServer.Expect(HttpStatusCode.Forbidden, otherCase);
        Assert.Equal(await exists.Content.ReadAsStringAsync(), await absent.Content.ReadAsStringAsync());
    }

    [Fact]
    public async Task A_read_token_is_refused_writes_and_may_diagnose()
    {
        await SeedShop();
        var (_, token) = await Mint("shop", "read");

        await ApiServer.ExpectError(
            HttpStatusCode.Forbidden,
            "forbidden",
            await server.Send(HttpMethod.Post, $"{Shop}/flags/checkout/environments/prod/toggle", Bearer(token), new { on = true }));
        var diagnosed = await server.Send(HttpMethod.Post, $"{Shop}/environments/prod/evaluate", Bearer(token), new { context = new { key = "u" }, flagKey = "checkout" });

        await ApiServer.Expect(HttpStatusCode.OK, diagnosed);
        Assert.DoesNotContain(server.AuditRows(), r => r.Actor != "admin");
    }

    [Fact]
    public async Task A_write_is_recorded_as_the_token_and_a_root_write_as_admin()
    {
        await SeedShop();
        var (id, token) = await Mint("shop", "write");

        await ApiServer.Expect(
            HttpStatusCode.OK,
            await server.Send(HttpMethod.Post, $"{Shop}/flags/checkout/environments/prod/toggle", Bearer(token), new { on = true }));
        Assert.Equal($"token:{id}", server.AuditRows()[^1].Actor);
        Assert.True(server.Configs("checkout").Single().On);

        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Shop}/flags/checkout/environments/prod/toggle", new { on = false }));
        Assert.Equal("admin", server.AuditRows()[^1].Actor);
    }

    private static void Expire(ApiServer on, long id)
    {
        using var connection = on.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "UPDATE admin_tokens SET expires_at = '2020-01-01 00:00:00' WHERE id = $id";
        command.Parameters.AddWithValue("$id", id);
        Assert.Equal(1, command.ExecuteNonQuery());
    }

    [Fact]
    public async Task Expired_revoked_and_unknown_tokens_get_the_same_401()
    {
        await SeedShop();
        var expired = await Mint("shop", "write");
        var revoked = await Mint("shop", "write");
        Expire(server, expired.Id);
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Tokens}/{revoked.Id}"));

        var unknown = await server.Send(HttpMethod.Get, Shop, Bearer("fs_admin_0000000000000000000000000000000000000000000000000000000000000000"));
        await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", unknown);
        var same = await unknown.Content.ReadAsStringAsync();
        foreach (var dead in new[] { expired.Token, revoked.Token })
        {
            var response = await server.Send(HttpMethod.Get, Shop, Bearer(dead));
            await ApiServer.ExpectError(HttpStatusCode.Unauthorized, "unauthorized", response);
            Assert.Equal(same, await response.Content.ReadAsStringAsync());
        }
    }

    [Theory]
    [InlineData(true)]
    [InlineData(false)]
    public async Task A_dead_token_counts_towards_the_ip_lockout(bool expire)
    {
        using var limited = ApiServer.With(builder => builder.UseSetting(RateLimitSettings.AuthFailuresSetting, "2"));
        await limited.SeedProject();
        var (id, token) = await Mint(limited, "shop", "write");
        if (expire)
        {
            Expire(limited, id);
        }
        else
        {
            await ApiServer.Expect(HttpStatusCode.NoContent, await limited.Delete($"{Tokens}/{id}"));
        }

        await ApiServer.Expect(HttpStatusCode.Unauthorized, await limited.Send(HttpMethod.Get, Shop, Bearer(token)));
        await ApiServer.Expect(HttpStatusCode.Unauthorized, await limited.Send(HttpMethod.Get, Shop, Bearer(token)));
        await ApiServer.ExpectError(HttpStatusCode.TooManyRequests, "rate_limited", await limited.Get(Shop));
    }

    public static TheoryData<string, string> SdkRoutes => new()
    {
        { "GET", "/sdk/v1/ruleset" },
        { "GET", "/sdk/v1/stream" },
        { "POST", "/sdk/v1/evaluate" },
        { "POST", "/sdk/v1/events" },
    };

    [Theory]
    [MemberData(nameof(SdkRoutes))]
    public async Task A_scoped_token_is_403_on_the_sdk_routes(string method, string path)
    {
        await SeedShop();
        var (_, token) = await Mint("shop", "write");

        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "forbidden", await server.Send(new HttpMethod(method), path, Bearer(token), method == "POST" ? new { } : null));
    }

    [Fact]
    public async Task The_project_list_holds_only_the_tokens_project()
    {
        await SeedShop();
        await server.SeedProject("other");
        var (_, token) = await Mint("shop", "read");

        var mine = await ApiServer.Json(await server.Send(HttpMethod.Get, "/api/v1/projects", Bearer(token)));
        var all = await ApiServer.Json(await server.Get("/api/v1/projects"));

        Assert.Equal(["shop"], mine.EnumerateArray().Select(p => p.GetProperty("key").GetString()));
        Assert.Equal(2, all.GetArrayLength());
    }

    public static TheoryData<string> BadBodies => new()
    {
        """{"name":"n","permission":"read","expiresInDays":0}""",
        """{"name":"n","permission":"read","expiresInDays":366}""",
        """{"name":"n","permission":"read"}""",
        """{"name":"n","permission":"read","expiresInDays":1.5}""",
        """{"name":"n","permission":"admin","expiresInDays":30}""",
    };

    [Theory]
    [MemberData(nameof(BadBodies))]
    public async Task A_bad_create_is_400_and_stores_nothing(string body)
    {
        await server.SeedProject();
        using var client = server.Server.CreateClient();
        using var request = new HttpRequestMessage(HttpMethod.Post, Tokens)
        {
            Content = new StringContent(body, System.Text.Encoding.UTF8, "application/json"),
        };
        request.Headers.TryAddWithoutValidation("Authorization", Bearer(ApiServer.AdminToken));

        await ApiServer.Expect(HttpStatusCode.BadRequest, await client.SendAsync(request));

        Assert.Equal(0, (await ApiServer.Json(await server.Get(Tokens))).GetArrayLength());
    }

    [Theory]
    [InlineData(1)]
    [InlineData(365)]
    public async Task The_ends_of_the_range_are_accepted_and_set_the_expiry(int days)
    {
        await server.SeedProject();

        var created = await server.Post(Tokens, new { name = "ci", permission = "read", expiresInDays = days });

        await ApiServer.Expect(HttpStatusCode.Created, created);
        var body = await ApiServer.Json(created);
        Assert.Equal(TimeSpan.FromDays(days), body.GetProperty("expiresAt").GetDateTime() - body.GetProperty("createdAt").GetDateTime());
    }

    [Fact]
    public async Task The_plain_token_is_in_the_create_response_and_nowhere_else()
    {
        await server.SeedProject();

        var created = await ApiServer.Json(await server.Post(Tokens, new { name = "ci", permission = "write", expiresInDays = 30 }));

        Assert.Equal(
            new[] { "createdAt", "expiresAt", "id", "name", "permission", "prefix", "revokedAt", "token" },
            created.EnumerateObject().Select(p => p.Name).Order(StringComparer.Ordinal));
        var token = created.GetProperty("token").GetString()!;
        Assert.Matches("^fs_admin_[0-9a-f]{64}$", token);
        var hash = ApiKeySecret.HashOf(token);
        Assert.StartsWith(created.GetProperty("prefix").GetString()!, token);

        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Tokens}/{created.GetProperty("id").GetInt64()}"));

        var list = await (await server.Get(Tokens)).Content.ReadAsStringAsync();
        Assert.DoesNotContain(token, list);
        Assert.DoesNotContain(hash, list);
        Assert.DoesNotContain("\"hash\"", list);

        var events = server.AuditRows().Where(r => r.EntityType == "admin_token").ToList();
        Assert.Equal(["created", "revoked"], events.Select(r => r.Action));
        var audit = await (await server.Get($"{Shop}/audit")).Content.ReadAsStringAsync();
        foreach (var text in events.Select(r => r.Everything).Append(audit))
        {
            Assert.DoesNotContain(token, text);
            Assert.DoesNotContain(hash, text);
        }

        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT * FROM admin_tokens";
        using var reader = command.ExecuteReader();
        Assert.True(reader.Read());
        var columns = Enumerable.Range(0, reader.FieldCount).Select(i => reader.IsDBNull(i) ? "" : reader.GetValue(i).ToString()).ToList();
        Assert.DoesNotContain(columns, column => column!.Contains(token, StringComparison.Ordinal));
        Assert.Contains(hash, columns);
        Assert.False(reader.Read());
    }

    [Fact]
    public async Task The_list_shows_expired_and_revoked_tokens()
    {
        await server.SeedProject();
        var expired = await Mint("shop", "read");
        var revoked = await Mint("shop", "read");
        await Mint("shop", "write");
        Expire(server, expired.Id);
        await server.Delete($"{Tokens}/{revoked.Id}");

        var list = await ApiServer.Json(await server.Get(Tokens));

        Assert.Equal(3, list.GetArrayLength());
    }

    [Fact]
    public async Task Revoking_twice_keeps_the_first_time_and_another_projects_token_is_404()
    {
        await server.SeedProject();
        await server.SeedProject("other");
        var (id, _) = await Mint("shop", "read");

        await ApiServer.ExpectError(HttpStatusCode.NotFound, "not_found", await server.Delete($"/api/v1/projects/other/tokens/{id}"));

        async Task<string> RevokedAt() => (await ApiServer.Json(await server.Get(Tokens)))[0].GetProperty("revokedAt").GetRawText();
        Assert.Equal("null", await RevokedAt());
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Tokens}/{id}"));
        var first = await RevokedAt();
        Assert.NotEqual("null", first);
        await Task.Delay(5);
        await ApiServer.Expect(HttpStatusCode.NoContent, await server.Delete($"{Tokens}/{id}"));
        Assert.Equal(first, await RevokedAt());
    }

    [Fact]
    public async Task With_the_admin_token_unset_a_live_scoped_token_gets_503()
    {
        using var database = new TempDatabase();
        string token;
        using (var on = new ServerOn(database.Path, builder => builder.UseSetting(ServerAuth.AdminTokenSetting, ApiServer.AdminToken)))
        {
            using var admin = on.CreateClient();
            admin.DefaultRequestHeaders.Authorization = new("Bearer", ApiServer.AdminToken);
            await ApiServer.Expect(HttpStatusCode.Created, await admin.PostAsync("/api/v1/projects", System.Net.Http.Json.JsonContent.Create(new { key = "shop", name = "Shop" })));
            var created = await admin.PostAsync(Tokens, System.Net.Http.Json.JsonContent.Create(new { name = "ci", permission = "write", expiresInDays = 30 }));
            token = (await ApiServer.Json(created)).GetProperty("token").GetString()!;
            await ApiServer.Expect(HttpStatusCode.OK, await SendWith(on, token));
        }

        using var off = new ServerOn(database.Path, builder => builder.UseSetting(ServerAuth.AdminTokenSetting, ""));
        await ApiServer.ExpectError(HttpStatusCode.ServiceUnavailable, "admin_token_unset", await SendWith(off, token));

        static async Task<HttpResponseMessage> SendWith(ServerOn on, string bearer)
        {
            using var client = on.CreateClient();
            client.DefaultRequestHeaders.Authorization = new("Bearer", bearer);
            return await client.GetAsync(Shop);
        }
    }
}
