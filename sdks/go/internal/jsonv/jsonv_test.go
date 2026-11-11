package jsonv

import (
	"math"
	"strings"
	"testing"
)

func mustParse(t *testing.T, text string) Value {
	t.Helper()
	v, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return v
}

func TestNumbersAreBinary64(t *testing.T) {
	cases := map[string]float64{
		"1e999":            math.Inf(1),
		"-1e999":           math.Inf(-1),
		"9007199254740993": 9007199254740992,
		"1.0":              1,
		"1E2":              100,
		"0.5":              0.5,
		"-0":               0,
		"1e-999":           0,
	}
	for text, want := range cases {
		got, ok := mustParse(t, text).AsNumber()
		if !ok || got != want {
			t.Errorf("%s = %v, want %v", text, got, want)
		}
	}
	if got, _ := mustParse(t, "-0.0").AsNumber(); !math.Signbit(got) {
		t.Errorf("-0.0 lost its sign")
	}
}

func TestStringsKeepLoneSurrogates(t *testing.T) {
	s, _ := mustParse(t, `"\ud800"`).AsString()
	if s != "\xED\xA0\x80" {
		t.Errorf("lone high = % x", s)
	}
	s, _ = mustParse(t, `"\udc00"`).AsString()
	if s != "\xED\xB0\x80" {
		t.Errorf("lone low = % x", s)
	}
	s, _ = mustParse(t, `"😀"`).AsString()
	if s != "😀" {
		t.Errorf("pair = % x", s)
	}
	s, _ = mustParse(t, `"\ude00\ud83d"`).AsString()
	if s != "\xED\xB8\x80\xED\xA0\xBD" {
		t.Errorf("reversed pair = % x", s)
	}
	s, _ = mustParse(t, `"\ud83dx"`).AsString()
	if s != "\xED\xA0\xBDx" {
		t.Errorf("lone high then x = % x", s)
	}
	out := string(Marshal(String("\xED\xA0\x80")))
	if out != `"\ud800"` {
		t.Errorf("write lone = %s", out)
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, text := range []string{"", "{", "[1,]", `{"a":1,}`, "01", "1.", "-", `"a`, "tru", "1 2", `"\x"`, "\"a\x01\"", `"\ud8"`, "+1", ".5"} {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("Parse(%q) succeeded", text)
		}
	}
}

func TestDuplicateKeysKeepTheLast(t *testing.T) {
	v := mustParse(t, `{"a":1,"b":2,"a":3}`)
	if n, _ := v.Get("a").AsNumber(); n != 3 {
		t.Errorf("a = %v", n)
	}
	o, _ := v.AsObject()
	if o.Len() != 2 {
		t.Errorf("len = %d", o.Len())
	}
}

func TestDepth(t *testing.T) {
	deep := strings.Repeat("[", 5000) + strings.Repeat("]", 5000)
	if _, err := Parse([]byte(deep)); err != nil {
		t.Errorf("5000 deep: %v", err)
	}
	tooDeep := strings.Repeat("[", MaxDepth+5) + strings.Repeat("]", MaxDepth+5)
	if _, err := Parse([]byte(tooDeep)); err == nil {
		t.Errorf("past the limit succeeded")
	}
}

func TestDecodeUTF8(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abc", "abc"},
		{"\xFF", "�"},
		{"\xC0\x80", "��"},
		{"\xE2\x82", "�"},
		{"\xE2\x82x", "�x"},
		{"\xED\xA0\x80", "���"},
		{"\xF0\x9F\x98", "�"},
		{"\xF4\x90\x80\x80", "����"},
		{"a\x80b", "a�b"},
	}
	for _, c := range cases {
		if got := string(DecodeUTF8([]byte(c.in))); got != c.want {
			t.Errorf("DecodeUTF8(% x) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := StripBOM([]byte("\xEF\xBB\xBF{}")); string(got) != "{}" {
		t.Errorf("BOM not stripped")
	}
}

func TestWTF8Helpers(t *testing.T) {
	s := "a\xED\xA0\x80😀"
	got := Runes(s)
	want := []rune{'a', 0xD800, 0x1F600}
	if len(got) != len(want) {
		t.Fatalf("Runes = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rune %d = %x, want %x", i, got[i], want[i])
		}
	}
	units := UTF16(s)
	wantUnits := []uint16{'a', 0xD800, 0xD83D, 0xDE00}
	for i := range wantUnits {
		if units[i] != wantUnits[i] {
			t.Errorf("unit %d = %x, want %x", i, units[i], wantUnits[i])
		}
	}
	if ToValidUTF8(s) != "a�😀" {
		t.Errorf("ToValidUTF8 = %q", ToValidUTF8(s))
	}
	if !HasLoneSurrogate(s) || HasLoneSurrogate("😀é") {
		t.Errorf("HasLoneSurrogate wrong")
	}
}

func TestWriteRoundTrip(t *testing.T) {
	text := `{"a":[1,2.5,"x\n",true,null],"b":{"c":1e21}}`
	v := mustParse(t, text)
	back := mustParse(t, string(Marshal(v)))
	if !Equal(v, back) {
		t.Errorf("round trip: %s", Marshal(v))
	}
	if got := string(Marshal(Number(math.Inf(1)))); got != "null" {
		t.Errorf("inf = %s", got)
	}
	if got := string(Marshal(Number(math.Copysign(0, -1)))); got != "0" {
		t.Errorf("-0 = %s", got)
	}
	if got := string(Marshal(Number(1e-7))); got != "1e-7" {
		t.Errorf("1e-7 = %s", got)
	}
}

func TestEqual(t *testing.T) {
	if !Equal(Number(0), Number(math.Copysign(0, -1))) {
		t.Errorf("-0 != 0")
	}
	if Equal(Number(1), String("1")) {
		t.Errorf("1 == \"1\"")
	}
	a := mustParse(t, `{"a":1,"b":[1,2]}`)
	b := mustParse(t, `{"b":[1,2],"a":1}`)
	if !Equal(a, b) {
		t.Errorf("order mattered")
	}
}
