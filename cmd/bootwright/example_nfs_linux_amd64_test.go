//go:build linux && amd64

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// TestExampleNFSAcceptanceWritesAsTheSubvolumeOwner keeps the README's
// allowed-client check runnable: every client entry squashes root, so the
// check writes through the export as the frozen subvolume's owner, which must
// not be root.
func TestExampleNFSAcceptanceWritesAsTheSubvolumeOwner(t *testing.T) {
	state, _ := compileAcceptance(t, exampleDirectory(t, nfsExample))
	definitions := productionDefinitions(t, state, nfsExample)
	index := slices.IndexFunc(definitions, func(block reconciliation.BlockDefinition) bool { return block.ID == "ceph-subvolume-docs" })
	if index < 0 {
		t.Fatal("the example plans no ceph-subvolume-docs block")
	}
	request, err := ceph.DecodeRequest(definitions[index].Request)
	if err != nil || request.Subvolume == nil {
		t.Fatalf("the subvolume block froze %s (%v)", definitions[index].Request, err)
	}
	readme, err := os.ReadFile(filepath.Join(examplePath(t, nfsExample), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	identities := regexp.MustCompile(`setpriv --reuid=(\d+) --regid=(\d+) `).FindAllStringSubmatch(string(readme), -1)
	if len(identities) != 1 {
		t.Fatalf("the README switches identity %d times, want once", len(identities))
	}
	uid, _ := strconv.Atoi(identities[0][1])
	gid, _ := strconv.Atoi(identities[0][2])
	if request.Subvolume.UID == 0 || uid != request.Subvolume.UID || gid != request.Subvolume.GID {
		t.Fatalf("the README writes as %d:%d through a root-squashing export, but the subvolume is owned by %d:%d", uid, gid, request.Subvolume.UID, request.Subvolume.GID)
	}
}
