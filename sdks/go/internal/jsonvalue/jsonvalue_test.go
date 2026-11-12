package jsonvalue

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func mustParse(t *testing.T, s string) Value {
	t.Helper()
	v, err := Parse(s)
	if err != nil {
		t.Fatalf("%q: %v", s, err)
	}
	return v
}

func TestNumbers(t *testing.T) {
	if n, _ := mustParse(t, "1e999").AsNumber(); !math.IsInf(n, 1) {
		t.Fatalf("1e999 is %v", n)
	}
	if n, _ := mustParse(t, "-1e999").AsNumber(); !math.IsInf(n, -1) {
		t.Fatalf("-1e999 is %v", n)
	}
	if n, _ := mustParse(t, "-0.0").AsNumber(); !math.Signbit(n) || n != 0 {
		t.Fatalf("-0.0 is %v", n)
	}
	if n, _ := mustParse(t, "9007199254740993").AsNumber(); n != 9007199254740992 {
		t.Fatalf("binary64 rounding: %v", n)
	}
	if n, _ := mustParse(t, "1E-400").AsNumber(); n != 0 {
		t.Fatalf("underflow: %v", n)
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, s := range []string{"", "01", "1.", ".5", "+1", "[1,]", "{\"a\":1,}", "{\"a\"}", "'a'", "tru", "nul", "[", "\"\\x\"", "\"\n\"", "1 2", "\"\\ud800\\u12\""} {
		if _, err := Parse(s); !errors.Is(err, ErrSyntax) {
			t.Errorf("%q: %v", s, err)
		}
	}
}

func TestStrings(t *testing.T) {
	v := mustParse(t, `"a\ud83d\ude00\ud800b\udc00\n\/"`)
	s, _ := v.AsString()
	if s != "a\U0001F600\xed\xa0\x80b\xed\xb0\x80\n/" {
		t.Fatalf("got %q", s)
	}
	out := string(Marshal(v))
	if out != `"a😀\ud800b\udc00\n/"` {
		t.Fatalf("marshal: %s", out)
	}
}

func TestBodyDecoding(t *testing.T) {
	v, err := ParseBytes([]byte("\xef\xbb\xbf{\"a\":\"\xff\"}"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := v.Get("a")
	if s, _ := got.AsString(); s != "\uFFFD" {
		t.Fatalf("got %q", s)
	}
}

func TestDuplicateKeysKeepTheLast(t *testing.T) {
	v := mustParse(t, `{"a":1,"b":2,"a":3}`)
	if a, _ := v.Get("a"); !Equal(a, Number(3)) {
		t.Fatalf("a = %v", a)
	}
	if v.Len() != 2 {
		t.Fatalf("len %d", v.Len())
	}
}

func TestDepthLimit(t *testing.T) {
	ok := strings.Repeat("[", MaxDepth) + strings.Repeat("]", MaxDepth)
	if _, err := Parse(ok); err != nil {
		t.Fatalf("depth %d: %v", MaxDepth, err)
	}
	deep := strings.Repeat("[", MaxDepth+1) + strings.Repeat("]", MaxDepth+1)
	if _, err := Parse(deep); !errors.Is(err, ErrDepth) {
		t.Fatalf("depth %d: %v", MaxDepth+1, err)
	}
}

func TestMarshal(t *testing.T) {
	for _, c := range []struct {
		in   Value
		want string
	}{
		{Number(0), "0"},
		{Number(math.Copysign(0, -1)), "0"},
		{Number(1.5), "1.5"},
		{Number(1e21), "1e+21"},
		{Number(1e-7), "1e-7"},
		{Number(math.Inf(1)), "null"},
		{String("a\"\\\x01"), `"a\"\\\u0001"`},
		{Array([]Value{Bool(true), Null()}), "[true,null]"},
		{Object(Member{Key: "b", Value: Number(1)}, Member{Key: "a", Value: Number(2)}), `{"b":1,"a":2}`},
	} {
		if got := string(Marshal(c.in)); got != c.want {
			t.Errorf("got %s want %s", got, c.want)
		}
	}
}

func TestEqual(t *testing.T) {
	if !Equal(Number(0), Number(math.Copysign(0, -1))) || Equal(Number(math.NaN()), Number(math.NaN())) {
		t.Fatal("IEEE number equality")
	}
	if !Equal(mustParse(t, `{"a":[1,{"b":null}]}`), mustParse(t, `{"a":[1,{"b":null}]}`)) || Equal(Null(), Bool(false)) {
		t.Fatal("deep equality")
	}
}
