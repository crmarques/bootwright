package prerequisites

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func recordedPackages() []ReceiptPackage {
	return []ReceiptPackage{
		{Requirement: "podman", RequiredBy: []string{"setup"}, Provider: "podman-5:5.8.1-1.fc44", Repository: "updates", InstalledBy: "setup-000042"},
		{Requirement: "openssh-clients", RequiredBy: []string{"setup"}, Provider: "openssh-clients-10.2p1-1.fc44", Repository: "fedora", InstalledBy: PresentBeforeBootwright},
	}
}

func TestTheReadyPathRendersTheReceiptTable(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.store.view.State.Receipt.Packages = recordedPackages()
	want := ReceiptTable{
		Host:     "Fedora Linux 44 (amd64), glibc 2.43 — tested",
		Runtime:  "Python 3.14.8 (/usr/bin/python3.14), ansible-core 2.21.5 — ready",
		Packages: "podman 5:5.8.1-1.fc44 (updates), openssh-clients 10.2p1-1.fc44 (present before bootwright)",
	}
	setup, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || setup.Outcome != "unchanged" || setup.Table == nil || *setup.Table != want {
		t.Fatalf("Setup = %+v, %v", setup, err)
	}
	check, err := f.service.Check(context.Background(), CheckRequest{})
	if err != nil || check.Outcome != "ready" || check.Table == nil || *check.Table != want {
		t.Fatalf("Check = %+v, %v", check, err)
	}
}

func TestAnUntestedReleaseShowsInTheTable(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.host.release.VersionID = "45"
	report, err := f.service.Check(context.Background(), CheckRequest{})
	if err != nil || report.Table == nil || report.Table.Host != "Fedora Linux 45 (amd64), glibc 2.43 — untested" {
		t.Fatalf("Check = %+v, %v", report, err)
	}
}

func TestPreflightIsReadOnlyAndNamesWhatIsMissing(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.python.ready = false
	f.packages.present["podman"] = false
	report, err := f.service.Check(context.Background(), CheckRequest{})
	if codeOf(err) != "preflight.failed" || report == nil || report.Outcome != "not-ready" || report.Next != setupInvocation {
		t.Fatalf("Check = %+v, %v", report, err)
	}
	if report.Table == nil || report.Table.Packages != "missing podman" || report.Table.Runtime != "Python 3.14.8 (/usr/bin/python3.14), ansible-core 2.21.5 — not prepared" {
		t.Fatalf("table = %+v", report.Table)
	}
	if effects := f.effects(); len(effects) != 0 {
		t.Fatalf("preflight ran %v", f.events)
	}
}

func TestADryRunShowsNoTable(t *testing.T) {
	f := newSetupFixture(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true})
	if err != nil || report.Table != nil {
		t.Fatalf("dry run = %+v, %v", report, err)
	}
}

func TestALoaderPreloadIsInformationalEverywhere(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.host.preload = true
	run := map[string]func() (*Report, error){
		"setup":     func() (*Report, error) { return f.service.Setup(context.Background(), SetupRequest{}) },
		"dry run":   func() (*Report, error) { return f.service.Setup(context.Background(), SetupRequest{DryRun: true}) },
		"preflight": func() (*Report, error) { return f.service.Check(context.Background(), CheckRequest{}) },
	}
	for name, call := range run {
		report, err := call()
		if err != nil || report == nil {
			t.Fatalf("%s = %+v, %v", name, report, err)
		}
		found := slices.ContainsFunc(report.Warnings, func(warning diagnostics.Diagnostic) bool {
			return warning.Code == "controller.loader-preload" && warning.Severity == "warning"
		})
		if !found || len(report.Warnings) != 1 {
			t.Fatalf("%s warnings = %+v", name, report.Warnings)
		}
	}
	f.host.preload = false
	if report, err := f.service.Check(context.Background(), CheckRequest{}); err != nil || len(report.Warnings) != 0 {
		t.Fatalf("an empty preload warned: %+v, %v", report, err)
	}
}
