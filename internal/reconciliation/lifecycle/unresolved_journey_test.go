package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

const lostMachine = "machine-rhel-01"

// lostMachineRequest is the frozen request of one libvirt machine on a
// provider host reached over SSH.
func lostMachineRequest() libvirt.MachineRequest {
	return libvirt.MachineRequest{
		Version:  "machine-libvirt-v1",
		Identity: libvirt.Identity{Block: lostMachine, Context: lifecycle.JourneyContext, Object: "rhel-01"},
		Controller: libvirt.Controller{
			Address: "192.0.2.1", CredentialsRef: "lab-bmc-credentials", Endpoint: "http://192.0.2.1:8000/redfish/v1/Systems/5c1d3a52-0b3e-4f6a-9d2c-7e8f9a0b1c2d",
			Image: "quay.io/metal3-io/sushy-tools@sha256:" + strings.Repeat("e", 64), Port: 8000, Unit: "bootwright-bmc-lab-rhel-01",
		},
		Directory: "/var/lib/bootwright/machines/lab/rhel-01",
		Disks:     []libvirt.Disk{{Name: "root", Path: "/var/lib/bootwright/machines/lab/rhel-01/root.qcow2", SizeGiB: 60, Target: "vda"}},
		Domain:    "bootwright-lab-rhel-01",
		MemoryMiB: 8192,
		Placement: machineref.Placement{
			Address: "192.0.2.5", Connection: machineref.ConnectionSSH, KnownHostsRef: "host-key", Machine: "hypervisor",
			PrivateKeyRef: "hypervisor-key", User: "root",
		},
		URI:  "qemu:///system",
		UUID: "5c1d3a52-0b3e-4f6a-9d2c-7e8f9a0b1c2d",
		VCPU: 4,
	}
}

// hypervisor is the machine adapter double: what one observation, removal or
// lost apply of the frozen machine returns, from what the provider host holds.
// Each evidence form is what substrate_machine_protocol.py publishes for that
// state, as the libvirt package's goldens pin.
type hypervisor struct {
	mutex   sync.Mutex
	request libvirt.MachineRequest
	// silent is a host the adapter cannot reach; foreign is a domain of the
	// frozen name without this context's ownership; listener is something
	// listening on the controller's socket with nothing of the machine behind
	// it; partial is this context's own domain defined, with its disk, while
	// its controller never started.
	silent, foreign, listener, partial bool
	calls                              []string
}

func (h *hypervisor) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.calls = append(h.calls, run.Operation)
	if h.silent {
		return lifecycle.RunResult{}, errors.New("unreachable")
	}
	gone := []libvirt.DiskEvidence{{Name: "root"}}
	absent := libvirt.MachineEvidence{Absent: true, Answered: true, Disks: gone, Listener: held(false), Postcondition: true, Request: run.Digest}
	switch run.Operation {
	case "apply":
		return lifecycle.RunResult{}, errors.New("the adapter's result was lost")
	case "destroy":
		h.partial = false
		return lifecycle.RunResult{Outcome: "changed", Evidence: evidenceOf(absent)}, nil
	}
	switch {
	case h.foreign:
		return lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(libvirt.MachineEvidence{
			Answered: true, Disks: gone, Domain: h.request.Domain, Listener: held(false), Request: run.Digest,
			State: "running", System: "0f5c6d7e-8a9b-4c0d-9e1f-2a3b4c5d6e7f",
		})}, nil
	case h.listener:
		return lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(libvirt.MachineEvidence{
			Answered: true, Disks: gone, Listener: held(true), Request: run.Digest,
		})}, nil
	case h.partial:
		return lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(libvirt.MachineEvidence{
			Answered: true, Disks: []libvirt.DiskEvidence{{Name: "root", Present: true, SizeGiB: 60}}, Domain: h.request.Domain,
			Listener: held(false), Owned: true, Request: run.Digest, State: "shut off", System: h.request.UUID,
		})}, nil
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceOf(absent)}, nil
}

func (h *hypervisor) set(change func(*hypervisor)) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	change(h)
}

func (h *hypervisor) took() []string {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	calls := h.calls
	h.calls = nil
	return calls
}

func held(value bool) *bool { return &value }

func evidenceOf(evidence libvirt.MachineEvidence) json.RawMessage {
	data, _ := json.Marshal(evidence)
	return data
}

