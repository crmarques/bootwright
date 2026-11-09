package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A not-ready controller check offers the one command its service decided,
// never one re-derived from the pending scope: a host whose pending check is a
// context's is sent to the command the service named.
func TestANotReadyControllerCheckOffersTheCommandItsServiceDecided(t *testing.T) {
	for _, next := range []string{"bootwright apply --context lab", "bootwright apply --stage controller --context lab", "bootwright controller install"} {
		report := controllerReport("not-ready", false)
		report.Next = next
		report.Checks = append(report.Checks, prerequisites.Check{ID: "setup-state", Required: "complete", Observed: "missing or unverified", Status: "not-ready", Scope: prerequisites.HostScope})
		report.Checks[0].Status, report.Checks[0].Observed, report.Checks[0].Scope = "ready", "qualified", prerequisites.HostScope
		record := &dispatchRecord{result: commandResult{controller: report}, err: diagnostics.NewFailureWithRemediation("preflight.failed", "required controller prerequisites are not ready", "", "run "+next)}
		var out, errOut bytes.Buffer
		code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"controller", "check"})
		if code != 1 || !strings.Contains(out.String(), "    Outcome  not-ready\n    Next     "+next+"\n") {
			t.Errorf("next %q: exit %d, out %q", next, code, out.String())
		}
	}
}
