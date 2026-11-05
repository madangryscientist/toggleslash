using System.Buffers.Binary;
using System.Security.Cryptography;
using System.Text;

namespace FeatureSlash.Evaluation;

public static class Bucketing
{
    /// <summary>Buckets run 0..99999; a rollout weight of 1000 is 1%.</summary>
    internal const int BucketCount = 100_000;

    /// <summary>
    /// Where a context lands for a flag or segment: SHA-256 of the UTF-8 bytes of
    /// <c>key.salt.contextKey</c>, the first 4 bytes as a big-endian <b>unsigned</b> integer, modulo 100000.
    /// </summary>
    public static int Bucket(string key, string salt, string contextKey)
    {
        // Encoding.UTF8 turns a lone surrogate into U+FFFD, as docs/evaluation.md requires.
        var digest = SHA256.HashData(Encoding.UTF8.GetBytes($"{key}.{salt}.{contextKey}"));
        return (int)(BinaryPrimitives.ReadUInt32BigEndian(digest) % BucketCount);
    }
}