// lostMachineJourney plans the one machine, whose apply attempt the adapter
// loses, so its block is unknown before any journey begins.
func lostMachineJourney(t *testing.T) (*lifecycle.CapabilityJourney, *hypervisor) {
	t.Helper()
	request := lostMachineRequest()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	adapter := &hypervisor{request: request}
	journey := lifecycle.NewCapabilityJourney(t, libvirt.NewMachine(adapter),
		lifecycle.CapabilityBinding{Kind: libvirt.MachineKind, Implementation: libvirt.MachineImplementation},
		lifecycle.CapabilityPlan{
			Definitions: []reconciliation.BlockDefinition{{
				ID: lostMachine, Description: "realize the virtual machine rhel-01 and its controller", Stage: reconciliation.StageMachines,
				Kind: libvirt.MachineKind, Object: "rhel-01", Implementation: libvirt.MachineImplementation,
				ContentDigest: strings.Repeat("d", 64), Request: canonical,
				Groups: []reconciliation.Group{{ID: "define-domain", Description: "define the domain", Machines: []string{"rhel-01"}}},
			}},
			Reservations: []prerequisites.HostReservation{{Context: lifecycle.JourneyContext, Kind: "substrate-machine", Service: "rhel-01", Keys: request.ReservationKeys()}},
			Secrets:      request.SecretReferences(),
		})
	if _, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil {
		t.Fatal("the lost apply attempt completed")
	}
	if calls := adapter.took(); !slices.Equal(calls, []string{"apply"}) {
		t.Fatalf("the lost apply ran %v", calls)
	}
	return journey, adapter
}

func destroyLost(journey *lifecycle.CapabilityJourney) (*lifecycle.OperationResult, error) {
	return journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{
		ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true,
	})
}

// unresolvedOf is what status reports about the lost machine: the operation
// holding it, its state and why it is unproved.
func unresolvedOf(t *testing.T, journey *lifecycle.CapabilityJourney) (string, string, *lifecycle.Unresolved) {
	t.Helper()
	status, err := journey.Service.Status(context.Background(), lifecycle.StatusRequest{ContextName: lifecycle.JourneyContext})
	if err != nil || status.Lifecycle == nil || len(status.Lifecycle.Blocks) != 1 {
		t.Fatalf("status = %+v (%v)", status, err)
	}
	summary := status.Lifecycle
	return summary.Verb + " " + summary.State, summary.Blocks[0].State, summary.Blocks[0].Unresolved
}

// requireRefusal holds a refused removal to what D38 decided: one diagnostic
// naming why the machine stayed unknown and its remedy, then the refusal
// naming the block, with nothing registered, probed, removed or released, and
// status naming the same reason.
func requireRefusal(t *testing.T, journey *lifecycle.CapabilityJourney, adapter *hypervisor, reason, remedy string) {
	t.Helper()
	before := journey.Reservations()
	result, err := destroyLost(journey)
	if result != nil || err == nil {
		t.Fatalf("the removal over an unproved machine went on: %+v", result)
	}
	want := []diagnostics.Diagnostic{
		{Severity: "error", Code: "lifecycle.unknown", Message: "the outcome of " + lostMachine + " is still unknown: " + reason,
			Remediation: remedy + ", then repeat the operation to observe it again"},
		{Severity: "error", Code: "lifecycle.unknown", Message: "this removal cannot prove what these effects left behind, so it registered nothing: " + lostMachine,
			Remediation: "do what the diagnostic of each reports, then repeat bootwright destroy"},
	}
	if got := diagnostics.Of(err); !slices.Equal(got, want) {
		t.Fatalf("the refusal reported %+v, want %+v", got, want)
	}
	if calls := adapter.took(); !slices.Equal(calls, []string{"observe"}) {
		t.Fatalf("the refused removal ran %v, want only its resolution's observation", calls)
	}
	if !slices.EqualFunc(journey.Reservations(), before, func(x, y prerequisites.HostReservation) bool {
		return x.Context == y.Context && slices.Equal(x.Keys, y.Keys)
	}) || len(before) != 1 {
		t.Fatalf("the refused removal changed the reservations %v to %v", before, journey.Reservations())
	}
	operation, block, unresolved := unresolvedOf(t, journey)
	if operation != "apply unknown" || block != "unknown" || unresolved == nil || unresolved.Reason != reason || unresolved.Remedy != remedy {
		t.Fatalf("status reports %s with %s block, unresolved %+v", operation, block, unresolved)
	}
}

