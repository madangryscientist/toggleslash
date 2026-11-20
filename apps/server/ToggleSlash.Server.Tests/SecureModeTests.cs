using System.Buffers.Text;
using System.Net;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using ToggleSlash.Evaluation;

namespace ToggleSlash.Server.Tests;

/// <summary>
/// Secure mode: in an environment with it on, a client-key <c>POST /sdk/v1/evaluate</c> must carry an ECDSA P-256
/// signature over the context's keys, made with the environment's signing key, whose private half the server returns
/// once and never keeps. The message format and its vectors are in docs/sdk-browser.md, "Secure mode".
/// </summary>
public sealed class SecureModeTests : IDisposable
{
    private const string Prod = "/api/v1/projects/shop/environments/prod";
    private const string Evaluate = "/sdk/v1/evaluate";

    private readonly ApiServer server = new();

    public void Dispose() => server.Dispose();

    private async Task Seed()
    {
        await server.SeedProject();
        await server.SeedEnvironment("shop", "prod");
        await server.SeedFlag("shop", "checkout", clientVisible: true);
    }

    private async Task<string> Key(string kind)
    {
        var created = await server.Post($"{Prod}/keys", new { kind, name = kind });
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("key").GetString()!;
    }

    private async Task<string> SigningKey()
    {
        var created = await server.Post($"{Prod}/signing-key");
        await ApiServer.Expect(HttpStatusCode.Created, created);
        return (await ApiServer.Json(created)).GetProperty("privateKey").GetString()!;
    }

    private async Task SecureMode(bool on) =>
        await ApiServer.Expect(HttpStatusCode.OK, await server.Patch(Prod, new { secureMode = on }));

    /// <summary>A secure-mode environment with a signing key, a client key and its private key.</summary>
    private async Task<(string Client, string Pem)> Secured()
    {
        await Seed();
        var pem = await SigningKey();
        await SecureMode(true);
        return (await Key("client"), pem);
    }

    /// <summary>Signs <paramref name="message"/> as an application server would: P-256, SHA-256, IEEE P1363, base64url with no padding.</summary>
    private static string Sign(string pem, string message)
    {
        using var key = ECDsa.Create();
        key.ImportFromPem(pem);
        var signature = key.SignData(Encoding.UTF8.GetBytes(message), HashAlgorithmName.SHA256, DSASignatureFormat.IeeeP1363FixedFieldConcatenation);
        return Base64Url.EncodeToString(signature);
    }

    private Task<HttpResponseMessage> Sdk(string key, object body) => server.Send(HttpMethod.Post, Evaluate, $"Bearer {key}", body);

    private static readonly object User = new { key = "user-1" };
    private const string UserMessage = "4:user,6:user-1,";

    private static readonly object Multi = new
    {
        kind = "multi",
        contexts = new { user = new { key = "u", attributes = new { plan = "pro" } }, org = new { key = "acme" } },
    };
    private const string MultiMessage = "3:org,4:acme,4:user,1:u,";

    // --- the message -----------------------------------------------------------------------------------------

    [Fact]
    public void The_two_vectors_are_the_bytes_the_server_verifies()
    {
        static byte[] Message(string json) => ContextSignature.Message(JsonDocument.Parse(json).RootElement)!;
        Assert.Equal(UserMessage, Encoding.UTF8.GetString(Message("""{ "key": "user-1" }""")));
        Assert.Equal(MultiMessage, Encoding.UTF8.GetString(Message(
            """{ "kind": "multi", "contexts": { "user": { "key": "u" }, "org": { "key": "acme" } } }""")));
        // A key counts bytes, not characters, and attributes are not signed.
        Assert.Equal("4:user,2:é,", Encoding.UTF8.GetString(Message("""{ "key": "é", "attributes": { "a": 1 } }""")));
        Assert.Null(ContextSignature.Message(JsonDocument.Parse("""{ "key": "" }""").RootElement));
    }

    // --- the signing key -------------------------------------------------------------------------------------

