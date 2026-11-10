package jsonvalue

import (
	"math"
	"testing"
)

func mustParse(t *testing.T, s string) Value {
	t.Helper()
	v, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func TestNumbersAreBinary64(t *testing.T) {
	if n, _ := mustParse(t, "1e999").Number(); !math.IsInf(n, 1) {
		t.Errorf("1e999 = %v, want +Inf", n)
	}
	if n, _ := mustParse(t, "-1e999").Number(); !math.IsInf(n, -1) {
		t.Errorf("-1e999 = %v, want -Inf", n)
	}
	if n, _ := mustParse(t, "9007199254740993").Number(); n != 9007199254740992 {
		t.Errorf("2^53+1 = %v", n)
	}
	if n, _ := mustParse(t, "-0.0").Number(); !math.Signbit(n) {
		t.Error("-0.0 lost its sign")
	}
	if !mustParse(t, "1.0").IsCount() || mustParse(t, "1.5").IsCount() || mustParse(t, "-1").IsCount() {
		t.Error("IsCount")
	}
}

func TestStrings(t *testing.T) {
	if s, _ := mustParse(t, `"😀"`).Text(); s != "\U0001F600" {
		t.Errorf("pair = %q", s)
	}
	lone, _ := mustParse(t, `"\ud83d"`).Text()
	if len(lone) != 3 {
		t.Errorf("lone surrogate = %q", lone)
	}
	if got := string(mustParse(t, `"\ud83d"`).AppendJSON(nil)); got != `"\ud83d"` {
		t.Errorf("round trip = %s", got)
	}
	if s, _ := mustParse(t, "\"a\xffb\"").Text(); s != "a�b" {
		t.Errorf("invalid utf-8 = %q", s)
	}
}

func TestObjects(t *testing.T) {
	v := mustParse(t, `{"a":1,"b":2,"a":3}`)
	if n, _ := v.Field("a").Number(); n != 3 {
		t.Errorf("last duplicate wins: %v", n)
	}
	if keys := v.Keys(); len(keys) != 2 || keys[0] != "a" {
		t.Errorf("keys = %v", keys)
	}
	if _, ok := v.Get("toString"); ok {
		t.Error("only own members")
	}
	if !v.Field("missing").IsNull() {
		t.Error("missing is null")
	}
}

func TestRejectsBadJSON(t *testing.T) {
	for _, s := range []string{"", "{", `{"a":}`, "[1,]", "01", "1.", "nul", `"\x"`, "1 2", "\"a\nb\"", "[" + string(make([]byte, 0)) + "1"} {
		if _, err := Parse([]byte(s)); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
	deep := make([]byte, 0, 4000)
	for range 2000 {
		deep = append(deep, '[')
	}
	if _, err := Parse(deep); err == nil {
		t.Error("deep nesting parsed")
	}
}

func TestWriteIsParseable(t *testing.T) {
	in := `{"a":[1,2.5,-0,1e999,"x\n\u0001"],"b":{"c":null,"d":true}}`
	v := mustParse(t, in)
	again := mustParse(t, string(v.AppendJSON(nil)))
	if !Equal(v, again) {
		t.Errorf("round trip changed %s", v.AppendJSON(nil))
	}
}
