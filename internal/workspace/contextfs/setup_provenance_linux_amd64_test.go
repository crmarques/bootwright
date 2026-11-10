//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestSetupPackageIntentGoldenAndUnloggedIdentityRoundTrip(t *testing.T) {
	state := setupReceiptState()
	state.Pending = &prerequisites.PendingSetup{
		ID: "install-" + strings.Repeat("a", 32), PlanDigest: setupDigest(4),
		Actions: []string{"runtime", "receipt"}, StartedAt: "2026-10-14T09:00:00Z", Requested: []string{"podman", "python3.14"},
		Added: []prerequisites.InstalledPackage{{NEVRA: "containers-common-5:0.64.2-1.fc44.noarch", InstallTime: 1760432400}, {NEVRA: "podman-5:5.8.1-1.fc44.x86_64", InstallTime: 1760432400}},
	}
	state.Receipt.Packages[0].InstalledBy = state.Pending.ID
	state.Receipt.Added = []prerequisites.InstalledPackage{{NEVRA: "python3.14-0:3.14.8-1.fc44.x86_64", InstallTime: 1760000000}}
	data, err := encodeSetupState(state)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "setup-package-intent", data, true)
	decoded, foreign := decodeSetupState(data)
	if foreign != "" || decoded.Pending == nil || !slices.Equal(decoded.Pending.Requested, state.Pending.Requested) || decoded.Receipt.Packages[0].InstalledBy != state.Pending.ID ||
		!slices.Equal(decoded.Pending.Added, state.Pending.Added) || !slices.Equal(decoded.Receipt.Added, state.Receipt.Added) {
		t.Fatalf("round trip = %+v (%s)", decoded, foreign)
	}
	state.Pending.Requested[0] = "other"
	if decoded.Pending.Requested[0] != "podman" {
		t.Fatal("aliased pending intent")
	}
}

func TestSetupSequenceExhaustionKeepsReceiptWithoutAStoredRun(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
		_, run, err := tx.OpenRun(context.Background())
		if err != nil {
			return err
		}
		return run.Close()
	})
	root := filepath.Join(store.options.Root+logsSuffix, logController)
	if err := os.Rename(filepath.Join(root, "install-000001.output"), filepath.Join(root, "install-999999.output")); err != nil {
		t.Fatal(err)
	}
	identity, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("1", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
	if err != nil {
		t.Fatal(err)
	}
	python := &healingPython{setup: filepath.Join(store.options.Root, controllerContainer, installSubtree)}
	service := prerequisites.New(healingController{}, healingController{}, healingHost{identity}, prerequisites.Ports{
		Setup: store, Packages: healingPackages{}, Python: python, Lock: healingLock,
	}, prerequisites.Options{Presenter: &healingPlans{}})
	report, err := service.Setup(context.Background(), prerequisites.SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" || report.LogLocation != "" {
		t.Fatalf("unlogged setup = %+v (%v)", report, err)
	}
	view, err := store.ReadSetup(context.Background())
	if err != nil || view.State.Receipt == nil || view.State.Pending != nil {
		t.Fatalf("receipt = %+v (%v)", view, err)
	}
}

func TestSetupRunOperationalCreationFaultsRemainOptional(t *testing.T) {
	for _, fault := range []struct {
		point      string
		occurrence int
		cause      error
		primed     bool
	}{
		{"mkdir", 1, syscall.ENOSPC, false}, {"create-file", 1, syscall.ENOSPC, true},
		{"sync-directory", 1, syscall.ENOSPC, true}, {"sync-directory", 1, syscall.EIO, true},
	} {
		point := fault.point
		t.Run(point+"-"+fault.cause.Error(), func(t *testing.T) {
			store := New(testOptions(filepath.Join(t.TempDir(), "state")))
			if fault.primed {
				mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
					_, run, err := tx.OpenRun(context.Background())
					if err != nil {
						return err
					}
					return run.Close()
				})
			}
			mutateSetup(t, store, func(tx prerequisites.SetupTransaction) error {
				reached := false
				occurrences := 0
				store.fail = func(at string) error {
					if at == point {
						occurrences++
						if occurrences == fault.occurrence {
							reached = true
							return fault.cause
						}
					}
					return nil
				}
				_, run, err := tx.OpenRun(context.Background())
				store.fail = nil
				if run != nil {
					run.Close()
				}
				if !reached || err == nil || errors.Is(err, prerequisites.ErrUnsafeSetupRun) {
					t.Fatalf("operational %s: reached=%v error=%v", point, reached, err)
				}
				return nil
			})
		})
	}
}

func TestSetupRunAccessFailureClassification(t *testing.T) {
	for _, item := range []struct {
		name   string
		err    error
		unsafe bool
	}{
		{"listing unavailable", &listingFailure{listingUnreadable, state("unavailable")}, false},
		{"listing replaced", &listingFailure{listingReplaced, state("changed")}, true},
		{"listing limit", &listingFailure{listingOverLimit, state("too many")}, true},
		{"stat IO", &os.PathError{Op: "fstat", Path: "synthetic", Err: syscall.EIO}, false},
		{"storage IO", storeFailure("unavailable", syscall.EIO), false},
		{"cancel", context.Canceled, false},
		{"link", syscall.ELOOP, true}, {"type", syscall.ENOTDIR, true},
		{"collision", syscall.EEXIST, true}, {"permissions", state("unsafe permissions"), true},
	} {
		t.Run(item.name, func(t *testing.T) {
			err := setupRunAccessError("host/synthetic", item.err)
			if errors.Is(err, prerequisites.ErrUnsafeSetupRun) != item.unsafe {
				t.Fatalf("classification = %v, unsafe want %v", err, item.unsafe)
			}
		})
	}
}

func TestSetupLogInspectionWithUnavailableStatIsOptional(t *testing.T) {
	file, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := statHandle(file)
	if err != nil {
		t.Fatal(err)
	}
	run := &directory{file: file, path: file.Name(), identity: identity}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, inspect := range []func() error{
		func() error { _, err := setupRunNumbers(context.Background(), run); return err },
	} {
		err := inspect()
		if err == nil || errors.Is(err, prerequisites.ErrUnsafeSetupRun) {
			t.Fatalf("unavailable metadata = %v", err)
		}
	}
}
