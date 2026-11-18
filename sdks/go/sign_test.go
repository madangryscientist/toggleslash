package toggleslash

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/slashsuite/toggleslash/sdks/go/jsonval"
)

var signatureShape = regexp.MustCompile(`^[A-Za-z0-9_-]{86}$`)

// newKey makes a key pair for one test; no private key is kept in the tree.
func newKey(t *testing.T, curve elliptic.Curve) (string, *ecdsa.PublicKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), &key.PublicKey
}

// verifies checks a P1363 base64url signature over message.
func verifies(t *testing.T, public *ecdsa.PublicKey, message []byte, signature string) bool {
	t.Helper()
	if !signatureShape.MatchString(signature) {
		t.Fatalf("the signature is not 86 base64url characters: %q", signature)
	}
	raw, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || len(raw) != 64 {
		t.Fatalf("the signature does not decode to 64 bytes: %q", signature)
	}
	digest := sha256.Sum256(message)
	r := new(big.Int).SetBytes(raw[:32])
	s := new(big.Int).SetBytes(raw[32:])
	return ecdsa.Verify(public, digest[:], r, s)
}

type signingMessage struct {
	name    string
	context jsonval.Value
	message string
}

// signingMessages reads fixtures/signing/messages.json from the repository at
// test time, walking up from the package directory.
func signingMessages(t *testing.T) []signingMessage {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for i := 0; i < 8; i++ {
		file, err := os.Open(filepath.Join(dir, "fixtures", "signing", "messages.json"))
		if err == nil {
			defer func() { _ = file.Close() }()
			doc, err := jsonval.Read(file, 64)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var out []signingMessage
			for _, raw := range doc.(jsonval.Object)["messages"].(jsonval.Array) {
				entry := raw.(jsonval.Object)
				out = append(out, signingMessage{
					name:    string(entry["name"].(jsonval.String)),
					context: entry["context"],
					message: string(entry["message"].(jsonval.String)),
				})
			}
			return out
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("no fixtures/signing/messages.json above the package")
	return nil
}

func TestSignContextSignsEveryFixtureMessage(t *testing.T) {
	privatePEM, public := newKey(t, elliptic.P256())
	messages := signingMessages(t)
	if len(messages) == 0 {
		t.Fatal("the fixture holds no messages")
	}
	for _, m := range messages {
		got, err := contextMessage(m.context)
		if err != nil {
			t.Errorf("%s: %v", m.name, err)
			continue
		}
		if string(got) != m.message {
			t.Errorf("%s: message %q, want %q", m.name, got, m.message)
		}
		signature, err := SignContext(privatePEM, m.context)
		if err != nil {
			t.Errorf("%s: %v", m.name, err)
			continue
		}
		if !verifies(t, public, []byte(m.message), signature) {
			t.Errorf("%s: the signature does not verify over %q", m.name, m.message)
		}
	}
}

func TestSignContextRoundTrip(t *testing.T) {
	privatePEM, public := newKey(t, elliptic.P256())
	context := jsonval.Object{"key": jsonval.String("user-1"), "plan": jsonval.String("pro")}
	signature, err := SignContext(privatePEM, context)
	if err != nil {
		t.Fatal(err)
	}
	if !verifies(t, public, []byte("4:user,6:user-1,"), signature) {
		t.Fatal("the signature does not verify")
	}
	if verifies(t, public, []byte("4:user,6:user-2,"), signature) {
		t.Fatal("the signature verifies over another context")
	}
}

func TestSignContextRefusesAKeyThatIsNotP256(t *testing.T) {
	p384, _ := newKey(t, elliptic.P384())
	context := jsonval.Object{"key": jsonval.String("u")}
	for name, key := range map[string]string{"P-384": p384, "not a PEM": "nonsense", "empty": ""} {
		if signature, err := SignContext(key, context); err == nil {
			t.Errorf("%s: signed %q instead of refusing", name, signature)
		}
	}
}

func TestSignContextRefusesAnInvalidContext(t *testing.T) {
	privatePEM, _ := newKey(t, elliptic.P256())
	for _, context := range []jsonval.Value{
		jsonval.Object{},
		jsonval.Object{"key": jsonval.String("")},
		jsonval.String("user-1"),
		jsonval.Object{"kind": jsonval.String("multi"), "contexts": jsonval.Object{}},
	} {
		if signature, err := SignContext(privatePEM, context); err == nil {
			t.Errorf("%v: signed %q instead of refusing", context, signature)
		}
	}
}
