//go:build linux && amd64

package main

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
)

const nfsExample = "libvirt-3-nodes-ceph-nfs"

// TestExampleNFSPlansTheExportBlock plans the NFS export example through the
// production capabilities: no capability refuses it, and the export block
// waits on the cluster, its image, the nfs service unit and, through its
// requirement, the subvolume block.
func TestExampleNFSPlansTheExportBlock(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, nfsExample))
	if refused := productionUnsupported(t, state); len(refused) != 0 {
		t.Fatalf("the capabilities refuse %+v", refused)
	}
	definitions := productionDefinitions(t, state, nfsExample)
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(plan.Blocks, func(block reconciliation.Block) bool { return block.ID == ceph.NFSExportBlockID("docs") })
	if index < 0 {
		t.Fatal("the example plans no ceph-nfs-export-docs block")
	}
	for _, dependency := range []string{"ceph-cluster-ceph-lab", "ceph-image-ceph-lab", "ceph-service-ceph-lab-nfs-shares", "ceph-subvolume-docs"} {
		if !slices.Contains(plan.Blocks[index].Dependencies, dependency) {
			t.Errorf("the export block depends on %q, want it to include %q", plan.Blocks[index].Dependencies, dependency)
		}
	}
}
