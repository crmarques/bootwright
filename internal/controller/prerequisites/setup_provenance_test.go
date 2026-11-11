package prerequisites

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestSetupRetainsOriginalPackageProvenanceAcrossProviderUpdates(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.packages.present["python3.14"] = true
	f.packages.present["podman"] = true
	f.python.ready = false
	f.store.view.State.Receipt.Packages = []ReceiptPackage{
		{Requirement: "podman", Provider: "podman-0:1.0-1.x86_64", InstalledBy: "install-000042"},
		{Requirement: "python3.14", Provider: provider("python3.14"), InstalledBy: "install-000042"},
	}
	f.store.view.State.Receipt.Added = []InstalledPackage{{NEVRA: "podman-0:1.0-1.x86_64", InstallTime: 1750000000}, {NEVRA: provider("python3.14"), InstallTime: 1750000000}}
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	owned, err := addedNames(f.store.view)
	if err != nil || !slices.Contains(owned, "podman") || !slices.Contains(owned, "python3.14") {
		t.Fatalf("owned packages = %v, %v; receipt %+v", owned, err, f.store.view.State.Receipt.Packages)
	}
	for _, entry := range f.store.view.State.Receipt.Packages {
		if entry.Requirement != "openssh-clients" && entry.InstalledBy != "install-000042" {
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
	f.store.runID = "install-000044"
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	owned, err := addedNames(f.store.view)
	if err != nil || !slices.Contains(owned, "openssh-clients") {
		t.Fatalf("resumed owned packages = %v (%v)", owned, err)
	}
	for _, item := range f.store.view.State.Receipt.Packages {
		if item.Requirement == "openssh-clients" && item.InstalledBy != "install-000043" {
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
	owned, err := addedNames(f.store.view)
	if err != nil || !slices.Contains(owned, "openssh-clients") {
		t.Fatalf("unlogged receipt lost ownership: %v (%v)", owned, err)
	}
	for _, item := range f.store.view.State.Receipt.Packages {
		if item.Requirement == "openssh-clients" && item.InstalledBy != "install-"+strings.Repeat("a", 32) {
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
	f.store.runID, f.store.runNumber = "install-"+strings.Repeat("b", 32), 57
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil {
		t.Fatal(err)
	}
	const path = "/var/log/bootwright/controller/install-000057.output"
	if report.LogLocation != path || report.SetupRun != "install-000057" {
		t.Fatalf("report names %q and %q, want %q and install-000057", report.LogLocation, report.SetupRun, path)
	}
	if want := (LogLocation{Path: path, Setup: "install-000057"}); len(named.located) != 1 || named.located[0] != want {
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

func addedNames(view SetupView) ([]string, error) {
	added, _, err := receiptNames(view, false)
	names := []string{}
	for _, item := range added {
		name, _ := providerName(item.NEVRA)
		names = append(names, name)
	}
	return names, err
}

// The receipt records what the install transaction added, a dependency it
// pulled in included, and never a package already on the host.
func TestTheReceiptRecordsWhatTheInstallTransactionAdded(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.packages.present["libnet"] = true
	f.packages.pulls = map[string][]string{"openssh-clients": {"libfido2"}}
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	added, err := addedNames(f.store.view)
	if err != nil || !slices.Equal(added, []string{"libfido2", "openssh-clients"}) {
		t.Fatalf("added = %q (%v)", added, err)
	}
}

// A package step that fails after dnf ran leaves the identities the host held
// before the transaction in the pending intent, so the resumed install still
// records what that transaction added.
func TestASetupThatFailsAfterDnfResumesWithItsAddedSet(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.packages.present["libnet"] = true
	f.packages.pulls = map[string][]string{"openssh-clients": {"libfido2"}}
	f.packages.broken = errors.New("injected rpm failure after dnf")
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); !errors.Is(err, f.packages.broken) {
		t.Fatalf("setup = %v", err)
	}
	pending := f.store.view.State.Pending
	if pending == nil || pending.Transaction == nil || !slices.Contains(pending.Transaction.Held, "libnet.x86_64") || slices.Contains(pending.Transaction.Held, "openssh-clients.x86_64") {
		t.Fatalf("pending intent = %+v", pending)
	}
	f.packages.broken = nil
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	added, err := addedNames(f.store.view)
	if err != nil || !slices.Equal(added, []string{"libfido2", "openssh-clients"}) {
		t.Fatalf("resumed added = %q (%v)", added, err)
	}
}

// A carried set keeps only the members still installed with the install time
// the transaction recorded: one removed, or removed and installed again, drops.
func TestACarriedAddedSetDropsWhatIsGone(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.python.ready = false
	f.packages.present["podman"], f.packages.present["buildah"] = true, true
	f.packages.times = map[string]int64{"buildah": 1755000000}
	f.store.view.State.Receipt.Added = MergeAdded([]InstalledPackage{
		{NEVRA: provider("podman"), InstallTime: 1750000000}, {NEVRA: provider("buildah"), InstallTime: 1750000000}, {NEVRA: provider("toolbox"), InstallTime: 1750000000},
	})
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	added, err := addedNames(f.store.view)
	if err != nil || !slices.Equal(added, []string{"podman"}) {
		t.Fatalf("carried added = %q (%v)", added, err)
	}
}

// An added set past its bound refuses with a remedy that empties it.
func TestTheAddedSetBoundNamesARemedy(t *testing.T) {
	f := newSetupFixture(t)
	f.prepared()
	f.python.ready = false
	added := []InstalledPackage{}
	for index := range MaxAddedPackages + 1 {
		name := fmt.Sprintf("package%04d", index)
		f.packages.present[name] = true
		added = append(added, InstalledPackage{NEVRA: provider(name), InstallTime: 1750000000})
	}
	f.store.view.State.Receipt.Added = MergeAdded(added)
	_, err := f.service.Setup(context.Background(), SetupRequest{})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || !strings.Contains(reported[0].Message, "more than 1024 packages") || !strings.Contains(reported[0].Remediation, "run bootwright controller uninstall") {
		t.Fatalf("bound refusal = %#v", reported)
	}
}

// A resumed install ends the transaction's window at dnf's own deadline, so a
// package another tool installed after it, before the install ran again, is
// not counted as added.
func TestAResumedTransactionWindowEndsAtTheInstallDeadline(t *testing.T) {
	f := newSetupFixture(t)
	f.packages.present["openssh-clients"] = false
	f.packages.broken = errors.New("injected crash after dnf")
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); !errors.Is(err, f.packages.broken) {
		t.Fatalf("setup = %v", err)
	}
	start := f.store.view.State.Pending.Transaction.Start
	f.packages.broken = nil
	f.packages.present["toolbox"] = true
	f.packages.times["toolbox"] = start + int64(2*InstallDeadline/time.Second)
	f.service.options.Now = func() time.Time { return time.Unix(start, 0).Add(72 * time.Hour) }
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	added, err := addedNames(f.store.view)
	if err != nil || !slices.Equal(added, []string{"openssh-clients"}) {
		t.Fatalf("resumed added = %q (%v)", added, err)
	}
}
