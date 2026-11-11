package jsonv

import (
	"math"
	"strings"
	"testing"
)

func TestParseAndMarshalRoundTrip(t *testing.T) {
	for _, in := range []string{
		`null`, `true`, `false`, `0`, `-0`, `1.5`, `1e+21`, `"x"`, `[]`, `{}`,
		`[1,"a",[true,null],{"k":{"j":[]}}]`, `"\u0000\n\t\"\\"`,
	} {
		v, err := Parse([]byte(in))
		if err != nil {
			t.Errorf("Parse(%s): %v", in, err)
			continue
		}
		again, err := Parse(Marshal(v))
		if err != nil || !Equal(v, again) {
			t.Errorf("%s did not survive Marshal: %s", in, Marshal(v))
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{
		``, ` `, `{`, `[1,]`, `{"a":1,}`, `{"a"}`, `01`, `1.`, `.5`, `+1`, `-`, `1e`, `nul`, `"abc`, `'a'`,
		`"\x"`, `"\u12"`, "\"a\nb\"", `[1] 2`, `NaN`, `Infinity`, `{1:2}`,
		strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1),
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) accepted", in)
		}
	}
}

func TestParseDetails(t *testing.T) {
	if _, err := Parse([]byte(strings.Repeat("[", MaxDepth) + strings.Repeat("]", MaxDepth))); err != nil {
		t.Errorf("nesting at MaxDepth: %v", err)
	}
	v, err := Parse([]byte("\xEF\xBB\xBF {\"a\":1,\"a\":2}"))
	if err != nil {
		t.Fatalf("BOM and duplicate key: %v", err)
	}
	if a, _ := v.Get("a"); !Equal(a, NewNumber(2)) {
		t.Error("a duplicate key must keep the last value")
	}
	big, err := Parse([]byte(`1e999`))
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := big.Number(); !math.IsInf(f, 1) || big.IsFinite() {
		t.Errorf("1e999 should read as an infinity, got %v", f)
	}
	neg, _ := Parse([]byte(`-0`))
	if f, _ := neg.Number(); !math.Signbit(f) {
		t.Error("-0 lost its sign")
	}
	// A lone surrogate escape is kept, and written back as an escape.
	lone, err := Parse([]byte(`"a\ud800b"`))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := lone.Str()
	if !HasLoneSurrogate(s) {
		t.Error("the lone surrogate was lost")
	}
	if got := string(Marshal(lone)); got != `"a\ud800b"` {
		t.Errorf("Marshal = %s", got)
	}
	pair, _ := Parse([]byte(`"😀"`))
	if s, _ := pair.Str(); s != "\U0001F600" {
		t.Errorf("a surrogate pair = %q", s)
	}
	bad, _ := Parse([]byte("\"a\xFFb\""))
	if s, _ := bad.Str(); s != "a�b" {
		t.Errorf("an invalid byte = %q", s)
	}
}

func TestDecodeUTF8(t *testing.T) {
	cases := map[string]string{
		"abc":                  "abc",
		"\xFF":                 "�",
		"\xC3":                 "�",
		"\xE2\x82":             "�",
		"\xE2\x82\xAC":         "€",
		"\xF0\x9F\x98":         "�",
		"\xED\xA0\x80":         "���", // a surrogate in UTF-8 is invalid
		"\xC0\x80":             "��",
		"\xF4\x90\x80\x80":     "����",
		"a\xE2\x82b":           "a�b",
		"\xEF\xBB\xBFx":        "\xEF\xBB\xBFx",
		"\xE0\x80\x80":         "���",
		"\xF0\x80\x80\x80":     "����",
		"\xC2\x41":             "�A",
		"\xE2\x82\xAC\xE2\x82": "€�",
	}
	for in, want := range cases {
		if got := string(DecodeUTF8([]byte(in))); got != want {
			t.Errorf("DecodeUTF8(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompareIsByUTF16Unit(t *testing.T) {
	// U+FFFF is above U+1F600 by code point order is false: in UTF-16 the astral character's high
	// surrogate (D83D) sorts below U+FFFF, so the astral one is smaller.
	if Compare("\U0001F600", "￿") >= 0 {
		t.Error("an astral character must sort below U+FFFF by UTF-16 units")
	}
	if Compare("a", "b") >= 0 || Compare("b", "a") <= 0 || Compare("x", "x") != 0 {
		t.Error("ASCII order")
	}
	if Compare("ab", "abc") >= 0 {
		t.Error("a prefix sorts first")
	}
}

func TestStringHelpersUseCodePoints(t *testing.T) {
	if !Contains("a\U0001F600b", "\U0001F600") || Contains("abc", "d") {
		t.Error("Contains")
	}
	if !HasPrefix("\U0001F600x", "\U0001F600") || HasPrefix("x", "xy") {
		t.Error("HasPrefix")
	}
	if !HasSuffix("x\U0001F600", "\U0001F600") || HasSuffix("x", "yx") {
		t.Error("HasSuffix")
	}
	if got := len(CodePoints("a\U0001F600")); got != 2 {
		t.Errorf("CodePoints counted %d", got)
	}
}

func TestEqualAndWrite(t *testing.T) {
	a, _ := Parse([]byte(`{"x":[1,2,{"y":null}],"z":"s"}`))
	b, _ := Parse([]byte(`{"z":"s","x":[1,2,{"y":null}]}`))
	c, _ := Parse([]byte(`{"z":"s","x":[1,2,{"y":0}]}`))
	if !Equal(a, b) || Equal(a, c) {
		t.Error("Equal")
	}
	if Equal(NewNumber(1), NewString("1")) || !Equal(NewNumber(0), NewNumber(math.Copysign(0, -1))) {
		t.Error("Equal across kinds and zeros")
	}
	if got := string(Marshal(NewNumber(math.NaN()))); got != "null" {
		t.Errorf("NaN is written as %s, want null", got)
	}
	if got := string(Marshal(NewNumber(math.Inf(-1)))); got != "null" {
		t.Errorf("-Inf is written as %s, want null", got)
	}
	if got := string(AppendString(nil, "a\"\\\n\r\t\x01 ")); got != `"a\"\\\n\r\t\u0001`+" "+`"` {
		t.Errorf("AppendString = %s", got)
	}
}
