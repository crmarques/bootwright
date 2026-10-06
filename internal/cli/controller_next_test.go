package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// A context whose readiness holds moves on to its plan. A ready host with no
// context names no next command, because the context to plan is the
// operator's to choose.
func TestReadyReadinessNamesThePlanOnlyForAContext(t *testing.T) {
	for _, test := range []struct {
		args []string
		name string
		want string
	}{
		{args: []string{"preflight", "controller"}},
		{args: []string{"preflight", "controller", "--context", "lab"}, name: "lab", want: "  Next     bootwright plan --context lab\n"},
	} {
		report := controllerReport("ready", false)
		report.Actions = nil
		if test.name != "" {
			report.ContextName, report.Machine = test.name, "controller"
		}
		record := &dispatchRecord{result: commandResult{controller: report}}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), test.args)
		if code != 0 || errOut.Len() != 0 {
			t.Fatalf("%v: exit %d, stderr %q", test.args, code, errOut.String())
		}
		if !strings.HasSuffix(out.String(), "  Outcome  ready\n"+test.want) {
			t.Errorf("%v ended %q, want the outcome followed by %q", test.args, out.String(), test.want)
		}
	}
}
