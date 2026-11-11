package jsonvalue

import (
	"math"
	"strings"
	"testing"
)

func parse(t *testing.T, text string) Value {
	t.Helper()
	v, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return v
}

func TestParseNumbersAreBinary64(t *testing.T) {
	cases := []struct {
		text string
		want float64
	}{
		{"1.0", 1},
		{"-0.0", math.Copysign(0, -1)},
		{"1e999", math.Inf(1)},
		{"-1e999", math.Inf(-1)},
		{"9007199254740993", 9007199254740992},
		{"0.1", 0.1},
		{"1E2", 100},
		{"-12.5e-1", -1.25},
		{"1e-999", 0},
	}
	for _, c := range cases {
		n, ok := parse(t, c.text).AsNumber()
		if !ok || n != c.want || math.Signbit(n) != math.Signbit(c.want) {
			t.Errorf("%s = %v (%v), want %v", c.text, n, ok, c.want)
		}
	}
	if parse(t, "1e999").IsFiniteNumber() {
		t.Error("1e999 should not be finite")
	}
}

func TestParseRejectsWhatJSONForbids(t *testing.T) {
	bad := []string{
		"", " ", "{", "[1,]", "{\"a\":1,}", "01", "+1", "1.", ".5", "-", "1e", "0x10", "NaN", "Infinity",
		"'a'", "\"a", "\"\\x\"", "\"\\u12\"", "\"a\nb\"", "\"\x01\"", "tru", "nul", "[1 2]", "{\"a\" 1}",
		"{1:2}", "1 2", "[] x", "\"\\ud800\\u0041\\\"",
	}
	for _, text := range bad {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("Parse(%q) should fail", text)
		}
	}
}

func TestParseStringsKeepLoneSurrogates(t *testing.T) {
	cases := []struct{ text, want string }{
		{`"\ud800"`, "\xed\xa0\x80"},
		{`"\udc00"`, "\xed\xb0\x80"},
		{`"\ud83d\ude00"`, "\U0001F600"},
		{`"\ude00\ud83d"`, "\xed\xb8\x80\xed\xa0\xbd"},
		{`"\u0041\n\t\/"`, "A\n\t/"},
		{"\"a\xffb\"", "a\ufffdb"},
	}
	for _, c := range cases {
		s, ok := parse(t, c.text).AsString()
		if !ok || s != c.want {
			t.Errorf("%s = %q, want %q", c.text, s, c.want)
		}
	}
}

func TestParseSkipsABOMAndSpace(t *testing.T) {
	v := parse(t, "\xef\xbb\xbf \t\r\n[ 1 , 2 ]\n")
	items, ok := v.AsArray()
	if !ok || len(items) != 2 {
		t.Errorf("got %v", Marshal(v))
	}
}

func TestDuplicateKeysLastWins(t *testing.T) {
	v := parse(t, `{"a":1,"b":2,"a":3}`)
	obj, _ := v.AsObject()
	a, _ := obj.Get("a")
	if n, _ := a.AsNumber(); n != 3 || obj.Len() != 2 {
		t.Errorf("got %s", Marshal(v))
	}
}

func TestNestingLimit(t *testing.T) {
	deep := strings.Repeat("[", MaxDepth) + strings.Repeat("]", MaxDepth)
	if _, err := Parse([]byte(deep)); err != nil {
		t.Errorf("depth %d should parse: %v", MaxDepth, err)
	}
	tooDeep := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	if _, err := Parse([]byte(tooDeep)); err == nil {
		t.Errorf("depth %d should fail", MaxDepth+1)
	}
}

func TestErrorCarriesNoInput(t *testing.T) {
	_, err := Parse([]byte(`{"secret-context-key": }`))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error text: %v", err)
	}
}

func TestEqualIsTheSpecs(t *testing.T) {
	same := [][2]string{
		{"0", "-0"}, {"1", "1.0"}, {`{"a":1,"b":2}`, `{"b":2,"a":1}`}, {"[1,[2]]", "[1,[2]]"},
	}
	for _, c := range same {
		if !Equal(parse(t, c[0]), parse(t, c[1])) {
			t.Errorf("%s should equal %s", c[0], c[1])
		}
	}
	differ := [][2]string{
		{"1", `"1"`}, {"[1]", "[1,2]"}, {`{"a":1}`, `{"a":1,"b":2}`}, {"null", "false"}, {`{"a":1}`, `{"a":2}`},
		{`"a"`, `"b"`},
	}
	for _, c := range differ {
		if Equal(parse(t, c[0]), parse(t, c[1])) {
			t.Errorf("%s should differ from %s", c[0], c[1])
		}
	}
	nan := Number(math.NaN())
	if Equal(nan, nan) {
		t.Error("NaN equals nothing")
	}
}

func TestMarshalRoundTrips(t *testing.T) {
	texts := []string{
		`{"a":[1,2.5,-0.25,true,false,null,"x\ny"],"b":{"c":"\u0001"}}`,
		`"\ud800"`,
		`"\ud83d\ude00"`,
		`[1e300,1e-7,123456789012345680000]`,
	}
	for _, text := range texts {
		v := parse(t, text)
		again, err := Parse(Marshal(v))
		if err != nil || !Equal(v, again) {
			t.Errorf("%s did not round trip: %s (%v)", text, Marshal(v), err)
		}
	}
}

func TestMarshalNonFiniteNumbersAreNull(t *testing.T) {
	if got := string(Marshal(Number(math.Inf(1)))); got != "null" {
		t.Errorf("got %s", got)
	}
}

func TestFieldOnlyReadsObjects(t *testing.T) {
	if _, ok := parse(t, `[1]`).Field("a"); ok {
		t.Error("an array has no fields")
	}
	if v, ok := parse(t, `{"a":null}`).Field("a"); !ok || !v.IsNull() {
		t.Error("a null field is present and null")
	}
}
