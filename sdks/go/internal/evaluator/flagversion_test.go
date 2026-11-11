package evaluator

import (
	"testing"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonvalue"
)

func TestFlagVersion(t *testing.T) {
	body := `{"flags":[
	 {"key":"ok","version":7,"on":true,"salt":"s","variations":[true],"offVariation":0,"targets":[],"prerequisites":[],"rules":[],"fallthrough":{"variation":0}},
	 {"key":"broken","version":9,"on":"yes"}],"segments":[]}`
	v, err := jsonvalue.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	rs := New(v)
	if got, ok := rs.FlagVersion("ok"); !ok || got != 7 {
		t.Errorf("ok: %v %v", got, ok)
	}
	if _, ok := rs.FlagVersion("broken"); ok {
		t.Error("a malformed flag has no version")
	}
	if _, ok := rs.FlagVersion("absent"); ok {
		t.Error("an absent flag has no version")
	}
}