    [Fact]
    public async Task The_private_key_is_in_the_201_and_only_the_public_half_is_stored()
    {
        await Seed();
        var created = await server.Post($"{Prod}/signing-key");
        await ApiServer.Expect(HttpStatusCode.Created, created);
        var body = await ApiServer.Json(created);
        var pem = body.GetProperty("privateKey").GetString()!;
        Assert.StartsWith("-----BEGIN PRIVATE KEY-----", pem);
        Assert.Equal(2, body.EnumerateObject().Count());

        using var connection = server.OpenDatabase();
        using var command = connection.CreateCommand();
        command.CommandText = "SELECT * FROM environments WHERE key = 'prod'";
        using var reader = command.ExecuteReader();
        Assert.True(reader.Read());
        var stored = (byte[])reader["signing_public_key"];
        using var mine = ECDsa.Create();
        mine.ImportFromPem(pem);
        Assert.Equal(mine.ExportSubjectPublicKeyInfo(), stored);
        // No column holds the private half, as text or as bytes.
        var privateBytes = mine.ExportPkcs8PrivateKey();
        for (var i = 0; i < reader.FieldCount; i++)
        {
            var value = reader.GetValue(i);
            Assert.False(value is string text && text.Contains("PRIVATE KEY"), reader.GetName(i));
            Assert.False(value is byte[] bytes && bytes.SequenceEqual(privateBytes), reader.GetName(i));
        }
        // Nor does the audit trail, or any read of the environment.
        Assert.DoesNotContain(server.AuditRows(), row => row.Everything.Contains("PRIVATE KEY"));
        var read = await (await server.Get(Prod)).Content.ReadAsStringAsync();
        Assert.DoesNotContain("PRIVATE", read);
        Assert.DoesNotContain("privateKey", read);
    }

    [Fact]
    public async Task The_environment_says_whether_secure_mode_is_on_and_when_its_key_was_made()
    {
        await Seed();
        var fresh = await ApiServer.Json(await server.Get(Prod));
        Assert.False(fresh.GetProperty("secureMode").GetBoolean());
        Assert.Equal(JsonValueKind.Null, fresh.GetProperty("signingKeyCreatedAt").ValueKind);

        var created = await ApiServer.Json(await server.Post($"{Prod}/signing-key"));
        await SecureMode(true);
        var after = await ApiServer.Json(await server.Get(Prod));
        Assert.True(after.GetProperty("secureMode").GetBoolean());
        Assert.Equal(created.GetProperty("createdAt").GetDateTime(), after.GetProperty("signingKeyCreatedAt").GetDateTime());
    }

    [Fact]
    public async Task Turning_secure_mode_on_with_no_signing_key_is_a_409()
    {
        await Seed();
        await ApiServer.ExpectError(HttpStatusCode.Conflict, "conflict", await server.Patch(Prod, new { secureMode = true }));
        Assert.False((await ApiServer.Json(await server.Get(Prod))).GetProperty("secureMode").GetBoolean());
        // Off needs no key.
        await SecureMode(false);
    }

    [Fact]
    public async Task A_patch_leaves_out_what_it_does_not_name()
    {
        await Seed();
        await SigningKey();
        await SecureMode(true);

        var renamed = await server.Patch(Prod, new { name = "Production" });
        await ApiServer.Expect(HttpStatusCode.OK, renamed);
        var body = await ApiServer.Json(renamed);
        Assert.True(body.GetProperty("secureMode").GetBoolean());
        Assert.Equal("Production", body.GetProperty("name").GetString());

        await SecureMode(false);
        Assert.Equal("Production", (await ApiServer.Json(await server.Get(Prod))).GetProperty("name").GetString());
    }

