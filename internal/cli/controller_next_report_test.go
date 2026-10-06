package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A not-ready preflight offers the one command its service decided, never one
// re-derived from the pending scope: a context with no controller stage is
// sent to its first apply although its pending check is a context's.
func TestANotReadyPreflightOffersTheCommandItsServiceDecided(t *testing.T) {
	for _, next := range []string{"bootwright apply --context lab", "bootwright apply --stage controller --context lab", "bootwright setup"} {
		report := controllerReport("not-ready", false)
		report.ContextName, report.Machine, report.Next = "lab", "controller", next
		report.Checks = append(report.Checks, prerequisites.Check{ID: "controller-binding", Required: "controller", Observed: "missing or unverified", Status: "not-ready", Scope: prerequisites.ContextScope})
		report.Checks[0].Status, report.Checks[0].Observed, report.Checks[0].Scope = "ready", "qualified", prerequisites.HostScope
		record := &dispatchRecord{result: commandResult{controller: report}, err: diagnostics.NewFailureWithRemediation("preflight.failed", "required controller prerequisites are not ready", "", "run "+next)}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"preflight", "controller", "--context", "lab"})
		if code != 1 || !strings.Contains(out.String(), "  Outcome  not-ready\n  Next     "+next+"\n") {
			t.Errorf("next %q: exit %d, out %q", next, code, out.String())
		}
	}
}
