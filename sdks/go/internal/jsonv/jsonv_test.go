package jsonv

import (
	"math"
	"strings"
	"testing"
)

func TestParseNumbers(t *testing.T) {
	for in, want := range map[string]float64{
		"0": 0, "-1": -1, "1.5": 1.5, "1e3": 1000, "1E-2": 0.01, "9007199254740993": 9007199254740992,
	} {
		v, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if n, _ := v.AsNumber(); n != want {
			t.Errorf("%s = %v, want %v", in, n, want)
		}
	}
	v, err := Parse([]byte("1e999"))
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := v.AsNumber(); !math.IsInf(n, 1) {
		t.Errorf("1e999 = %v, want +Inf", n)
	}
	v, _ = Parse([]byte("-0"))
	if n, _ := v.AsNumber(); !math.Signbit(n) {
		t.Errorf("-0 lost its sign")
	}
	v, err = Parse([]byte("-1e999"))
	if n, _ := v.AsNumber(); err != nil || !math.IsInf(n, -1) {
		t.Errorf("-1e999 = %v, %v", n, err)
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{
		"", "01", "1.", ".5", "+1", "NaN", "Infinity", "[1,]", "{\"a\":1,}", "{a:1}", "\"a", "\"\x01\"",
		"[1] 2", "\"\\x\"", "\"\\u12\"", "tru", "nul", "-", "1e", strings.Repeat("[", MaxDepth+2),
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%q parsed", in)
		}
	}
}

func TestStrings(t *testing.T) {
	cases := map[string]string{
		`"a\u0041"`:        "aA",
		`"\ud83d\ude00"`:   "\U0001F600",
		`"\ud800"`:         "\xed\xa0\x80",
		`"\ud800x"`:        "\xed\xa0\x80x",
		`"\udc00\ud800"`:   "\xed\xb0\x80\xed\xa0\x80",
		`"\ud800\u0041"`:   "\xed\xa0\x80A",
		"\"\xff\"":         "\uFFFD",
		"\"\u00e9\"":       "\u00e9",
		`"\/\b\f\n\r\t\""`: "/\b\f\n\r\t\"",
	}
	for in, want := range cases {
		v, err := Parse([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if s, _ := v.AsString(); s != want {
			t.Errorf("%s = %q, want %q", in, s, want)
		}
	}
}

func TestBomAndDuplicates(t *testing.T) {
	v, err := Parse([]byte("\xef\xbb\xbf{\"a\":1,\"b\":2,\"a\":3}"))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := v.Get("a")
	if n, _ := a.AsNumber(); n != 3 {
		t.Errorf("duplicate a = %v, want 3", n)
	}
	if got := v.Keys(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("keys = %v", got)
	}
}

func TestMarshal(t *testing.T) {
	cases := map[string]string{
		`{"a":[1,2.5,-0,1e21,1e-7,true,null,"x\n\u0001"]}`: `{"a":[1,2.5,0,1e+21,1e-7,true,null,"x\n\u0001"]}`,
		`"\ud800"`: `"\ud800"`,
		`1e999`:    `null`,
	}
	for in, want := range cases {
		v, err := Parse([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(Marshal(v)); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}

func TestFromAny(t *testing.T) {
	v, err := FromAny(map[string]any{"a": []any{1, "x", nil, true, 2.5}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(Marshal(v)); got != `{"a":[1,"x",null,true,2.5]}` {
		t.Errorf("got %s", got)
	}
	if _, err := FromAny(struct{}{}); err == nil {
		t.Error("struct accepted")
	}
}
