using System.Security.Cryptography;
using System.Text;
using ToggleSlash.Sdk.Json;

namespace ToggleSlash.Sdk.Tests;

/// <summary>
/// <see cref="Signing.SignContext"/> (contract 14) on every entry of fixtures/signing/messages.json, read at test time,
/// and a round trip on a key the test makes. No key is in the tree.
/// </summary>
public sealed class SigningTests
{
    private static string FixtureFile()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir is not null; dir = dir.Parent)
        {
            var candidate = Path.Combine(dir.FullName, "fixtures", "signing", "messages.json");
            if (File.Exists(candidate))
            {
                return candidate;
            }
        }

        throw new FileNotFoundException("fixtures/signing/messages.json was not found above " + AppContext.BaseDirectory);
    }

    private static string Pem(ECDsa key) => key.ExportPkcs8PrivateKeyPem();

    private static byte[] FromBase64Url(string text)
    {
        var padded = text.Replace('-', '+').Replace('_', '/');
        return Convert.FromBase64String(padded + new string('=', (4 - (padded.Length % 4)) % 4));
    }

    [Fact]
    public void EveryFixtureMessageIsTheBytesSignedAndItsSignatureVerifies()
    {
        var file = JsonValue.Parse(File.ReadAllText(FixtureFile())) as JsonObject
            ?? throw new InvalidDataException("messages.json is not an object");
        var messages = file.Properties["messages"] as JsonArray ?? throw new InvalidDataException("no messages");
        using var key = ECDsa.Create(ECCurve.NamedCurves.nistP256);
        var pem = Pem(key);
        Assert.Equal(7, messages.Count);
        foreach (var raw in messages.Items)
        {
            var entry = ((JsonObject)raw).Properties;
            var name = ((JsonString)entry["name"]).Value;
            var expected = Encoding.UTF8.GetBytes(((JsonString)entry["message"]).Value);
            Assert.True(expected.AsSpan().SequenceEqual(Signing.ContextMessage(entry["context"])), name);

            var signature = Signing.SignContext(pem, entry["context"]);
            Assert.Matches("^[A-Za-z0-9_-]{86}$", signature);
            Assert.True(
                key.VerifyData(expected, FromBase64Url(signature), HashAlgorithmName.SHA256, DSASignatureFormat.IeeeP1363FixedFieldConcatenation),
                name);
        }
    }

    [Fact]
    public void ANativeContextSignsAndVerifiesWithTheKeysPublicHalf()
    {
        using var key = ECDsa.Create(ECCurve.NamedCurves.nistP256);
        using var verifier = ECDsa.Create();
        verifier.ImportSubjectPublicKeyInfo(key.ExportSubjectPublicKeyInfo(), out _);
        var context = new Dictionary<string, object?> { ["key"] = "user-1", ["plan"] = "pro" };

        var signature = Signing.SignContext(Pem(key), context);

        var message = Encoding.UTF8.GetBytes("4:user,6:user-1,");
        Assert.True(verifier.VerifyData(message, FromBase64Url(signature), HashAlgorithmName.SHA256, DSASignatureFormat.IeeeP1363FixedFieldConcatenation));
        Assert.False(verifier.VerifyData(Encoding.UTF8.GetBytes("4:user,6:user-2,"), FromBase64Url(signature), HashAlgorithmName.SHA256, DSASignatureFormat.IeeeP1363FixedFieldConcatenation));
    }

    [Fact]
    public void AKeyOnAnotherCurveIsRefused()
    {
        using var p384 = ECDsa.Create(ECCurve.NamedCurves.nistP384);
        var error = Assert.Throws<ArgumentException>(() => Signing.SignContext(Pem(p384), new Dictionary<string, object?> { ["key"] = "u" }));
        Assert.Contains("P-256", error.Message, StringComparison.Ordinal);
    }

    [Fact]
    public void AKeyThatIsNotAPrivateKeyIsRefused()
    {
        using var key = ECDsa.Create(ECCurve.NamedCurves.nistP256);
        var context = new Dictionary<string, object?> { ["key"] = "u" };
        Assert.Throws<ArgumentException>(() => Signing.SignContext("not a pem", context));
        Assert.Throws<ArgumentException>(() => Signing.SignContext(key.ExportSubjectPublicKeyInfoPem(), context));
        Assert.Throws<ArgumentNullException>(() => Signing.SignContext(null!, context));
    }

    [Fact]
    public void AnInvalidContextIsRefused()
    {
        using var key = ECDsa.Create(ECCurve.NamedCurves.nistP256);
        var error = Assert.Throws<ArgumentException>(() => Signing.SignContext(Pem(key), new Dictionary<string, object?> { ["name"] = "no key" }));
        Assert.Contains("context", error.Message, StringComparison.Ordinal);
        Assert.Throws<ArgumentException>(() => Signing.SignContext(Pem(key), null));
    }
}
