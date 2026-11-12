package evaluator

import (
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

func rulesetFrom(t *testing.T, text string) *Ruleset {
	t.Helper()
	v, err := jsonvalue.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return NewRuleset(v)
}

const goodFlag = `{"key":"f","variations":[false,true],"on":true,"offVariation":0,"targets":[],"prerequisites":[],"rules":[%s],"fallthrough":{"variation":1},"salt":"s","version":%s}`

func flagText(rules, version string) string {
	out := goodFlag
	out = replaceOnce(out, "%s", rules)
	return replaceOnce(out, "%s", version)
}

func replaceOnce(s, old, with string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + with + s[i+len(old):]
		}
	}
	return s
}

func TestVersion(t *testing.T) {
	for _, c := range []struct {
		name  string
		input string
		want  float64
		ok    bool
	}{
		{"valid", `{"flags":[` + flagText("", "7") + `],"segments":[]}`, 7, true},
		{"fraction is malformed", `{"flags":[` + flagText("", "1.5") + `],"segments":[]}`, 0, false},
		{"bad rule is malformed", `{"flags":[` + flagText(`{"clauses":[],"variation":9}`, "3") + `],"segments":[]}`, 0, false},
		{"missing segment is malformed", `{"flags":[` + flagText(`{"clauses":[{"op":"segmentMatch","values":["x"]}],"variation":0}`, "3") + `],"segments":[]}`, 0, false},
		{"cycle keeps its version", `{"flags":[{"key":"f","variations":[false,true],"on":true,"offVariation":0,"targets":[],"prerequisites":[{"flagKey":"f","variation":1}],"rules":[],"fallthrough":{"variation":1},"salt":"s","version":4}],"segments":[]}`, 4, true},
	} {
		got, ok := rulesetFrom(t, c.input).Version("f")
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %v %v", c.name, got, ok)
		}
	}
	if _, ok := rulesetFrom(t, `[]`).Version("f"); ok {
		t.Error("a ruleset that is not an object has no flags")
	}
}

func TestFirstEntryWins(t *testing.T) {
	rs := rulesetFrom(t, `{"flags":[{"key":"f"},`+flagText("", "2")+`],"segments":[]}`)
	if _, ok := rs.Version("f"); ok {
		t.Fatal("the first entry with a key wins, even when it is malformed")
	}
	if rs.Len() != 1 {
		t.Fatalf("len %d", rs.Len())
	}
}
