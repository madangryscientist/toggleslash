package jsonvalue

import (
	"math"
	"strings"
	"testing"
)

func mustParse(t *testing.T, s string) Value {
	t.Helper()
	v, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

func TestParseAccepts(t *testing.T) {
	for _, s := range []string{
		`null`, `true`, `false`, `0`, `-0`, `1.5e3`, `1E+2`, `""`, `"a\né😀"`, `[]`, `{}`, ` [1, {"a": [null]}] `,
		`"\ud800"`, `"\udc00x"`, `{"a":1,"a":2}`, `1e999`, `-1e999`,
	} {
		mustParse(t, s)
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{
		``, ` `, `nul`, `tru`, `01`, `+1`, `.5`, `1.`, `1e`, `1e+`, `-`, `"a`, `"\x"`, `"\u12"`, `"\ud8"`, "\"a\nb\"",
		`[1,]`, `[1 2]`, `{"a"}`, `{"a":1,}`, `{a:1}`, `[`, `{`, `1 2`, `NaN`, `Infinity`, `'a'`, "\"\x01\"",
	} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
}

func TestDepthIsBounded(t *testing.T) {
	deep := strings.Repeat("[", 100000) + strings.Repeat("]", 100000)
	if _, err := Parse([]byte(deep)); err == nil {
		t.Fatal("100000 levels parsed")
	}
}

func TestNumbers(t *testing.T) {
	if n := mustParse(t, `1e999`).NumberValue(); !math.IsInf(n, 1) {
		t.Fatalf("1e999 = %v", n)
	}
	if mustParse(t, `1e999`).IsFiniteNumber() {
		t.Fatal("1e999 is finite")
	}
	if n := mustParse(t, `-0`).NumberValue(); !math.Signbit(n) {
		t.Fatal("-0 lost its sign")
	}
	if n := mustParse(t, `9223372036854775807`).NumberValue(); n != 9223372036854775808 {
		t.Fatalf("2^63-1 reads as binary64: %v", n)
	}
}

func TestLoneSurrogatesSurvive(t *testing.T) {
	v := mustParse(t, `"a\ud800b"`)
	s := v.StringValue()
	if !HasLoneSurrogate(s) {
		t.Fatalf("lost the surrogate: %q", s)
	}
	if got := string(Marshal(v)); got != `"a\ud800b"` {
		t.Fatalf("Marshal: %s", got)
	}
	pair := mustParse(t, `"😀"`).StringValue()
	if pair != "😀" || HasLoneSurrogate(pair) {
		t.Fatalf("a pair is one code point: %q", pair)
	}
}

func TestWTF8Helpers(t *testing.T) {
	lone := string(AppendCodePoint(nil, 0xD800))
	if len(lone) != 3 || !HasLoneSurrogate(lone) {
		t.Fatalf("lone surrogate bytes % x", lone)
	}
	if r, n := DecodeRune(lone); r != 0xD800 || n != 3 {
		t.Fatalf("DecodeRune: %x %d", r, n)
	}
	if got := ReplaceLoneSurrogates("a" + lone + "b"); got != "a�b" {
		t.Fatalf("Replace: %q", got)
	}
	// UTF-16 order differs from code point order: U+FFFF sorts after U+10000 in UTF-8 bytes, before in UTF-16.
	if CompareUTF16("￿", "\U00010000") <= 0 {
		t.Fatal("UTF-16 order: U+FFFF must sort after U+10000's surrogate pair (D800)")
	}
	if got := UTF16("😀"); len(got) != 2 || got[0] != 0xD83D || got[1] != 0xDE00 {
		t.Fatalf("UTF16: %x", got)
	}
	if !ContainsUTF16("a😀b", "😀") || !HasPrefixUTF16("😀b", "😀") || !HasSuffixUTF16("a😀", "😀") {
		t.Fatal("UTF-16 containment")
	}
	if got := string(DecodeLossy([]byte("a\xffb"))); got != "a�b" {
		t.Fatalf("DecodeLossy: %q", got)
	}
	if got := string(DecodeLossy([]byte("a" + lone))); got != "a���" && got != "a�" {
		t.Fatalf("DecodeLossy of a surrogate: %q", got)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	for _, s := range []string{
		`null`, `true`, `[1,2.5,"x"]`, `{"a":{"b":[]},"c":"é"}`, `"\u0000\u001f\"\\"`, `{"k":1,"k2":2}`,
	} {
		v := mustParse(t, s)
		again := mustParse(t, string(Marshal(v)))
		if !Equal(v, again) {
			t.Errorf("%s did not round trip: %s", s, Marshal(v))
		}
	}
	if got := string(Marshal(mustParse(t, `{"a":1,"a":2}`))); got != `{"a":2}` {
		t.Fatalf("a later duplicate key wins: %s", got)
	}
	if got := string(Marshal(Number(math.Inf(1)))); got != "null" {
		t.Fatalf("an infinite number writes as %s", got)
	}
}

func TestFromGo(t *testing.T) {
	v, err := FromGo(map[string]any{
		"i": 3, "i8": int8(-1), "u": uint64(7), "f": 1.5, "b": true, "s": "x", "n": nil,
		"a": []any{1, "y"}, "ss": []string{"p", "q"}, "v": String("z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("ss"); len(got.Items()) != 2 {
		t.Fatalf("[]string: %v", got)
	}
	for _, bad := range []any{make(chan int), struct{}{}, map[string]any{"x": func() {}}} {
		if _, err := FromGo(bad); err == nil {
			t.Errorf("FromGo(%T) accepted", bad)
		}
	}
}

func TestObjectOrderAndLookup(t *testing.T) {
	o := NewObject()
	o.Set("b", Number(1))
	o.Set("a", Number(2))
	o.Set("b", Number(3))
	if o.Len() != 2 {
		t.Fatalf("Len %d", o.Len())
	}
	if k, v := o.At(0); k != "b" || v.NumberValue() != 3 {
		t.Fatalf("At(0): %s %v", k, v)
	}
	if _, ok := o.Get("zzz"); ok {
		t.Fatal("found a missing key")
	}
}

func TestParseBodyDropsABOMOnly(t *testing.T) {
	if _, err := ParseBody([]byte("\xEF\xBB\xBF{}")); err != nil {
		t.Fatalf("a body with a BOM: %v", err)
	}
	if _, err := ParseBody([]byte("{}")); err != nil {
		t.Fatal(err)
	}
}