    [Fact]
    public async Task Both_changes_are_in_the_audit_trail()
    {
        await Seed();
        await SigningKey();
        await SecureMode(true);

        var events = (await ApiServer.Json(await server.Get("/api/v1/projects/shop/audit?environment=prod"))).GetProperty("events");
        var mode = events[0];
        Assert.Equal("updated", mode.GetProperty("action").GetString());
        Assert.False(mode.GetProperty("before").GetProperty("secureMode").GetBoolean());
        Assert.True(mode.GetProperty("after").GetProperty("secureMode").GetBoolean());
        var key = events[1];
        Assert.Equal("updated", key.GetProperty("action").GetString());
        Assert.Equal(JsonValueKind.Null, key.GetProperty("before").GetProperty("signingKeyCreatedAt").ValueKind);
        Assert.Equal(JsonValueKind.String, key.GetProperty("after").GetProperty("signingKeyCreatedAt").ValueKind);
    }

    // --- evaluate --------------------------------------------------------------------------------------------

    [Fact]
    public async Task A_client_key_with_no_signature_is_refused()
    {
        var (client, _) = await Secured();
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "signature_required", await Sdk(client, new { context = User }));
        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "signature_required", await Sdk(client, new { context = User, signature = "" }));
    }

    [Fact]
    public async Task A_wrong_signature_is_refused()
    {
        var (client, pem) = await Secured();
        // Garbage, padding, a short one, and a valid signature over another context's key.
        foreach (var signature in new[] { "not-a-signature", Sign(pem, UserMessage) + "==", "AAAA", Sign(pem, "4:user,6:user-2,") })
        {
            await ApiServer.ExpectError(HttpStatusCode.Forbidden, "bad_signature", await Sdk(client, new { context = User, signature }));
        }
    }

    [Fact]
    public async Task A_signature_made_with_a_rotated_out_key_is_refused()
    {
        var (client, old) = await Secured();
        var signed = Sign(old, UserMessage);
        await ApiServer.Expect(HttpStatusCode.OK, await Sdk(client, new { context = User, signature = signed }));

        var rotated = await SigningKey();

        await ApiServer.ExpectError(HttpStatusCode.Forbidden, "bad_signature", await Sdk(client, new { context = User, signature = signed }));
        await ApiServer.Expect(HttpStatusCode.OK, await Sdk(client, new { context = User, signature = Sign(rotated, UserMessage) }));
    }

    [Fact]
    public async Task A_correct_signature_is_answered_for_a_single_and_a_multi_context()
    {
        var (client, pem) = await Secured();

        var single = await Sdk(client, new { context = User, signature = Sign(pem, UserMessage) });
        await ApiServer.Expect(HttpStatusCode.OK, single);
        Assert.True((await ApiServer.Json(single)).GetProperty("flags").TryGetProperty("checkout", out _));

        // Attributes are not signed: the browser may set any it likes.
        var multi = await Sdk(client, new { context = Multi, flagKey = "checkout", signature = Sign(pem, MultiMessage) });
        await ApiServer.Expect(HttpStatusCode.OK, multi);
    }

    [Fact]
    public async Task A_bad_context_is_a_400_before_any_signature_is_checked()
    {
        var (client, _) = await Secured();
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await Sdk(client, new { context = new { key = "" } }));
        await ApiServer.ExpectError(HttpStatusCode.BadRequest, "bad_request", await Sdk(client, new { flagKey = "checkout" }));
    }

    [Fact]
    public async Task A_server_key_and_diagnose_ignore_secure_mode()
    {
        await Secured();
        await ApiServer.Expect(HttpStatusCode.OK, await Sdk(await Key("server"), new { context = User }));
        await ApiServer.Expect(HttpStatusCode.OK, await server.Post($"{Prod}/evaluate", new { context = User }));
    }

    [Fact]
    public async Task With_secure_mode_off_a_signature_is_ignored()
    {
        var (client, _) = await Secured();
        await SecureMode(false);

        await ApiServer.Expect(HttpStatusCode.OK, await Sdk(client, new { context = User }));
        await ApiServer.Expect(HttpStatusCode.OK, await Sdk(client, new { context = User, signature = "anything" }));
    }
}
