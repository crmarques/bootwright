package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func tableReport(outcome string) *prerequisites.Report {
	report := controllerReport(outcome, false)
	report.Actions = nil
	report.Table = &prerequisites.ReceiptTable{
		Host:     "Fedora Linux 44 (amd64), glibc 2.43 — tested",
		Runtime:  "Python 3.14.8 (/usr/bin/python3.14), ansible-core 2.21.5 — ready",
		Packages: "podman 5:5.8.1-1.fc44 (updates)",
	}
	return report
}

func runControllerResult(t *testing.T, report *prerequisites.Report, failure error, args ...string) string {
	t.Helper()
	record := &dispatchRecord{result: commandResult{controller: report}, err: failure}
	var out bytes.Buffer
	New(Config{Out: &out, Services: dispatchSpies(record)}).Run(context.Background(), args)
	return out.String()
}

// A streamed ready setup has already shown each check, so its result adds the
// table and the nothing-to-do line and no second list of checks.
func TestAReadySetupStreamedItsChecksAndEndsWithTheTable(t *testing.T) {
	report := tableReport("unchanged")
	report.ProgressPresented = true
	want := "\n  Host      Fedora Linux 44 (amd64), glibc 2.43 — tested\n  Runtime   Python 3.14.8 (/usr/bin/python3.14), ansible-core 2.21.5 — ready\n" +
		"  Packages  podman 5:5.8.1-1.fc44 (updates)\n  Nothing to do.\n\n  Outcome  unchanged\n  Next     bootwright preflight controller\n"
	if got := runControllerResult(t, report, nil, "setup"); got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
}

// A not-ready preflight shows the table with what is missing, keeps the host
// check that is not ready and plans nothing.
func TestANotReadyPreflightShowsTheTableAndNoPlan(t *testing.T) {
	report := tableReport("not-ready")
	report.Table.Packages = "missing podman"
	report.Table.Runtime = "Python 3.14.8 (/usr/bin/python3.14), ansible-core 2.21.5 — not prepared"
	report.Next = "bootwright setup"
	got := runControllerResult(t, report, diagnostics.NewFailureWithRemediation("preflight.failed", "required controller prerequisites are not ready", "", "run bootwright setup"), "preflight", "controller")
	for _, want := range []string{"  Packages  missing podman\n", "  Outcome  not-ready\n", "[FAIL]  Controller runtime"} {
		if !strings.Contains(got, want) {
			t.Fatalf("result %q lacks %q", got, want)
		}
	}
	for _, unwanted := range []string{"Planned changes", "Nothing to do.", "[OK]"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("result %q holds %q", got, unwanted)
		}
	}
}

// Setup's plan and its changed result keep the check rows: the table is the
// ready path's alone.
func TestASetupThatChangedTheHostShowsNoTable(t *testing.T) {
	report := tableReport("changed")
	report.PlanPresented, report.Actions = true, []string{"Install host packages with dnf: podman"}
	if got := runControllerResult(t, report, nil, "setup", "--yes"); strings.Contains(got, "Packages") || strings.Contains(got, "Nothing to do.") {
		t.Fatalf("result = %q", got)
	}
}
