using System.Globalization;
using System.Security.Cryptography;
using System.Text;
using ToggleSlash.Sdk.Evaluation;

namespace ToggleSlash.Sdk;

/// <summary>
/// Secure-mode signing (contract section 14): the signature an application's server hands the browser with a context,
/// made with the environment's signing key. It needs no client and does no I/O.
/// </summary>
public static class Signing
{
    /// <summary>The OID of the P-256 curve (secp256r1, prime256v1).</summary>
    private const string P256Oid = "1.2.840.10045.3.1.7";

    /// <summary>
    /// Signs <paramref name="context"/> for secure mode with <paramref name="privateKeyPem"/>, the PKCS#8 PEM that
    /// <c>POST .../environments/{env}/signing-key</c> answered once. The answer is the ECDSA P-256 SHA-256 signature as
    /// the 64-byte IEEE P1363 <c>r || s</c>, base64url with no padding (86 characters), over the bytes
    /// <c>docs/sdk-browser.md</c>, "Secure mode", defines. The context is read as the accessors read it.
    /// </summary>
    /// <remarks>
    /// Unlike the rest of the SDK this throws: an <see cref="ArgumentException"/> for a context that is not valid or a
    /// key that is not an ECDSA P-256 private key, and an <see cref="ArgumentNullException"/> for a null key. Both
    /// are mistakes at the call site, not conditions at run time.
    /// </remarks>
    /// <param name="privateKeyPem">The environment's signing key, a PKCS#8 PEM.</param>
    /// <param name="context">The context, as a <see cref="Json.JsonValue"/> or a native value.</param>
    /// <returns>The signature, 86 base64url characters.</returns>
    public static string SignContext(string privateKeyPem, object? context)
    {
        ArgumentNullException.ThrowIfNull(privateKeyPem);
        var message = ContextMessage(context);
        using var key = ECDsa.Create();
        try
        {
            key.ImportFromPem(privateKeyPem);
        }
        catch (Exception error) when (error is CryptographicException or ArgumentException)
        {
            throw new ArgumentException("SignContext: the key is not an ECDSA P-256 private key", nameof(privateKeyPem), error);
        }

        if (!IsP256(key))
        {
            throw new ArgumentException("SignContext: the key is not an ECDSA P-256 private key", nameof(privateKeyPem));
        }

        byte[] signature;
        try
        {
            signature = key.SignData(message, HashAlgorithmName.SHA256, DSASignatureFormat.IeeeP1363FixedFieldConcatenation);
        }
        catch (CryptographicException error)
        {
            // A PEM that holds only a public key imports and cannot sign.
            throw new ArgumentException("SignContext: the key is not an ECDSA P-256 private key", nameof(privateKeyPem), error);
        }

        return Convert.ToBase64String(signature).TrimEnd('=').Replace('+', '-').Replace('/', '_');
    }

    /// <summary>
    /// The bytes a signature covers: the context normalised as the evaluator does it, keys only, the kinds in byte
    /// order, each kind and then its key as a netstring (<c>&lt;UTF-8 byte length&gt;:&lt;UTF-8 bytes&gt;,</c>).
    /// </summary>
    internal static byte[] ContextMessage(object? context)
    {
        if (ContextKinds.Normalise(ContextConverter.Convert(context)) is not { } contexts)
        {
            throw new ArgumentException("SignContext: the context is not valid; see docs/evaluation.md", nameof(context));
        }

        using var bytes = new MemoryStream();
        // A kind is ASCII, so ordinal order of the string is byte order.
        foreach (var (kind, entry) in contexts.OrderBy(pair => pair.Key, StringComparer.Ordinal))
        {
            Netstring(bytes, kind);
            Netstring(bytes, entry.Key);
        }

        return bytes.ToArray();
    }

    private static void Netstring(MemoryStream into, string text)
    {
        var utf8 = Encoding.UTF8.GetBytes(text);
        into.Write(Encoding.ASCII.GetBytes(utf8.Length.ToString(CultureInfo.InvariantCulture) + ":"));
        into.Write(utf8);
        into.WriteByte((byte)',');
    }

    /// <summary>A parse alone also reads a P-384 or P-521 key, so the curve is checked here.</summary>
    private static bool IsP256(ECDsa key)
    {
        var oid = key.ExportParameters(false).Curve.Oid;
        return oid.Value == P256Oid || oid.FriendlyName is "nistP256" or "ECDSA_P256" or "secp256r1" or "prime256v1";
    }
}
