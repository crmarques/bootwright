package kubevirt

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	resetRemoval = "op-0123456789abcdef0123456789abcdef"
	resetStep    = "cluster-install-edge"
)

func blankRoot(t *testing.T) RootResetEvidence {
	t.Helper()
	return RootResetEvidence{Request: machineBlock(t).RequestDigest, Answered: true, Owned: true, KubeSystemUID: "cluster-1", VirtualMachine: "vm-1",
		RootReset: resetRemoval + "/" + resetStep, Root: RootVolumeEvidence{Blank: true, Name: machineRequest(t).RootVolume, Phase: "Succeeded", Present: true,
			Storage: "120Gi", StorageClass: "guest-block", UID: "root-blank", Volume: "pv-blank"}}
}

func resetExecution(t *testing.T, operation string) lifecycle.Execution {
	t.Helper()
	block := machineBlock(t)
	block.ContentDigest = MachineContentDigest()
	request := machineRequest(t)
	previous := machineFixture(t, block.RequestDigest, func(e *MachineEvidence) {
		e.Receipts = append(e.Receipts, Receipt{Basis: "owner-referenced", Kind: "DataVolume", Name: request.RootVolume, UID: "root-installed"})
	})
	execution := lifecycle.Execution{Context: testContext, Block: block, ApplyOperation: "op-applied", PreviousEvidence: previous,
		ApplyProved: []lifecycle.BlockEvidence{provedHostEvidence(t, hostBlock(t), "cluster-1")}}
	if operation == RootResetOperation {
		execution.Attempt = 1
	} else {
		execution.Resolution = 1
	}
	return execution
}

// B1880: a kept VirtualMachine's root disk is reset through the machine's own
// frozen request, named by the staged destroy and step whose removal blanks
// it: the reset is a recorded write of its own operation and its reading the
// machine's read-only observation, each lent only the provider's kubeconfig
// and handed the cluster and receipts the Machine block's apply recorded, and
// its evidence is its own, never the machine's. Either is completed only for
// this context's own VirtualMachine by the UID its apply recorded, on the
// cluster its provider proof pinned, stopped, whose root DataVolume is blank,
// Succeeded, at its frozen name, size and class, never the one the apply
// created, under that mark; no effect otherwise, and unknown for a host that
// did not answer or a VirtualMachine that is not this Machine's. The reset
// answers its own contract, which leaves the machine's content digest where it
// was.
func TestARootDiskResetIsCompletedOnlyUnderItsMarkOverAFreshBlankRoot(t *testing.T) {
	request := machineRequest(t)
	digest := machineBlock(t).RequestDigest
	blank := blankRoot(t)
	for _, tc := range []struct {
		name   string
		change func(*RootResetEvidence)
		effect reconciliation.EffectState
	}{
		{"blank under its mark", func(*RootResetEvidence) {}, reconciliation.EffectCompleted},
		{"a final instance", func(e *RootResetEvidence) { e.Instance = "Succeeded" }, reconciliation.EffectCompleted},
		{"another removal's mark", func(e *RootResetEvidence) { e.RootReset = "op-ffffffffffffffffffffffffffffffff/" + resetStep }, reconciliation.EffectNoEffect},
		{"no mark", func(e *RootResetEvidence) { e.RootReset = "" }, reconciliation.EffectNoEffect},
		{"the installed root", func(e *RootResetEvidence) { e.Root.UID = "root-installed" }, reconciliation.EffectNoEffect},
		{"another size", func(e *RootResetEvidence) { e.Root.Storage = "121Gi" }, reconciliation.EffectNoEffect},
		{"another class", func(e *RootResetEvidence) { e.Root.StorageClass = "other" }, reconciliation.EffectNoEffect},
		{"another volume", func(e *RootResetEvidence) { e.Root.Name = request.MediaVolume }, reconciliation.EffectNoEffect},
		{"not blank", func(e *RootResetEvidence) { e.Root.Blank = false }, reconciliation.EffectNoEffect},
		{"no bound volume", func(e *RootResetEvidence) { e.Root.Volume = "" }, reconciliation.EffectNoEffect},
		{"still importing", func(e *RootResetEvidence) { e.Root.Phase = "ImportInProgress" }, reconciliation.EffectNoEffect},
		{"no root", func(e *RootResetEvidence) { e.Root = RootVolumeEvidence{} }, reconciliation.EffectNoEffect},
		{"a running instance", func(e *RootResetEvidence) { e.Instance = "Running" }, reconciliation.EffectNoEffect},
		{"a silent host", func(e *RootResetEvidence) { e.Answered = false }, reconciliation.EffectUnknown},
		{"a foreign VirtualMachine", func(e *RootResetEvidence) { e.Owned = false }, reconciliation.EffectUnknown},
		{"another VirtualMachine", func(e *RootResetEvidence) { e.VirtualMachine = "vm-2" }, reconciliation.EffectUnknown},
		{"another cluster", func(e *RootResetEvidence) { e.KubeSystemUID = "cluster-2" }, reconciliation.EffectUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := blank
			tc.change(&evidence)
			for operation, call := range map[string]func(MachineCapability, context.Context, lifecycle.RootReset) (reconciliation.EffectState, error){
				RootResetOperation: func(c MachineCapability, ctx context.Context, reset lifecycle.RootReset) (reconciliation.EffectState, error) {
					outcome, effect, err := c.ResetRootDisk(ctx, reset)
					if err == nil && outcome != reconciliation.OutcomeChanged {
						t.Fatalf("the reset reported %s, not its run's outcome", outcome)
					}
					return effect, err
				},
				lifecycle.ObserveOperation: MachineCapability.ObserveRootDisk,
			} {
				run := &runner{result: lifecycle.RunResult{Outcome: "changed", Evidence: encoded(t, evidence)}}
				effect, err := call(NewMachine(run), context.Background(), lifecycle.RootReset{Execution: resetExecution(t, operation), Removal: resetRemoval, Step: resetStep})
				if err != nil || effect != tc.effect || len(run.requests) != 1 {
					t.Fatalf("%s read %s (%v), want %s", operation, effect, diagnostics.Of(err), tc.effect)
				}
				sent := run.requests[0]
				var pinned proof
				if err := json.Unmarshal([]byte(sent.MaterialValues["pinned"]), &pinned); err != nil || pinned.KubeSystemUID != "cluster-1" || len(pinned.Receipts) != 2 {
					t.Fatalf("%s pinned %+v (%v)", operation, pinned, err)
				}
				canonical, _ := request.Canonical()
				if sent.Operation != operation || sent.Implementation != MachineImplementation || sent.ReadOnly != (operation == lifecycle.ObserveOperation) ||
					sent.MaterialValues["rootReset"] != resetRemoval || sent.MaterialValues["rootResetStep"] != resetStep || sent.MaterialValues["operation"] != "" ||
					sent.Digest != digest || string(sent.Canonical) != string(canonical) || sent.Deadline != machineDeadline ||
					!slices.ContainsFunc(sent.Materials, func(file lifecycle.MaterialFile) bool {
						return file.Secret == "host-kubeconfig" && file.Part == secrets.ValuePart && file.Variable == "kubeconfig"
					}) {
					t.Fatalf("%s ran %+v", operation, sent)
				}
			}
		})
	}
	machine := machineFixture(t, digest, nil)
	if _, err := NewMachine(&runner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: machine}}).ObserveRootDisk(context.Background(),
		lifecycle.RootReset{Execution: resetExecution(t, lifecycle.ObserveOperation), Removal: resetRemoval, Step: resetStep}); err == nil {
		t.Fatal("a root disk reading decoded the machine's own evidence")
	}
	another := blank
	another.Request = strings.Repeat("c", 64)
	if _, err := NewMachine(&runner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encoded(t, another)}}).ObserveRootDisk(context.Background(),
		lifecycle.RootReset{Execution: resetExecution(t, lifecycle.ObserveOperation), Removal: resetRemoval, Step: resetStep}); err == nil {
		t.Fatal("a root disk reading accepted evidence for another request")
	}
	if implementation, digest := NewMachine(nil).RootResetContract(); implementation != RootResetImplementation || digest != RootResetContentDigest() || digest == MachineContentDigest() {
		t.Fatalf("the reset answers the contract %s %s", implementation, digest)
	}
	if references, err := NewMachine(nil).RootResetSecrets(machineBlock(t)); err != nil || !slices.Equal(references, request.SecretReferences()) {
		t.Fatalf("the reset is lent %v (%v)", references, err)
	}
}

