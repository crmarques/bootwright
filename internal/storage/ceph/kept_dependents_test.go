package ceph

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type keptDependent interface {
	lifecycle.Capability
	lifecycle.KeptHostRemoval
	lifecycle.OnlineKeptHosts
	lifecycle.KeptHostsContract
}

func keptDependents(t *testing.T) map[string]struct {
	capability keptDependent
	block      reconciliation.Block
	handed     string
} {
	t.Helper()
	smb := reconciliation.Block{}
	for _, block := range smbPlan(t, smbGraph(t)).Blocks {
		if block.Implementation == SMBImplementation {
			smb = block
			break
		}
	}
	_, replication := replicationPlan(t, replicationCatalog("image"))
	return map[string]struct {
		capability keptDependent
		block      reconciliation.Block
		handed     string
	}{
		"smb":         {NewSMB(nil), smb, "SMB resource"},
		"consumer":    {NewConsumer(nil), reconciliation.Block{BlockDefinition: managedExportPlan(t, false)}, "the CephX keys of consumer"},
		"replication": {NewReplication(nil), replication, "the rbd-mirror peer credential"},
		"sign-on":     {NewSignOn(nil), reconciliation.Block{BlockDefinition: signOnDefinition(t, signOnCatalog(t, true))}, "the identity-provider sign-on"},
	}
}

// B1885: an SMB, consumer, replication or sign-on step is taken back from its
// kept hosts by the X763 rule for a step that hands out access: admitted
// failed or pending, refused done (B491) and unknown or running (naming
// apply). Its removal there makes no call, records that it goes with its
// cluster's teardown under that teardown's contract, and states what it
// removes and what it keeps.
func TestACephDependentIsTakenBackWithItsClustersTeardownWhileNothingHandedOutAccess(t *testing.T) {
	for name, step := range keptDependents(t) {
		if step.block.Request == nil {
			t.Fatalf("%s: no block planned", name)
		}
		for state, refused := range map[reconciliation.BlockState]string{
			reconciliation.BlockDone: "B491", reconciliation.BlockUnknown: "bootwright apply", reconciliation.BlockRunning: "bootwright apply",
			reconciliation.BlockFailed: "", reconciliation.BlockPending: "",
		} {
			refusal := step.capability.KeptHostsRefusal(step.block, lifecycle.RemovalRecords{State: state}, false)
			if refused == "" && refusal != "" || refused != "" && (!strings.Contains(refusal, step.handed) || !strings.Contains(refusal, refused)) {
				t.Fatalf("%s: a %s step answered %q", name, state, refusal)
			}
		}
		if refusal := step.capability.KeptHostsRefusal(step.block, lifecycle.RemovalRecords{State: reconciliation.BlockPending, Completed: true}, false); !strings.Contains(refusal, "B491") {
			t.Fatalf("%s: a step an earlier record completed answered %q", name, refusal)
		}
		if hosts, err := step.capability.OnlineKeptHosts(step.block); err != nil || len(hosts) == 0 {
			t.Fatalf("%s: runs its kept removal on %v (%v)", name, hosts, err)
		}
		if implementation, digest := step.capability.KeptHostsContract(); implementation != KeptImplementation || digest != KeptContentDigest() {
			t.Fatalf("%s: names the kept contract %s %s", name, implementation, digest)
		}
		removal, err := step.capability.KeptHostsRemoval(context.Background(), step.block)
		if err != nil || !strings.Contains(removal.Description, "teardown") || len(removal.Impacts) != 1 || len(removal.Retains) != 1 {
			t.Fatalf("%s: its kept removal reads %+v (%v)", name, removal, err)
		}
		kept := step.block
		kept.Inverse = &reconciliation.Inverse{KeptHosts: []string{"node-1"}}
		kept.RequestDigest = strings.Repeat("d", 64)
		e := lifecycle.Execution{Block: kept, ApplyOperation: "apply-original", Operation: "destroy-later"}
		result, err := step.capability.Destroy(context.Background(), e)
		if err != nil || result.Outcome != reconciliation.OutcomeChanged || !strings.Contains(string(result.Evidence), KeptTeardownVersion) {
			t.Fatalf("%s: its kept removal returned %+v (%v)", name, result, err)
		}
		observed, err := step.capability.ObserveRemoval(context.Background(), e)
		if err != nil || observed.Effect != reconciliation.EffectCompleted {
			t.Fatalf("%s: its kept removal is observed %+v (%v)", name, observed, err)
		}
		quiescence, err := step.capability.Quiescent(context.Background(), lifecycle.Probe{Block: kept})
		if err != nil || !quiescence.Settled() {
			t.Fatalf("%s: its kept removal answered quiescence %+v (%v)", name, quiescence, err)
		}
	}
}
