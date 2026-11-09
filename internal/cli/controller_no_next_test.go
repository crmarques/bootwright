package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A not-ready controller check whose service decided no command, as for a libvirt
// selection on RHEL that only another controller settles, offers none: its
// refusal names its own remedy, and setup would settle nothing.
func TestANotReadyControllerCheckWithoutACommandOffersNone(t *testing.T) {
	report := controllerReport("not-ready", false)
	report.Checks[0].Status, report.Checks[0].Observed, report.Checks[0].Scope = "ready", "qualified", prerequisites.HostScope
	report.Checks = append(report.Checks, prerequisites.Check{ID: "libvirt-client", Required: "libvirt client", Observed: "unsupported on rhel 9.8", Status: "not-ready", Scope: prerequisites.HostScope})
	record := &dispatchRecord{result: commandResult{controller: report}, err: diagnostics.NewFailureWithRemediation("controller.unsupported", "RHEL libvirt requires an authenticated AppStream source adapter", "",
		"Use a Fedora controller for libvirt preparation until RHEL AppStream credential acquisition is configured.")}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"controller", "check"})
	if code != 1 || !strings.Contains(out.String(), "    Outcome  not-ready\n") || strings.Contains(out.String(), "Next") {
		t.Fatalf("exit %d, out %q", code, out.String())
	}
	if !strings.Contains(out.String(), "Libvirt client") || !strings.Contains(errOut.String(), "Use a Fedora controller") {
		t.Fatalf("out %q, errOut %q", out.String(), errOut.String())
	}
}
