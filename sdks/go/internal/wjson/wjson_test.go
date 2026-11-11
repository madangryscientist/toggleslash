package wjson

import (
	"errors"
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

func TestParseBasics(t *testing.T) {
	v := mustParse(t, ` {"a": [1, 2.5, -0, 1e3, true, null, "x"], "b": {"c": "d"}} `)
	a, _ := v.Object().Get("a")
	if len(a.Array()) != 7 || a.Array()[1].Number() != 2.5 || a.Array()[3].Number() != 1000 {
		t.Errorf("array: %s", Marshal(a))
	}
	if got := string(Marshal(v)); got != `{"a":[1,2.5,-0,1000,true,null,"x"],"b":{"c":"d"}}` {
		t.Errorf("Marshal = %s", got)
	}
}

func TestDuplicateKeyLastWinsKeepsFirstPosition(t *testing.T) {
	v := mustParse(t, `{"a":1,"b":2,"a":3}`)
	if got := string(Marshal(v)); got != `{"a":3,"b":2}` {
		t.Errorf("got %s", got)
	}
}

func TestParseRejects(t *testing.T) {
	for _, text := range []string{``, `{`, `[1,]`, `{"a":}`, `01`, `1.`, `.5`, `"a`, `"\x"`, `tru`, `[1] 2`, "\"a\nb\"", `{'a':1}`, `+1`, `NaN`} {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("Parse(%q) should fail", text)
		}
	}
}

func TestBOMAndInvalidUTF8(t *testing.T) {
	v, err := Parse([]byte("\xEF\xBB\xBF" + `{"a":"x` + "\xFF" + `y"}`))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := v.Object().Get("a")
	if a.String() != "x�y" {
		t.Errorf("a = %q", a.String())
	}
}

func TestSurrogateEscapes(t *testing.T) {
	pair := mustParse(t, `"😀"`)
	if pair.String() != "\U0001F600" {
		t.Errorf("pair = %q", pair.String())
	}
	lone := mustParse(t, `"\ud83d"`)
	if lone.String() != "\xED\xA0\xBD" {
		t.Errorf("lone = %q", lone.String())
	}
	if got := string(Marshal(lone)); got != `"\ud83d"` {
		t.Errorf("Marshal(lone) = %s", got)
	}
}

func TestNumbers(t *testing.T) {
	if v := mustParse(t, `1e999`); !math.IsInf(v.Number(), 1) {
		t.Errorf("1e999 = %v", v.Number())
	}
	if got := string(Marshal(Number(math.Inf(1)))); got != "null" {
		t.Errorf("Marshal(+Inf) = %s", got)
	}
	// The shortest form that reads back as the same double, as JavaScript writes it.
	text := string(Marshal(Number(9223372036854774784)))
	if text != "9223372036854775000" || mustParse(t, text).Number() != 9223372036854774784 {
		t.Errorf("Marshal(2^63-1024) = %s", text)
	}
}

func TestDepth(t *testing.T) {
	ok := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	if _, err := Parse([]byte(ok)); err != nil {
		t.Errorf("depth 600 should parse: %v", err)
	}
	deep := strings.Repeat("[", MaxDepth+2) + strings.Repeat("]", MaxDepth+2)
	if _, err := Parse([]byte(deep)); !errors.Is(err, ErrDepth) {
		t.Errorf("depth %d: %v", MaxDepth+2, err)
	}
}

func TestEqual(t *testing.T) {
	if !mustParse(t, `{"a":1,"b":[1,2]}`).Equal(mustParse(t, `{"b":[1,2],"a":1.0}`)) {
		t.Error("objects should be equal regardless of order")
	}
	if mustParse(t, `[1,2]`).Equal(mustParse(t, `[2,1]`)) || mustParse(t, `1`).Equal(mustParse(t, `"1"`)) {
		t.Error("should differ")
	}
}
