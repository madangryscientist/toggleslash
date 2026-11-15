package toggleslash

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"strconv"

	"github.com/madangryscientist/toggleslash/sdks/go/eval"
	"github.com/madangryscientist/toggleslash/sdks/go/jsonval"
)

// SignContext signs a context for secure mode (docs/sdk-contract.md section 14)
// with an environment's signing key, the PKCS#8 PEM that
// POST .../environments/{env}/signing-key answered once. The answer is the
// 64-byte IEEE P1363 r || s of an ECDSA P-256 SHA-256 signature, base64url with
// no padding (86 characters); the browser SDK sends it with every evaluate for
// that context. It needs no Client.
//
// Unlike the rest of the SDK it returns an error: for a key that is not an
// ECDSA P-256 private key in a PKCS#8 PEM, and for a context that is not valid.
// Both are mistakes at the call site, not conditions at run time.
func SignContext(privateKeyPEM string, context Context) (string, error) {
	message, err := contextMessage(context)
	if err != nil {
		return "", err
	}
	key, err := signingKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(message)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

// contextMessage answers the bytes a secure-mode signature covers
// (docs/sdk-browser.md, "Secure mode"): the normalised context's kinds in byte
// order, each kind and then its key as a netstring, keys only.
func contextMessage(context Context) ([]byte, error) {
	entries, ok := eval.ContextKeys(context)
	if !ok {
		return nil, errors.New("toggleslash: SignContext: the context is not valid; see docs/evaluation.md")
	}
	var message []byte
	for _, entry := range entries {
		message = netstring(message, entry.Kind)
		// A lone surrogate is written as U+FFFD, as a UTF-8 encoder writes it.
		message = netstring(message, jsonval.ReplaceLoneSurrogates(entry.Key))
	}
	return message, nil
}

// netstring appends `<byte length>:<bytes>,`.
func netstring(dst []byte, text string) []byte {
	dst = strconv.AppendInt(dst, int64(len(text)), 10)
	dst = append(dst, ':')
	dst = append(dst, text...)
	return append(dst, ',')
}

func signingKey(privateKeyPEM string) (*ecdsa.PrivateKey, error) {
	refused := errors.New("toggleslash: SignContext: the key is not an ECDSA P-256 private key in a PKCS#8 PEM")
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, refused
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, refused
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	// The parser reads a P-384 or P-521 key just as well, so the curve is checked here.
	if !ok || key.Curve != elliptic.P256() {
		return nil, refused
	}
	return key, nil
}
