package jsonv

import (
	"math"
	"strings"
	"testing"
)

func TestParseKeepsTheLastDuplicateKey(t *testing.T) {
	v, err := Parse([]byte(`{"a":1,"b":2,"a":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.Field("a").Number() != 3 || v.Object().Len() != 2 {
		t.Errorf("value = %s", Marshal(v))
	}
}

func TestParseBodySkipsABOM(t *testing.T) {
	if _, err := ParseBody([]byte("\xef\xbb\xbf{}")); err != nil {
		t.Error(err)
	}
	if _, err := Parse([]byte("\xef\xbb\xbf{}")); err == nil {
		t.Error("Parse accepted a BOM")
	}
}

func TestOverflowingNumberIsInfinity(t *testing.T) {
	v, err := Parse([]byte(`[1e999,-1e999]`))
	if err != nil {
		t.Fatal(err)
	}
	if !math.IsInf(v.Items()[0].Number(), 1) || !math.IsInf(v.Items()[1].Number(), -1) {
		t.Errorf("numbers = %v", v.Items())
	}
}

func TestLoneSurrogateEscapeSurvivesParse(t *testing.T) {
	v, err := Parse([]byte(`"\ud83d"`))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.String()) != 3 {
		t.Errorf("string = %q", v.String())
	}
	// A pair is one code point.
	v, err = Parse([]byte(`"😀"`))
	if err != nil || v.String() != "\U0001F600" {
		t.Errorf("pair = %q, %v", v.String(), err)
	}
}

func TestNestingBeyond512IsAccepted(t *testing.T) {
	deep := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	if _, err := Parse([]byte(deep)); err != nil {
		t.Errorf("600 levels: %v", err)
	}
}

func TestRejectsMalformed(t *testing.T) {
	for _, in := range []string{``, `{`, `[1,]`, `{"a":}`, `01`, `1.`, `"\x"`, `nul`, `{"a":1} x`, `'a'`, `[1 2]`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("%q parsed", in)
		}
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	in := `{"a":[1,2.5,"x",null,true],"b":{"c":"\u0001"}}`
	v, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out := Marshal(v)
	w, err := Parse(out)
	if err != nil || !Equal(v, w) {
		t.Errorf("round trip %s -> %s (%v)", in, out, err)
	}
}

func TestMarshalNonFiniteIsNull(t *testing.T) {
	if got := string(Marshal(NewNumber(math.Inf(1)))); got != "null" {
		t.Errorf("inf = %s", got)
	}
}

func TestEqual(t *testing.T) {
	a, _ := Parse([]byte(`{"x":[1,{"y":null}]}`))
	b, _ := Parse([]byte(`{"x":[1,{"y":null}]}`))
	c, _ := Parse([]byte(`{"x":[1,{"y":false}]}`))
	if !Equal(a, b) || Equal(a, c) {
		t.Error("Equal is wrong")
	}
}
