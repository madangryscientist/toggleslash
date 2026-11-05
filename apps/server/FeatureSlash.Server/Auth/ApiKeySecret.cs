using System.Security.Cryptography;
using System.Text;
using FeatureSlash.Server.Data;

namespace FeatureSlash.Server.Auth;

/// <summary>
/// A freshly generated SDK key or scoped admin token. <see cref="Plaintext"/> leaves in the creation response and is not kept.
/// A class, not a record: a record's generated ToString would print the key into any log line it reached.
/// </summary>
public sealed class ApiKeySecret(string plaintext, string hash, string prefix)
{
    private const int RandomBytes = 32;
    private const int RandomCharsInPrefix = 8;

    public string Plaintext { get; } = plaintext;
    public string Hash { get; } = hash;
    public string Prefix { get; } = prefix;

    public static ApiKeySecret Generate(ApiKeyKind kind) => Generate(kind == ApiKeyKind.Server ? "fs_server_" : "fs_client_");

    public static ApiKeySecret GenerateAdminToken() => Generate("fs_admin_");

    private static ApiKeySecret Generate(string start)
    {
        var plaintext = start + Convert.ToHexStringLower(RandomNumberGenerator.GetBytes(RandomBytes));
        return new ApiKeySecret(plaintext, HashOf(plaintext), plaintext[..(start.Length + RandomCharsInPrefix)]);
    }

    /// <summary>SHA-256, lowercase hex. The key is 256 random bits, so a plain hash is enough: there is nothing to guess.</summary>
    public static string HashOf(string token) =>
        Convert.ToHexStringLower(SHA256.HashData(Encoding.UTF8.GetBytes(token)));
}