// requireRemoved holds a removal that resolved the machine to one that
// removed it, completed and released every reservation.
func requireRemoved(t *testing.T, journey *lifecycle.CapabilityJourney, adapter *hypervisor) {
	t.Helper()
	result, err := destroyLost(journey)
	if err != nil || result.Receipt.Verb != "destroy" || result.Receipt.State != "done" {
		t.Fatalf("the removal = %+v (%v)", result, err)
	}
	if calls := adapter.took(); !slices.Equal(calls, []string{"observe", "observe", "destroy"}) {
		t.Fatalf("the removal ran %v, want its resolution, its quiescence probe and its removal", calls)
	}
	if held := journey.Reservations(); len(held) != 0 {
		t.Fatalf("the completed removal kept %v", held)
	}
	if operation, block, unresolved := unresolvedOf(t, journey); operation != "destroy done" || block != "done" || unresolved != nil {
		t.Fatalf("status reports %s with %s block, unresolved %+v", operation, block, unresolved)
	}
}

// A libvirt machine whose apply attempt was lost is unknown, and the removal
// that observes it first either proves what it left or refuses before it
// registers anything, naming why the machine stayed unknown. Once the operator
// removes the foreign domain, restores the host or stops what listens on the
// controller's socket, repeating the removal resolves the machine and goes on
// to remove it; while the domain is still foreign, the refusal holds with the
// same reason. A domain this context owns
// whose controller never started is a partial realization, which the same
// removal resolves and takes back at once.
func TestALostLibvirtMachineIsRecoveredOnlyOnceItsObservationProvesIt(t *testing.T) {
	host := "Machine hypervisor at 192.0.2.5"
	foreignReason := "domain bootwright-lab-rhel-01 on " + host + " does not carry this context's ownership"
	foreignRemedy := "remove or rename domain bootwright-lab-rhel-01 on " + host + ", which this context does not own"
	t.Run("foreign domain, then removed", func(t *testing.T) {
		journey, adapter := lostMachineJourney(t)
		if operation, _, unresolved := unresolvedOf(t, journey); operation != "apply unknown" || unresolved == nil ||
			!strings.Contains(unresolved.Reason, "no observation has read it yet") {
			t.Fatalf("the lost attempt reads %s, unresolved %+v", operation, unresolved)
		}
		adapter.set(func(h *hypervisor) { h.foreign = true })
		requireRefusal(t, journey, adapter, foreignReason, foreignRemedy)
		adapter.set(func(h *hypervisor) { h.foreign = false })
		requireRemoved(t, journey, adapter)
	})
	t.Run("still foreign, and the refusal holds with its reason", func(t *testing.T) {
		journey, adapter := lostMachineJourney(t)
		adapter.set(func(h *hypervisor) { h.foreign = true })
		for range 3 {
			requireRefusal(t, journey, adapter, foreignReason, foreignRemedy)
		}
	})
	t.Run("silent host, then answering", func(t *testing.T) {
		journey, adapter := lostMachineJourney(t)
		adapter.set(func(h *hypervisor) { h.silent = true })
		requireRefusal(t, journey, adapter, "its observation returned no evidence from "+host,
			"read why in the resolution log and restore "+host+" so the observation reaches it")
		adapter.set(func(h *hypervisor) { h.silent = false })
		requireRemoved(t, journey, adapter)
	})
	t.Run("a listener with no machine behind it, then stopped", func(t *testing.T) {
		journey, adapter := lostMachineJourney(t)
		adapter.set(func(h *hypervisor) { h.listener = true })
		requireRefusal(t, journey, adapter,
			"something listens on the controller socket 192.0.2.1:8000 on "+host+", yet none of domain bootwright-lab-rhel-01, its controller unit and its disks is present",
			"stop what listens on 192.0.2.1:8000 on "+host)
		adapter.set(func(h *hypervisor) { h.listener = false })
		requireRemoved(t, journey, adapter)
	})
	t.Run("partially realized", func(t *testing.T) {
		journey, adapter := lostMachineJourney(t)
		adapter.set(func(h *hypervisor) { h.partial = true })
		requireRemoved(t, journey, adapter)
	})
}
