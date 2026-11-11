package eval

import (
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/jsonvalue"
)

func mustParse(t *testing.T, s string) jsonvalue.Value {
	t.Helper()
	v, err := jsonvalue.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return v
}

// matchesFlag builds a one-flag ruleset whose only rule is one clause, and returns whether the rule matched.
func matchesFlag(t *testing.T, clauseJSON, contextJSON string) bool {
	t.Helper()
	rs := NewRuleset(mustParse(t, `{"flags":[{"key":"f","on":true,"variations":[false,true],"offVariation":0,"salt":"s","version":1,
		"targets":[],"prerequisites":[],"rules":[{"clauses":[`+clauseJSON+`],"variation":1}],"fallthrough":{"variation":0}}],"segments":[]}`))
	return rs.Evaluate("f", mustParse(t, contextJSON)).Reason.Kind == KindRuleMatch
}

func TestStringOpsOnLoneSurrogates(t *testing.T) {
	// A lone surrogate in the candidate: contains/startsWith/endsWith compare UTF-16 units.
	ctx := `{"key":"k","attributes":{"s":"a\ud800b"}}`
	cases := []struct {
		op, value string
		want      bool
	}{
		{"contains", `"\ud800"`, true},
		{"startsWith", `"a\ud800"`, true},
		{"endsWith", `"\ud800b"`, true},
		{"contains", `"\ud801"`, false},
		{"contains", `"�"`, false},
	}
	for _, c := range cases {
		clause := `{"attribute":"s","op":"` + c.op + `","values":[` + c.value + `]}`
		if got := matchesFlag(t, clause, ctx); got != c.want {
			t.Errorf("%s %s = %v, want %v", c.op, c.value, got, c.want)
		}
	}
	// The comparison is by UTF-16 units, so the high half of a pair is found inside the pair.
	pair := `{"key":"k","attributes":{"s":"😀"}}`
	if !matchesFlag(t, `{"attribute":"s","op":"contains","values":["\ud83d"]}`, pair) {
		t.Error("the high half of a pair is a unit of the pair")
	}
}

func TestPatternLimits(t *testing.T) {
	for pat, valid := range map[string]bool{
		`a{100}`: true,
		`a{101}`: false,
		`a{01}`:  false,
		`[]`:     false,
		`[^]`:    false,
		`(?:a)`:  false,
		`a**`:    false,
		`^*`:     false,
		`a|`:     true,
		`[a-]`:   true,
		`[\d-z]`: false,
		`\q`:     false,
		`(a`:     false,
		`a)`:     false,
	} {
		got := compilePattern(pat) != nil
		if got != valid {
			t.Errorf("compilePattern(%q) valid = %v, want %v", pat, got, valid)
		}
	}
}

func TestMatchesIsNotBacktracking(t *testing.T) {
	p := compilePattern(`(a*)*b`)
	if p == nil {
		t.Fatal("pattern did not compile")
	}
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'a'
	}
	if p.matches(string(long)) {
		t.Error("no b, so no match")
	}
}

func TestSemverAndDates(t *testing.T) {
	if !matchesFlag(t, `{"attribute":"v","op":"semverGt","values":["1.2.3-rc.1"]}`, `{"key":"k","attributes":{"v":"1.2.3"}}`) {
		t.Error("1.2.3 > 1.2.3-rc.1")
	}
	if matchesFlag(t, `{"attribute":"v","op":"semverEq","values":["01.2.3"]}`, `{"key":"k","attributes":{"v":"1.2.3"}}`) {
		t.Error("a leading zero is not a version")
	}
	// 2024-03-01T00:00+02:00 is 2024-02-29T22:00Z: one second earlier is before it, the same instant is not.
	clause := `{"attribute":"d","op":"before","values":["2024-03-01T00:00:00+02:00"]}`
	if !matchesFlag(t, clause, `{"key":"k","attributes":{"d":"2024-02-29T21:59:59Z"}}`) {
		t.Error("21:59:59Z is before 00:00+02:00 the next day")
	}
	if matchesFlag(t, clause, `{"key":"k","attributes":{"d":"2024-02-29T22:00:00Z"}}`) {
		t.Error("equal instants match neither before nor after")
	}
	if matchesFlag(t, `{"attribute":"d","op":"before","values":["2024-02-30T00:00:00Z"]}`, `{"key":"k","attributes":{"d":"2024-01-01T00:00:00Z"}}`) {
		t.Error("Feb 30 is not an instant")
	}
}

func TestNeverPanicsOnJunk(t *testing.T) {
	for _, root := range []string{`null`, `[]`, `{"flags":3}`, `{"flags":[null,1,"x",{"key":3},{"key":"f"}],"segments":{}}`} {
		rs := NewRuleset(mustParse(t, root))
		for _, ctx := range []string{`null`, `{}`, `{"key":"k"}`, `{"kind":"multi","contexts":{}}`} {
			_ = rs.Evaluate("f", mustParse(t, ctx))
		}
	}
}
