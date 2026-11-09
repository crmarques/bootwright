package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestSetupRetainsOriginalPackageProvenanceAcrossProviderUpdates(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.packages.present["python3.14"] = true
	f.packages.present["podman"] = true
	f.python.ready = false
	f.store.view.State.Receipt.Packages = []ReceiptPackage{
		{Requirement: "podman", Provider: "podman-0:1.0-1.x86_64", InstalledBy: "setup-000042"},
		{Requirement: "python3.14", Provider: provider("python3.14"), InstalledBy: "setup-000042"},
	}
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	owned, _, err := receiptNames(f.store.view, false)
	if err != nil || !slices.Contains(owned, "podman") || !slices.Contains(owned, "python3.14") {
		t.Fatalf("owned packages = %v, %v; receipt %+v", owned, err, f.store.view.State.Receipt.Packages)
	}
	for _, entry := range f.store.view.State.Receipt.Packages {
		if entry.Requirement != "openssh-clients" && entry.InstalledBy != "setup-000042" {
			t.Fatalf("attribution changed: %+v", entry)
		}
	}
}

func TestInterruptedSetupKeepsTheOriginalPackageIntentUntilReceipt(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.python.ready = false
	f.python.buildErr = errors.New("injected build failure")
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err == nil {
		t.Fatal("missing failure")
	}
	if f.store.view.State.Pending == nil {
		t.Fatal("lost intent")
	}
	f.python.buildErr = nil
	f.store.runID = "setup-000044"
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	owned, _, err := receiptNames(f.store.view, false)
	if err != nil || !slices.Contains(owned, "openssh-clients") {
		t.Fatalf("resumed owned packages = %v (%v)", owned, err)
	}
	for _, item := range f.store.view.State.Receipt.Packages {
		if item.Requirement == "openssh-clients" && item.InstalledBy != "setup-000043" {
			t.Fatalf("lost original run: %+v", item)
		}
	}
}

func TestSetupCanDiscardUnavailableRunOutputWithoutLosingReceipt(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.store.runError = errors.New("injected run creation, rotation or exhaustion failure")
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" || report.LogLocation != "" {
		t.Fatalf("setup = %+v (%v)", report, err)
	}
	owned, _, err := receiptNames(f.store.view, false)
	if err != nil || !slices.Contains(owned, "openssh-clients") {
		t.Fatalf("unlogged receipt lost ownership: %v (%v)", owned, err)
	}
	for _, item := range f.store.view.State.Receipt.Packages {
		if item.Requirement == "openssh-clients" && item.InstalledBy != "setup-"+strings.Repeat("a", 32) {
			t.Fatalf("injected identity was lost: %+v", item)
		}
	}
}

func TestUnsafeRunStateStillRefusesBeforePackages(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.store.runError = ErrUnsafeSetupRun
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); !errors.Is(err, ErrUnsafeSetupRun) {
		t.Fatalf("unsafe run = %v", err)
	}
	if f.packages.present["openssh-clients"] {
		t.Fatal("installed through unsafe run state")
	}
}

func TestSetupRunCleanupFailureNeverChangesReceipt(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.store.runCloseError = errors.New("injected cleanup failure")
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" || f.store.view.State.Receipt == nil {
		t.Fatalf("setup = %+v (%v)", report, err)
	}
}

func TestUnavailableSetupIdentityRefusesBeforePackageOrStateEffects(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.store.runError = errors.New("logging unavailable")
	expected := errors.New("identity unavailable")
	f.store.setupIDError = expected
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); !errors.Is(err, expected) {
		t.Fatalf("identity failure = %v", err)
	}
	if f.packages.present["openssh-clients"] || len(f.store.recorded) != 0 {
		t.Fatal("identity failure reached effects")
	}
}

type namedRuns struct{ located []LogLocation }

func (n *namedRuns) ReportProgress(context.Context, ProgressEvent) {}
func (n *namedRuns) ReportLogLocation(_ context.Context, location LogLocation) {
	n.located = append(n.located, location)
}

// A resumed setup keeps the ID of the setup it resumes, but its output file
// carries the number the run area allocated, so the run an operator reads back
// is named by the file and not by the ID, in the report and in the progress
// that precedes the work.
func TestSetupReportsItsRunID(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	named := &namedRuns{}
	f.service.options.Progress = named
	f.store.runID, f.store.runNumber = "setup-"+strings.Repeat("b", 32), 57
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil {
		t.Fatal(err)
	}
	const path = "/var/log/bootwright/host/setup-000057.output"
	if report.LogLocation != path || report.SetupRun != "setup-000057" {
		t.Fatalf("report names %q and %q, want %q and setup-000057", report.LogLocation, report.SetupRun, path)
	}
	if want := (LogLocation{Path: path, Setup: "setup-000057"}); len(named.located) != 1 || named.located[0] != want {
		t.Fatalf("progress named %+v, want %+v", named.located, want)
	}
	f = newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.store.runError = errors.New("no run output")
	report, err = f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.SetupRun != "" || report.LogLocation != "" {
		t.Fatalf("a setup that retained nothing names %q (%v)", report.SetupRun, err)
	}
}
