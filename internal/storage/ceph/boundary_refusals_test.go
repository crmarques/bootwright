package ceph

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestBoundaryRefusalsShareOneListAndTemplate(t *testing.T) {
	remedy := "inspect the retained operation evidence before continuing"
	for name, table := range map[string]struct {
		refusals map[string]error
		subject  string
	}{
		"host":    {hostRefusals(), "the Ceph host preparation"},
		"smb":     {smbRefusals(), "the Ceph SMB adapter"},
		"cluster": {adapterRefusals(), "the Ceph adapter"},
	} {
		for _, reason := range adapterBoundaries {
			d := diagnostics.Of(table.refusals[reason])
			if len(d) != 1 || d[0].Code != cephBoundaryUnproved.Code || d[0].Message != table.subject+" could not prove its "+reason+" boundary" || d[0].Remediation != remedy {
				t.Fatalf("%s refusal %s is %+v", name, reason, d)
			}
		}
	}
	replication := replicationRefusals()
	for _, reason := range append([]string{"foreign-pool", "pool-identity", "foreign-image", "image-identity", "replication-identity", "replication-unavailable", "replication-token"}, adapterBoundaries...) {
		d := diagnostics.Of(replication[reason])
		if len(d) != 1 || d[0].Message != "the Ceph replication adapter could not prove its "+reason+" boundary" {
			t.Fatalf("replication refusal %s is %+v", reason, d)
		}
	}
}

func TestCompletedApplySelectsOnlyAConsistentCompletedOriginalApply(t *testing.T) {
	good := lifecycle.BlockEvidence{Kind: "CephCluster", Object: "c", Implementation: "impl", Verb: reconciliation.Apply, State: reconciliation.BlockDone}
	good.Block.Kind, good.Block.Object, good.Block.Implementation = "CephCluster", "c", "impl"
	if !completedApply(good, "CephCluster", "c", "impl") || !completedApplyAnyImplementation(good, "CephCluster", "c") {
		t.Fatal("a consistent completed original apply was not selected")
	}
	for name, mutate := range map[string]func(*lifecycle.BlockEvidence){
		"kind":         func(p *lifecycle.BlockEvidence) { p.Kind = "Machine" },
		"object":       func(p *lifecycle.BlockEvidence) { p.Object = "d" },
		"verb":         func(p *lifecycle.BlockEvidence) { p.Verb = reconciliation.Destroy },
		"state":        func(p *lifecycle.BlockEvidence) { p.State = reconciliation.BlockPending },
		"block kind":   func(p *lifecycle.BlockEvidence) { p.Block.Kind = "Machine" },
		"block object": func(p *lifecycle.BlockEvidence) { p.Block.Object = "d" },
		"block impl":   func(p *lifecycle.BlockEvidence) { p.Block.Implementation = "other" },
		"impl":         func(p *lifecycle.BlockEvidence) { p.Implementation = "other"; p.Block.Implementation = "other" },
	} {
		p := good
		mutate(&p)
		if completedApply(p, "CephCluster", "c", "impl") {
			t.Fatalf("a proof differing in %s was selected", name)
		}
	}
}
