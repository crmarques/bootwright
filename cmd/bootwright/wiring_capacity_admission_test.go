//go:build linux && amd64

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func TestComposedApplyRefusesAtTheLibvirtCapacityGateBeforeRegistration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository := testRepository(root)
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace, deps.Trust = repository, repository, repository
	asked := 0
	deps.Lifecycle = lifecycleDependencies{
		Workspace: repository,
		Inputs:    contexts.Inputs{Repository: repository, Selection: deps.Selection},
		Host:      testLifecycleHost{}, Guard: testLifecycleGuard{}, Selection: deps.Selection,
		Presenter: testLifecyclePresenter{}, Confirmer: deps.Confirmer,
		Capabilities: buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{}),
		Capacity: capacityCheckFunc(func(_ context.Context, got []capacity.Allocation, dry bool) ([]capacity.Check, error) {
			asked++
			if dry || len(got) != 1 || !got[0].Reserve.Configured {
				t.Fatalf("the composed gate lost the declared guest or its reserve: %+v dry %t", got, dry)
			}
			return []capacity.Check{{ID: "capacity.memory", Target: got[0].Host, Status: capacity.Failed, Reason: "declared guests exceed available memory"}}, nil
		}),
	}
	services := assembleServices(deps)
	input := filepath.Join(t.TempDir(), "lab-sno")
	copySources(t, exampleDirectory(t, "lab-sno"), input)
	contextRun(t, services, 0, "context", "init", "--name", "lab-sno", "--input-dir", input)
	before := stateFingerprint(t, root)
	_, stderr := contextRun(t, services, 1, "apply", "--context", "lab-sno", "--yes")
	if asked != 1 || !strings.Contains(stderr, "declared guests exceed available memory") {
		t.Fatalf("the composed apply asked the gate %d times and refused with %s", asked, stderr)
	}
	if !sameFingerprints(before, stateFingerprint(t, root)) {
		t.Fatal("the refused apply registered or changed state")
	}
}
