package toggleslash

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

func TestContextConstructors(t *testing.T) {
	got := NewContext("u1", map[string]any{"plan": "pro", "n": 3, "tags": []string{"a", "b"}, "ok": true, "x": nil})
	want := `{"key":"u1","attributes":{"n":3,"ok":true,"plan":"pro","tags":["a","b"],"x":null}}`
	if got.String() != want {
		t.Fatalf("got %s", got)
	}
	k := NewContextKind("org", "o1", nil)
	if k.String() != `{"kind":"org","key":"o1"}` {
		t.Fatalf("got %s", k)
	}
	m := NewMultiContext(map[string]Context{"user": NewContext("u", nil), "org": NewContextKind("org", "o", map[string]any{"a": 1})})
	if m.String() != `{"kind":"multi","contexts":{"org":{"key":"o","attributes":{"a":1}},"user":{"key":"u"}}}` {
		t.Fatalf("got %s", m)
	}
	if !ParseContext([]byte(`{"key":`)).IsNull() {
		t.Fatal("invalid text should give null")
	}
	if ParseContext([]byte(`{"key":"u"}`)).String() != `{"key":"u"}` {
		t.Fatal("parse")
	}
}

func TestValueOf(t *testing.T) {
	if v := ValueOf(int64(math.MaxInt64)); v.Kind() != KindNumber {
		t.Fatal("int64")
	}
	if v := ValueOf(struct{}{}); !v.IsNull() {
		t.Fatal("unsupported type should be null")
	}
	if v := ValueOf(json.Number("1.5")); v.String() != "1.5" {
		t.Fatalf("json.Number: %s", v)
	}
	if v := ValueOf("a\xffb"); v.String() != "\"a�b\"" {
		t.Fatalf("invalid utf8: %s", v)
	}
	if v := ValueOf(map[string]string{"b": "2", "a": "1"}); v.String() != `{"a":"1","b":"2"}` {
		t.Fatalf("map: %s", v)
	}
}

func TestNumericAttributeWidthsAllReachTheEvaluator(t *testing.T) {
	vals := []any{int(1), int8(1), int16(1), int32(1), int64(1), uint(1), uint8(1), uint16(1), uint32(1), uint64(1), float32(1), float64(1)}
	for _, v := range vals {
		got := ValueOf(v)
		f, ok := got.AsNumber()
		if !ok || f != 1 {
			t.Errorf("%T: %v", v, got)
		}
	}
}

func TestNumericAttributeMatches(t *testing.T) {
	flag := `{"key":"f","variations":[false,true],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[{"clauses":[{"attribute":"age","op":"gte","values":[18]}],"variation":1}],"fallthrough":{"variation":0},"salt":"s","version":1}`
	s := newFakeServer(t, flag)
	c := newTestClient(t, s, WithStream(false))
	c.Ready()
	for _, age := range []any{int(20), int64(20), uint8(20), float32(20), float64(20.5), json.Number("20")} {
		if !c.BoolVariation("f", NewContext("u", map[string]any{"age": age}), false) {
			t.Errorf("%T did not match", age)
		}
	}
	if c.BoolVariation("f", NewContext("u", map[string]any{"age": 17}), false) {
		t.Error("17 matched")
	}
	_ = jsonvalue.Null()
}
