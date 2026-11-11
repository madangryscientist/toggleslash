package jsonvalue_test

import (
	"math"
	"strings"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

func mustParse(t *testing.T, text string) jsonvalue.Value {
	t.Helper()
	v, err := jsonvalue.Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return v
}

func TestNumbers(t *testing.T) {
	cases := []struct {
		text string
		want float64
	}{
		{"1e999", math.Inf(1)},
		{"-1e999", math.Inf(-1)},
		{"1e-999", 0},
		{"9007199254740993", 9007199254740992},
		{"0.5", 0.5},
		{"-12e2", -1200},
	}
	for _, c := range cases {
		got, ok := mustParse(t, c.text).Number()
		if !ok || got != c.want {
			t.Errorf("%s: got %v, want %v", c.text, got, c.want)
		}
	}
	negZero, _ := mustParse(t, "-0.0").Number()
	if !math.Signbit(negZero) {
		t.Error("-0.0 lost its sign")
	}
	if got := string(jsonvalue.Marshal(jsonvalue.NewNumber(math.Copysign(0, -1)))); got != "-0" {
		t.Errorf("Marshal(-0) = %s", got)
	}
}

func TestStrings(t *testing.T) {
	// A lone surrogate escape survives; a pair combines.
	lone, _ := mustParse(t, `"\ud800"`).String()
	if lone != "\xED\xA0\x80" {
		t.Errorf("lone surrogate = %q", lone)
	}
	pair, _ := mustParse(t, `"😀"`).String()
	if pair != "\U0001F600" {
		t.Errorf("pair = %q", pair)
	}
	// Raw invalid bytes become U+FFFD, as the WHATWG decoder does.
	bad, _ := mustParse(t, "\"a\xffb\"").String()
	if bad != "a�b" {
		t.Errorf("invalid byte = %q", bad)
	}
	// Writing and reading again keeps a lone surrogate.
	round := mustParse(t, string(jsonvalue.Marshal(jsonvalue.NewString(lone))))
	if got, _ := round.String(); got != lone {
		t.Errorf("round trip = %q", got)
	}
}

func TestObjectsAndShape(t *testing.T) {
	v := mustParse(t, "\xEF\xBB\xBF{\"a\":1,\"a\":2,\"b\":[true,null]}")
	if n, _ := v.Get("a").Number(); n != 2 {
		t.Errorf("duplicate key kept %v, want the last", n)
	}
	if !v.Get("missing").IsNull() {
		t.Error("a missing member is null")
	}
	if _, ok := mustParse(t, "[]").Array(); !ok {
		t.Error("[] is an array")
	}
}

func TestRejects(t *testing.T) {
	for _, text := range []string{"", "{", "[1,]", `{"a"}`, "01", "1.", "+1", `"\x"`, "nul", `"a`, "1 2", "\"\x01\""} {
		if _, err := jsonvalue.Parse([]byte(text)); err == nil {
			t.Errorf("Parse(%q) succeeded", text)
		}
	}
}

func TestDepth(t *testing.T) {
	deep := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	if _, err := jsonvalue.Parse([]byte(deep)); err != nil {
		t.Errorf("600 levels: %v", err)
	}
	tooDeep := strings.Repeat("[", jsonvalue.MaxDepth+1) + strings.Repeat("]", jsonvalue.MaxDepth+1)
	if _, err := jsonvalue.Parse([]byte(tooDeep)); err == nil {
		t.Error("beyond the limit parsed")
	}
}

func TestEqualAndFrom(t *testing.T) {
	a := jsonvalue.From(map[string]any{"k": []any{1, "x", nil, true}, "n": 0.0})
	b := mustParse(t, `{"n":-0,"k":[1,"x",null,true]}`)
	if !jsonvalue.Equal(a, b) {
		t.Errorf("%s != %s", jsonvalue.Marshal(a), jsonvalue.Marshal(b))
	}
	if jsonvalue.Equal(a, jsonvalue.NewString("x")) {
		t.Error("kinds differ")
	}
	if !jsonvalue.From(struct{}{}).IsNull() {
		t.Error("an unsupported type is null")
	}
}
