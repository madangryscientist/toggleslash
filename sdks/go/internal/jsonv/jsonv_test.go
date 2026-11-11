package jsonv

import (
	"math"
	"strings"
	"testing"
)

func TestParseNumbersAreBinary64(t *testing.T) {
	for in, want := range map[string]float64{"1e999": math.Inf(1), "-1e999": math.Inf(-1), "1e-999": 0, "2.0": 2, "-0": math.Copysign(0, -1), "9007199254740993": 9007199254740992} {
		v, err := Parse([]byte(in))
		n, ok := v.AsNumber()
		if err != nil || !ok || n != want || math.Signbit(n) != math.Signbit(want) {
			t.Errorf("Parse(%q) = %v, %v, %v; want %v", in, n, ok, err, want)
		}
	}
}

func TestParseRejectsWhatIsNotJSON(t *testing.T) {
	for _, in := range []string{"", " ", "{", "[1,]", `{"a":1,}`, "01", "1.", ".5", "+1", "NaN", "Infinity", "tru", `"a`, `"\x"`, `"\u12"`, "[] x", "\x00", "'a'", "{1:2}", "\"\n\""} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) succeeded", in)
		}
	}
	if _, err := Parse([]byte(strings.Repeat("[", MaxDepth+2) + strings.Repeat("]", MaxDepth+2))); err == nil {
		t.Error("nesting past MaxDepth was accepted")
	}
	if _, err := Parse([]byte(strings.Repeat("[", 600) + strings.Repeat("]", 600))); err != nil {
		t.Errorf("512-plus levels of nesting: %v", err)
	}
}

func TestLoneSurrogatesSurviveAsWTF8(t *testing.T) {
	v, err := Parse([]byte(`"a\ud800b\udc00😀"`))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := v.AsString()
	cps := CodePoints(s)
	want := []rune{'a', 0xD800, 'b', 0xDC00, 0x1F600}
	if len(cps) != len(want) {
		t.Fatalf("code points = %x", cps)
	}
	for i := range want {
		if cps[i] != want[i] {
			t.Errorf("code point %d = %x, want %x", i, cps[i], want[i])
		}
	}
	// Written back, a lone surrogate is an escape again.
	back, err := Parse(AppendString(nil, s))
	if err != nil {
		t.Fatalf("written text does not parse: %s", AppendString(nil, s))
	}
	if got, _ := back.AsString(); got != s {
		t.Errorf("written and parsed again: %x, want %x", got, s)
	}
}

func TestInvalidUTF8BecomesReplacementCharacters(t *testing.T) {
	v, _ := Parse([]byte("\"a\xFFb\xC3\"")) // a lone lead byte at the end is one U+FFFD
	if s, _ := v.AsString(); s != "a�b�" {
		t.Errorf("got %q", s)
	}
	v, _ = Parse([]byte("\xEF\xBB\xBF[1]"))
	if a, ok := v.AsArray(); !ok || len(a) != 1 {
		t.Error("a byte order mark was not skipped")
	}
}

func TestDuplicateKeysKeepTheLastAndOrderIsKept(t *testing.T) {
	v, err := Parse([]byte(`{"b":1,"a":2,"b":3}`))
	if err != nil {
		t.Fatal(err)
	}
	o, _ := v.AsObject()
	if got := o.Keys(); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Errorf("keys = %v", got)
	}
	if b, _ := o.Get("b"); !Equal(b, NumberValue(3)) {
		t.Errorf("b = %v", b)
	}
}

func TestAppendRoundTrips(t *testing.T) {
	in := `{"k":[1,2.5,"x\n\"\\",null,true,{"z":{}}],"é":"😀"}`
	v, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out := Append(nil, v)
	w, err := Parse(out)
	if err != nil || !Equal(v, w) {
		t.Errorf("round trip of %s gave %s (%v)", in, out, err)
	}
}

func TestEqualFollowsJSONEquality(t *testing.T) {
	a, _ := Parse([]byte(`{"a":1,"b":[1,2]}`))
	b, _ := Parse([]byte(`{"b":[1,2],"a":1.0}`))
	c, _ := Parse([]byte(`{"b":[2,1],"a":1}`))
	if !Equal(a, b) || Equal(a, c) || Equal(NullValue(), BoolValue(false)) {
		t.Error("Equal disagrees with JSON equality")
	}
}
