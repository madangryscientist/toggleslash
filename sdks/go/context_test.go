package toggleslash

import (
	"math"
	"testing"
)

func TestContextConstructors(t *testing.T) {
	cases := []struct {
		name  string
		ctx   Context
		valid bool
	}{
		{"zero", Context{}, false},
		{"user", NewContext("u", nil), true},
		{"with attributes", NewContext("u", map[string]any{"plan": "pro", "age": 30, "tags": []any{"a"}, "nested": map[string]any{"k": 1.5}}), true},
		{"empty key", NewContext("", nil), false},
		{"kind", NewKindContext("org", "acme", nil), true},
		{"bad kind", NewKindContext("Bad Kind", "acme", nil), false},
		{"kind multi", NewKindContext("multi", "x", nil), false},
		{"unsupported attribute", NewContext("u", map[string]any{"f": func() {}}), false},
		{"a non-finite attribute", NewContext("u", map[string]any{"n": math.Inf(1)}), true},
		{"multi", NewMultiContext(NewContext("u", nil), NewKindContext("org", "acme", nil)), true},
		{"multi of none", NewMultiContext(), false},
		{"multi repeating a kind", NewMultiContext(NewContext("u", nil), NewContext("v", nil)), false},
		{"multi holding a multi", NewMultiContext(NewMultiContext(NewContext("u", nil))), false},
		{"multi holding an invalid", NewMultiContext(NewContext("", nil)), false},
		{"from JSON", ContextFromJSON([]byte(`{"key":"u"}`)), true},
		{"from bad JSON", ContextFromJSON([]byte(`{`)), false},
		{"from JSON, kind null", ContextFromJSON([]byte(`{"kind":null,"key":"u"}`)), true},
		{"from JSON, key number", ContextFromJSON([]byte(`{"key":1}`)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ctx.Valid(); got != tc.valid {
				t.Fatalf("Valid() = %v, want %v (%s)", got, tc.valid, tc.ctx.Value())
			}
		})
	}
}

func TestAnInvalidContextGivesTheFallbackAndAnErrorReason(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f)
	c.Ready()
	d := c.BoolVariationDetail("a", Context{}, false)
	if d.Value || d.VariationIndex != nil || d.Reason.Kind != ReasonError {
		t.Fatalf("%+v", d)
	}
}

func TestAMultiContextEvaluates(t *testing.T) {
	f := newFakeServer(t, boolFlag("a", true, 1))
	c := newTestClient(t, f)
	c.Ready()
	m := NewMultiContext(NewContext("u", nil), NewKindContext("org", "acme", nil))
	if !c.BoolVariation("a", m, false) {
		t.Fatal("a multi context was not accepted")
	}
}