// B1880: nothing runs for a VirtualMachine the reset cannot prove by the
// records the Machine block's apply holds: without the recorded VirtualMachine
// UID, the provider's proof, or with a recorded cluster other than the proof's,
// the reset refuses before any run, and a block another machine contract
// realized, or one hosted on a cluster of the context, refuses its reset.
func TestARootDiskResetRefusesATargetItsApplyRecordsDoNotProve(t *testing.T) {
	for name, change := range map[string]func(*lifecycle.Execution){
		"no receipt": func(e *lifecycle.Execution) {
			e.PreviousEvidence = machineFixture(t, e.Block.RequestDigest, func(m *MachineEvidence) { m.Receipts = nil })
		},
		"no previous evidence": func(e *lifecycle.Execution) { e.PreviousEvidence = nil },
		"another recorded cluster": func(e *lifecycle.Execution) {
			e.PreviousEvidence = machineFixture(t, e.Block.RequestDigest, func(m *MachineEvidence) { m.KubeSystemUID = "cluster-0" })
		},
		"no provider proof": func(e *lifecycle.Execution) { e.ApplyProved = nil },
		"a hosted VirtualMachine": func(e *lifecycle.Execution) {
			request := machineRequest(t)
			request.HostCluster, request.KubeconfigRef = "host", ""
			e.Block.Request, _ = request.Canonical()
		},
		"another contract": func(e *lifecycle.Execution) { e.Block.ContentDigest = "another" },
	} {
		t.Run(name, func(t *testing.T) {
			execution := resetExecution(t, RootResetOperation)
			change(&execution)
			run := &runner{}
			if _, effect, err := NewMachine(run).ResetRootDisk(context.Background(), lifecycle.RootReset{Execution: execution, Removal: resetRemoval, Step: resetStep}); err == nil ||
				effect != reconciliation.EffectUnknown || len(run.requests) != 0 {
				t.Fatalf("an unproved reset read %s (%v) after %d runs", effect, err, len(run.requests))
			}
		})
	}
	block := machineBlock(t)
	block.ContentDigest = MachineContentDigest()
	if refusal := NewMachine(nil).RootResetRefusal(block); refusal != "" {
		t.Fatalf("a machine this build realized refuses its reset: %s", refusal)
	}
	block.ContentDigest = "another"
	if refusal := NewMachine(nil).RootResetRefusal(block); !strings.Contains(refusal, "another machine contract") {
		t.Fatalf("a machine another build realized admits a reset this build defines: %q", refusal)
	}
	request := machineRequest(t)
	request.HostCluster, request.KubeconfigRef = "host", ""
	block.ContentDigest = MachineContentDigest()
	block.Request, _ = request.Canonical()
	if refusal := NewMachine(nil).RootResetRefusal(block); !strings.Contains(refusal, "(B1890)") {
		t.Fatalf("a hosted VirtualMachine admits a reset: %q", refusal)
	}
}
