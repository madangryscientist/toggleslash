package jsonvalue

import (
	"errors"
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

func TestNumbers(t *testing.T) {
	cases := map[string]float64{
		"1": 1, "1.0": 1, "-0.0": math.Copysign(0, -1), "1e999": math.Inf(1), "-1e999": math.Inf(-1),
		"9007199254740993": 9007199254740992, "1E2": 100, "0.5e1": 5, "1e-999": 0,
	}
	for in, want := range cases {
		n, ok := mustParse(t, in).AsNumber()
		if !ok || n != want || math.Signbit(n) != math.Signbit(want) {
			t.Errorf("%s = %v, %v; want %v", in, n, ok, want)
		}
	}
	for _, bad := range []string{"", "01", "1.", ".5", "-", "1e", "1e+", "+1", "0x1", "NaN", "Infinity", "1 2"} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestStrings(t *testing.T) {
	s, _ := mustParse(t, `"aé😀\n"`).AsString()
	if s != "aé\U0001F600\n" {
		t.Errorf("escapes: %q", s)
	}
	lone, _ := mustParse(t, `"\ud800"`).AsString()
	if lone != "\xed\xa0\x80" {
		t.Errorf("lone surrogate: % x", lone)
	}
	loneLow, _ := mustParse(t, `"\udc00\ud800"`).AsString()
	if loneLow != "\xed\xb0\x80\xed\xa0\x80" {
		t.Errorf("low then high: % x", loneLow)
	}
	raw, _ := Parse([]byte("\"a\xff\xe2\x82b\""))
	if got, _ := raw.AsString(); got != "a��b" {
		t.Errorf("raw invalid bytes: %q", got)
	}
	for _, bad := range []string{`"a`, `"\x"`, `"\u12"`, "\"a\x01\"", `"\ud800\u"`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestObjectsAndBOM(t *testing.T) {
	v := mustParse(t, "\xef\xbb\xbf{\"a\":1,\"b\":2,\"a\":3}")
	ms := v.Members()
	if len(ms) != 2 || ms[0].Key != "a" || ms[1].Key != "b" {
		t.Fatalf("members = %+v", ms)
	}
	if n, _ := ms[0].Value.AsNumber(); n != 3 {
		t.Errorf("duplicate key should keep the last value, got %v", n)
	}
	big := strings.Builder{}
	big.WriteString("{")
	for i := range 50 {
		if i > 0 {
			big.WriteString(",")
		}
		big.WriteString(`"k` + string(rune('A'+i%26)) + string(rune('a'+i/26)) + `":` + "1")
	}
	big.WriteString(`,"kAa":9}`)
	bv := mustParse(t, big.String())
	if n, ok := bv.Get("kAa"); !ok {
		t.Errorf("indexed lookup failed")
	} else if f, _ := n.AsNumber(); f != 9 {
		t.Errorf("indexed duplicate = %v", f)
	}
}

func TestDepth(t *testing.T) {
	ok := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	if _, err := Parse([]byte(ok)); err != nil {
		t.Errorf("600 levels: %v", err)
	}
	deep := strings.Repeat("[", 100000) + strings.Repeat("]", 100000)
	if _, err := Parse([]byte(deep)); !errors.Is(err, ErrDepth) {
		t.Errorf("100000 levels = %v, want ErrDepth", err)
	}
}

func TestWrite(t *testing.T) {
	v := mustParse(t, `{"a":[1,2.5,true,null,"x\"\n\u0001"],"b":"\ud800","c":1e21,"d":-0.0}`)
	got := v.String()
	want := `{"a":[1,2.5,true,null,"x\"\n\u0001"],"b":"\ud800","c":1e+21,"d":0}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if FromNumber(math.Inf(1)).String() != "null" {
		t.Errorf("infinity should write as null")
	}
	again := mustParse(t, got)
	if !again.Equal(mustParse(t, `{"d":0,"c":1e21,"b":"\ud800","a":[1,2.5,true,null,"x\"\n\u0001"]}`)) {
		t.Errorf("round trip lost something: %s", again)
	}
}

func TestFrom(t *testing.T) {
	v, ok := From(map[string]any{"a": []any{1, "x", nil, true}, "b": int64(5)})
	if !ok {
		t.Fatal("From failed")
	}
	if !v.Equal(mustParse(t, `{"a":[1,"x",null,true],"b":5}`)) {
		t.Errorf("From = %s", v)
	}
	if _, ok := From(struct{}{}); ok {
		t.Errorf("From of a struct should fail")
	}
	if s, _ := FromString("a\xffb").AsString(); s != "a�b" {
		t.Errorf("FromString sanitises: %q", s)
	}
}
