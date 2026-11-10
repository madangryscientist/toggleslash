using System.Buffers.Binary;
using System.Security.Cryptography;
using System.Text;

namespace ToggleSlash.Sdk.Evaluation;

internal static class Bucketing
{
    /// <summary>Buckets run 0..99999; a rollout weight of 1000 is 1%.</summary>
    internal const int BucketCount = 100_000;

    /// <summary>
    /// SHA-256 of the UTF-8 bytes of <c>key.salt.contextKey</c>, the first 4 bytes as a big-endian
    /// <b>unsigned</b> integer.
    /// </summary>
    public static uint Hash(string key, string salt, string contextKey)
    {
        // Encoding.UTF8 turns a lone surrogate into U+FFFD, as docs/evaluation.md requires.
        var digest = SHA256.HashData(Encoding.UTF8.GetBytes($"{key}.{salt}.{contextKey}"));
        return BinaryPrimitives.ReadUInt32BigEndian(digest);
    }

    /// <summary>Where a context lands for a flag or segment: <see cref="Hash"/> modulo 100000.</summary>
    public static int Bucket(string key, string salt, string contextKey) =>
        (int)(Hash(key, salt, contextKey) % BucketCount);
}
