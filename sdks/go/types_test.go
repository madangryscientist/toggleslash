package toggleslash

import (
	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
	"strings"
	"testing"
	"time"
)

func TestContextConstructors(t *testing.T) {
	user := UserContext("u1", map[string]Value{"plan": String("pro")})
	got := string(mustMarshal(user.Value()))
	if got != `{"key":"u1","attributes":{"plan":"pro"}}` {
		t.Fatalf("user: %s", got)
	}
	org := NewContext("org", "o1", nil)
	if got := string(mustMarshal(org.Value())); got != `{"kind":"org","key":"o1"}` {
		t.Fatalf("org: %s", got)
	}
	multi := MultiContext(map[string]Context{"user": NewContext("ignored", "u1", nil), "org": org})
	if got := string(mustMarshal(multi.Value())); got != `{"kind":"multi","contexts":{"org":{"key":"o1"},"user":{"key":"u1"}}}` {
		t.Fatalf("multi: %s", got)
	}
	native := ContextFromMap(map[string]any{
		"key": "u2", "attributes": map[string]any{"n": 3, "f": 1.5, "b": true, "l": []any{"a", nil}, "u": uint8(4), "bad": make(chan int)},
	})
	if got := string(mustMarshal(native.Value())); got != `{"attributes":{"b":true,"bad":null,"f":1.5,"l":["a",null],"n":3,"u":4},"key":"u2"}` {
		t.Fatalf("native: %s", got)
	}
	if !ContextFromJSON([]byte(`{`)).v.IsNull() {
		t.Fatal("bad JSON should make the zero context")
	}
	if got := string(mustMarshal(ContextFromJSON([]byte(`{"key":"x"}`)).Value())); got != `{"key":"x"}` {
		t.Fatalf("json: %s", got)
	}
}

func TestInvalidUTF8StringBecomesReplacement(t *testing.T) {
	s, _ := String("a\xffb").AsString()
	if s != "a�b" {
		t.Fatalf("%q", s)
	}
}

func TestDeepNativeContextDoesNotOverflow(t *testing.T) {
	var deep any = "x"
	for i := 0; i < 20000; i++ {
		deep = []any{deep}
	}
	_ = ContextFromMap(map[string]any{"key": "k", "attributes": map[string]any{"d": deep}})
}

func TestSyncErrorTextHoldsOnlyTheCode(t *testing.T) {
	for _, e := range []*SyncError{{Code: "http_503"}, {Code: SyncNetwork, Usage: true}} {
		if strings.ContainsAny(e.Error(), "/@") {
			t.Fatalf("text %q", e.Error())
		}
	}
}

func TestOptionsIgnoreNonPositiveDurations(t *testing.T) {
	cfg := defaultConfig()
	WithPollInterval(0)(&cfg)
	WithFlushInterval(-time.Second)(&cfg)
	WithRequestTimeout(0)(&cfg)
	if cfg.pollInterval != defaultPollInterval || cfg.flushInterval != defaultFlushInterval || cfg.requestTimeout != defaultRequestTimeout {
		t.Fatalf("%+v", cfg)
	}
}

func TestNewWithTrailingSlashAndNilOption(t *testing.T) {
	c := New("http://127.0.0.1:1///", "k", nil, WithStream(false))
	defer c.Close()
	if c.url != "http://127.0.0.1:1" {
		t.Fatal(c.url)
	}
}

func TestInitialStatus(t *testing.T) {
	c := New("http://127.0.0.1:1", "k", WithStream(true))
	defer c.Close()
	s := c.Status()
	if s.Stream != StreamConnecting && s.Stream != StreamDisconnected {
		t.Fatalf("%+v", s)
	}
}

func mustMarshal(v Value) []byte { return jsonvalue.Marshal(v) }
