package libvirt

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const resetMark = "op-0123456789abcdef0123456789abcdef/cluster-install-edge"

// B1861: a kept machine's root disk is reset through the machine's own frozen
// request, named by the staged destroy and step whose removal blanks it: the
// reset is a recorded write of its own operation and its reading the
// machine's read-only observation, and its evidence is its own, never the
// machine's. Either is completed only for this context's own domain, shut
// off, whose root disk carries that mark at its frozen size, no effect
// without the mark, over another size or while the domain runs, and unknown
// for a hypervisor that did not answer or a domain that is not this
// Machine's. The reset answers its own contract, which leaves the machine's
// content digest where it was.
func TestARootDiskResetIsCompletedOnlyUnderItsMarkAtTheFrozenSize(t *testing.T) {
	requests, err := MachineRequests(labCatalog(), "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	request := requests[0]
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "machine-node", Request: canonical, ContentDigest: MachineContentDigest()}, RequestDigest: "digest"}
	root := request.Disks[0]
	blank := RootResetEvidence{Request: "digest", Answered: true, Domain: request.Domain, Owned: true, System: request.UUID, State: domainOff,
		Disks: []DiskEvidence{{Name: root.Name, Present: true, SizeGiB: root.SizeGiB}}, RootReset: resetMark}
	for _, tc := range []struct {
		name   string
		change func(*RootResetEvidence)
		effect reconciliation.EffectState
	}{
		{"blank under its mark", func(*RootResetEvidence) {}, reconciliation.EffectCompleted},
		{"another removal's mark", func(e *RootResetEvidence) { e.RootReset = "op-ffffffffffffffffffffffffffffffff/cluster-install-edge" }, reconciliation.EffectNoEffect},
		{"no mark", func(e *RootResetEvidence) { e.RootReset = "" }, reconciliation.EffectNoEffect},
		{"another size", func(e *RootResetEvidence) { e.Disks[0].SizeGiB++ }, reconciliation.EffectNoEffect},
		{"no root disk", func(e *RootResetEvidence) { e.Disks[0].Present = false }, reconciliation.EffectNoEffect},
		{"a running domain", func(e *RootResetEvidence) { e.State = "running" }, reconciliation.EffectNoEffect},
		{"a silent hypervisor", func(e *RootResetEvidence) { e.Answered = false }, reconciliation.EffectUnknown},
		{"a foreign domain", func(e *RootResetEvidence) { e.Owned = false }, reconciliation.EffectUnknown},
		{"another system", func(e *RootResetEvidence) { e.System = "another" }, reconciliation.EffectUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := blank
			evidence.Disks = []DiskEvidence{blank.Disks[0]}
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
				runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: encode(t, evidence)}}
				execution := lifecycle.Execution{Context: "lab", Block: block, ApplyOperation: "op-applied"}
				if operation == RootResetOperation {
					execution.Attempt = 1
				} else {
					execution.Resolution = 1
				}
				effect, err := call(NewMachine(runner), context.Background(), lifecycle.RootReset{Execution: execution, Removal: "op-0123456789abcdef0123456789abcdef", Step: "cluster-install-edge"})
				if err != nil || effect != tc.effect || len(runner.requests) != 1 {
					t.Fatalf("%s read %s (%v), want %s", operation, effect, err, tc.effect)
				}
				run := runner.requests[0]
				if run.Operation != operation || run.Implementation != MachineImplementation || run.ReadOnly != (operation == lifecycle.ObserveOperation) ||
					run.MaterialValues["rootReset"] != "op-0123456789abcdef0123456789abcdef" || run.MaterialValues["rootResetStep"] != "cluster-install-edge" ||
					run.Digest != "digest" || string(run.Canonical) != string(canonical) || len(run.Materials) != len(lifecycle.Materials(request.Placement)) {
					t.Fatalf("%s ran %+v", operation, run)
				}
			}
		})
	}
	machineEvidence := encode(t, MachineEvidence{Request: "digest", Answered: true, Domain: request.Domain, Owned: true, System: request.UUID, State: domainOff})
	if _, err := NewMachine(&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: machineEvidence}}).ObserveRootDisk(context.Background(),
		lifecycle.RootReset{Execution: lifecycle.Execution{Context: "lab", Block: block, ApplyOperation: "op-applied", Resolution: 1}, Removal: "op-0123456789abcdef0123456789abcdef", Step: "cluster-install-edge"}); err == nil {
		t.Fatal("a root disk reading decoded the machine's own evidence")
	}
	if implementation, digest := NewMachine(nil).RootResetContract(); implementation != RootResetImplementation || digest != RootResetContentDigest() || digest == MachineContentDigest() {
		t.Fatalf("the reset answers the contract %s %s", implementation, digest)
	}
	if refusal := NewMachine(nil).RootResetRefusal(block); refusal != "" {
		t.Fatalf("a machine this build realized refuses its reset: %s", refusal)
	}
	block.ContentDigest = "another"
	if refusal := NewMachine(nil).RootResetRefusal(block); refusal == "" {
		t.Fatal("a machine another build realized admits a reset this build defines")
	}
}
