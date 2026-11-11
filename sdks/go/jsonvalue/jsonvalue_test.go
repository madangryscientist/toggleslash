package jsonvalue

import (
	"strings"
	"testing"
)

func TestParseAndWriteRoundTrip(t *testing.T) {
	cases := map[string]string{
		`null`:                           `null`,
		` [1, 2.5, -0, "a", true] `:      `[1,2.5,-0,"a",true]`,
		`{"a":1,"a":2,"b":{"c":[]}}`:     `{"a":2,"b":{"c":[]}}`,
		"\xef\xbb\xbf{\"k\":\"v\"}":      `{"k":"v"}`,
		`"😀"`:                            "\"\U0001F600\"",
		`"\ud800"`:                       `"\ud800"`,
		`"\udc00\ud800"`:                 `"\udc00\ud800"`,
		`1e999`:                          `null`,
		`"a\u0000b"`:                     `"a\u0000b"`,
		"\"\xff\"":                       "\"�\"",
		"\"\xe2\x82\"":                   "\"�\"",
		"\"\xf0\x9f\x98\"":               "\"�\"",
		`{"a":"\/"}`:                     `{"a":"/"}`,
		`[ ]`:                            `[]`,
		`123456789012345678901234567890`: `1.2345678901234568e+29`,
	}
	for in, want := range cases {
		v, err := Parse([]byte(in))
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got := v.String(); got != want {
			t.Errorf("Parse(%q).String() = %q, want %q", in, got, want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{``, `{`, `[1,]`, `01`, `1.`, `"\x"`, `{"a" 1}`, `nul`, `[1] x`, "\"a\nb\"", `'a'`, `+1`, `"\u12"`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", in)
		}
	}
}

func TestDepth(t *testing.T) {
	deep := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	if _, err := Parse([]byte(deep)); err != nil {
		t.Errorf("depth 600: %v", err)
	}
	tooDeep := strings.Repeat("[", MaxDepth+5) + strings.Repeat("]", MaxDepth+5)
	if _, err := Parse([]byte(tooDeep)); err == nil {
		t.Errorf("depth %d parsed", MaxDepth+5)
	}
}

func TestLossyUTF8Subparts(t *testing.T) {
	cases := map[string]string{
		"\xf0\x80\x80":     "���",
		"\xed\xa0\x80":     "���",
		"\xe0\x80":         "��",
		"a\xc3":            "a�",
		"\xc3\x28":         "�(",
		"\xf4\x90\x80\x80": "����",
	}
	for in, want := range cases {
		if got := LossyUTF8([]byte(in)); got != want {
			t.Errorf("LossyUTF8(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSurrogateHelpers(t *testing.T) {
	lone := FromUTF16([]uint16{0xd800, 'a', 0xdc00})
	if got := UTF16(lone); len(got) != 3 || got[0] != 0xd800 || got[2] != 0xdc00 {
		t.Errorf("UTF16 round trip = %x", got)
	}
	if got := ReplaceLoneSurrogates(lone); got != "�a�" {
		t.Errorf("ReplaceLoneSurrogates = %q", got)
	}
	pair := FromUTF16([]uint16{0xd83d, 0xde00})
	if pair != "\U0001F600" {
		t.Errorf("a pair joins to %q", pair)
	}
	if CompareUTF16("\U0001F600", "￿") >= 0 {
		t.Error("U+1F600 (units D83D DE00) must order before U+FFFF")
	}
	if CompareUTF16(lone, lone) != 0 || CompareUTF16("a", "b") >= 0 {
		t.Error("CompareUTF16 basic ordering")
	}
}

func TestEqualAndObjects(t *testing.T) {
	a, _ := Parse([]byte(`{"x":[1,{"y":-0}],"z":null}`))
	b, _ := Parse([]byte(`{"z":null,"x":[1,{"y":0}]}`))
	if !Equal(a, b) {
		t.Error("objects equal regardless of order, -0 equal 0")
	}
	c, _ := Parse([]byte(`{"x":[1,{"y":1}],"z":null}`))
	if Equal(a, c) {
		t.Error("different values compared equal")
	}
	if v, ok := a.Get("z"); !ok || !v.IsNull() {
		t.Error("Get of a present null")
	}
}

func TestFromAny(t *testing.T) {
	v := From(map[string]any{"a": []any{1, "x", nil, true, 2.5}})
	if got := v.String(); got != `{"a":[1,"x",null,true,2.5]}` {
		t.Errorf("From = %s", got)
	}
}
