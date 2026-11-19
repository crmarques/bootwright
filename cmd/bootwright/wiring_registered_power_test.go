package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
)

const kubevirtPowerDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// kubevirtPowerProof is a done KubeVirt Machine apply whose evidence proves its owned VirtualMachine.
func kubevirtPowerProof(t *testing.T, change func(*kubevirt.MachineRequest, *kubevirt.MachineEvidence)) lifecycle.BlockEvidence {
	t.Helper()
	request := kubevirt.MachineRequest{CPU: 2, DiskGiB: 20, Identity: kubevirt.Identity{Block: "machine-vm-1", Context: "lab", Object: "vm-1"},
		Interfaces: []kubevirt.Interface{}, KubeconfigRef: "host-kubeconfig", MediaVolume: "media", MemoryMiB: 4096, Name: "vm-1", Namespace: "lab-ns",
		Placement: machineref.Placement{Connection: machineref.ConnectionLocal, Machine: "controller"}, Provider: "host",
		RootVolume: "vm-1-root", Version: "machine-kubevirt-v1"}
	evidence := kubevirt.MachineEvidence{Answered: true, KubeSystemUID: "kube-system-uid", Owned: true, Postcondition: true, Receipts: []kubevirt.Receipt{},
		Request: kubevirtPowerDigest, Server: "https://api.host.example.test:6443", VirtualMachine: "vm-1", Volumes: []kubevirt.VolumeEvidence{}}
	evidence.Receipts = append(evidence.Receipts, kubevirt.Receipt{Basis: "created", Kind: "VirtualMachine", Name: "vm-1", UID: "vm-uid"})
	evidence.Volumes = append(evidence.Volumes, kubevirt.VolumeEvidence{Kind: "DataVolume", Name: "vm-1-root", Present: true})
	if change != nil {
		change(&request, &evidence)
	}
	frozen, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	observed, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.BlockEvidence{Operation: "op-1", Kind: "Machine", Object: "vm-1", Implementation: kubevirt.MachineImplementation,
		Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: observed,
		Block: reconciliation.Block{RequestDigest: kubevirtPowerDigest, BlockDefinition: reconciliation.BlockDefinition{
			ID: "machine-vm-1", Kind: "Machine", Object: "vm-1", Implementation: kubevirt.MachineImplementation, Request: frozen}}}
}

func TestRegisteredMachineRequestBindsAKubeVirtMachineToItsHostAndVirtualMachine(t *testing.T) {
	got, pin, err := registeredMachineRequest(kubevirtPowerProof(t, nil), power.Start)
	if err != nil {
		t.Fatal(err)
	}
	if got.Arm != substrate.ArmKubeVirt || got.Verb != power.Start || got.Controller.Endpoint != substrate.KubeVirtEndpoint("lab-ns", "vm-1") ||
		got.Controller.CredentialsRef != "host-kubeconfig" || !got.Controller.TLSVerify || got.Identity.Object != "vm-1" || got.Identity.Context != "lab" {
		t.Fatalf("registered request = %+v", got)
	}
	if pin.UUID != "kube-system-uid" || pin.Serial != "vm-uid" {
		t.Fatalf("pin = %+v, want the cluster identity and the VirtualMachine's UID", pin)
	}
}

func TestRegisteredMachineRequestRefusesWhatCannotProveTheKubeVirtMachine(t *testing.T) {
	for _, tc := range []struct {
		name   string
		proof  func() lifecycle.BlockEvidence
		change func(*kubevirt.MachineRequest, *kubevirt.MachineEvidence)
		verb   power.Verb
	}{
		{name: "hosted on another cluster", change: func(r *kubevirt.MachineRequest, _ *kubevirt.MachineEvidence) { r.HostCluster = "host" }},
		{name: "no kubeconfig reference", change: func(r *kubevirt.MachineRequest, _ *kubevirt.MachineEvidence) { r.KubeconfigRef = "" }},
		{name: "no VirtualMachine receipt", change: func(_ *kubevirt.MachineRequest, e *kubevirt.MachineEvidence) { e.Receipts = nil }},
		{name: "unowned VirtualMachine", change: func(_ *kubevirt.MachineRequest, e *kubevirt.MachineEvidence) { e.Owned = false }},
		{name: "an invalid context", change: func(r *kubevirt.MachineRequest, _ *kubevirt.MachineEvidence) { r.Identity.Context = "Not A Context" }},
		{name: "another object", change: func(r *kubevirt.MachineRequest, _ *kubevirt.MachineEvidence) { r.Identity.Object = "vm-2" }},
		{name: "another block", change: func(r *kubevirt.MachineRequest, _ *kubevirt.MachineEvidence) { r.Identity.Block = "machine-vm-2" }},
		{name: "a verb that is no transition", verb: power.Verb("restart")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verb := tc.verb
			if verb == "" {
				verb = power.Stop
			}
			if _, _, err := registeredMachineRequest(kubevirtPowerProof(t, tc.change), verb); err == nil {
				t.Fatal("the proof supplied a power request")
			}
		})
	}
	foreign := kubevirtPowerProof(t, nil)
	foreign.Implementation = "unknown-machine-v1"
	if _, _, err := registeredMachineRequest(foreign, power.Stop); err == nil {
		t.Fatal("an unregistered implementation supplied a power request")
	}
}

type powerRunnerStub struct {
	runs []lifecycle.RunRequest
	err  error
}

func (r *powerRunnerStub) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.runs = append(r.runs, request)
	return lifecycle.RunResult{}, r.err
}

// A power run drives the frozen machine under the attempt's own context name,
// and an unproved block never reaches the runner.
func TestRegisteredMachineDriverDrivesTheFrozenMachineUnderTheAttemptsContext(t *testing.T) {
	stopped := errors.New("the adapter stopped")
	runner := &powerRunnerStub{err: stopped}
	execution := lifecycle.Execution{Context: "lab", Operation: "op-2", Attempt: 1, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "power-vm-1"}}}
	drive := registeredMachineDriver(runner)
	if err := drive(context.Background(), execution, kubevirtPowerProof(t, nil), false); !errors.Is(err, stopped) {
		t.Fatalf("the drive returned %v, want the runner's error", err)
	}
	if len(runner.runs) != 1 || runner.runs[0].Implementation != power.Implementation || runner.runs[0].Operation != power.Operation ||
		runner.runs[0].Digest == "" || runner.runs[0].Placement.Machine != "controller" {
		t.Fatalf("runs = %+v", runner.runs)
	}
	pending := kubevirtPowerProof(t, nil)
	pending.State = reconciliation.BlockPending
	if err := drive(context.Background(), execution, pending, true); err == nil || len(runner.runs) != 1 {
		t.Fatalf("an unproved block drove the machine: %v after %d runs", err, len(runner.runs))
	}
}
