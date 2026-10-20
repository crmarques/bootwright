package ceph

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func subvolumeProof(t *testing.T, name, group, path string) lifecycle.BlockEvidence {
	t.Helper()
	frozen := frozenSubvolume()
	frozen.Subvolume.Name, frozen.Subvolume.Group = name, group
	data, err := frozen.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	runtime := runtimeRequest()
	runtime.Action, runtime.Subvolume = "subvolume", frozen.Subvolume
	e := readyEvidence(runtime)
	e.Resource = ResourceEvidence{ID: subvolumeID, Path: path}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "ceph-subvolume-" + name, Kind: string(api.StorageSubvolume), Object: "docs", Implementation: Implementation, Request: data}, RequestDigest: "digest"}
	return lifecycle.BlockEvidence{Block: block, Kind: string(api.StorageSubvolume), Object: "docs", Implementation: Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidenceBytes(t, e)}
}

func provedFails(t *testing.T, proofs []lifecycle.BlockEvidence, message, remedy string) {
	t.Helper()
	_, _, _, err := ProvedSubvolume(proofs, "docs")
	refusedWith(t, err, "lifecycle.state", message, remedy)
}

func TestProvedSubvolumeDecodesOnlyOwnerEvidence(t *testing.T) {
	exact := "/volumes/csi/docs/" + subvolumeID
	valid := subvolumeProof(t, "docs", "csi", exact)
	path, filesystem, group, err := ProvedSubvolume([]lifecycle.BlockEvidence{valid}, "docs")
	if err != nil || path != exact || filesystem != "files" || group != "csi" {
		t.Fatalf("the valid proof returned %q %q %q (%v)", path, filesystem, group, err)
	}
	const lacks, lacksRemedy = "StorageSubvolume docs lacks its applied creation proof", "apply StorageSubvolume docs first; a consumer binds the path its creation reported and never constructs one"
	foreignKind := valid
	foreignKind.Kind, foreignKind.Block.Kind = string(api.StorageFilesystem), string(api.StorageFilesystem)
	other := valid
	other.Implementation, other.Block.Implementation = ImageImplementation, ImageImplementation
	destroy := valid
	destroy.Verb = reconciliation.Destroy
	pending := valid
	pending.State = reconciliation.BlockFailed
	for _, ignored := range []lifecycle.BlockEvidence{foreignKind, other, destroy, pending} {
		provedFails(t, []lifecycle.BlockEvidence{ignored}, lacks, lacksRemedy)
	}
	provedFails(t, nil, lacks, lacksRemedy)
	const restore = "restore the original apply evidence"
	provedFails(t, []lifecycle.BlockEvidence{subvolumeProof(t, "docs", "csi", "/srv/docs")}, "the creation proof of StorageSubvolume docs does not prove a ready subvolume at its reported path", restore)
	provedFails(t, []lifecycle.BlockEvidence{valid, valid}, "StorageSubvolume docs has ambiguous creation proofs", restore)
	provedFails(t, []lifecycle.BlockEvidence{subvolumeProof(t, "notes", "csi", "/volumes/csi/notes/"+subvolumeID)}, "the creation proof of StorageSubvolume docs names another request", restore)
}

func settingsProof(t *testing.T) lifecycle.BlockEvidence {
	t.Helper()
	proof := coreClusterProof(t, settingsRequestFor(coreRequest()), nil)
	proof.Block.ID = SettingsBlockID("example")
	return proof
}

func TestProvedClusterDecodesOnlyTheCoreClusterEvidence(t *testing.T) {
	core := coreRequest()
	unit := settingsProof(t)
	unit.Block.ID = "ceph-service-example-nfs-files"
	image := imageProof(t, "example", core.Image, resolvedDigest)
	proofs := []lifecycle.BlockEvidence{settingsProof(t), unit, image, hostProof(t, "node-1", nil), coreClusterProof(t, core, nil)}
	scope, err := ProvedCluster(proofs, "example", "apply-original")
	if err != nil {
		t.Fatalf("the cluster proof is refused: %+v", err)
	}
	if scope.Scope.Owner != "apply-original" || scope.Scope.FSID != FSIDFor("apply-original", "example") || scope.Scope.Image != PinnedRepository+"@"+resolvedDigest ||
		scope.Bootstrap.Name != "node-1" || len(scope.Machines) != 3 || scope.Scope.Cluster != "example" || len(scope.Scope.Hosts) != 3 || len(scope.Authored) != 0 {
		t.Fatalf("the proved scope is %+v", scope)
	}
	const restore = "restore the original apply evidence"
	const lacks, lacksRemedy = "StorageCluster example lacks its applied cluster creation proof", "apply StorageCluster example first; an SMB resource takes its cluster's FSID, bootstrap host and images only from that proof"
	fails := func(proofs []lifecycle.BlockEvidence, owner, message, remedy string) {
		t.Helper()
		_, err := ProvedCluster(proofs, "example", owner)
		refusedWith(t, err, "lifecycle.state", message, remedy)
	}
	fails([]lifecycle.BlockEvidence{settingsProof(t), unit, image}, "apply-original", lacks, lacksRemedy)
	fails(append(proofs, coreClusterProof(t, core, nil)), "apply-original", "StorageCluster example has ambiguous cluster creation proofs", restore)
	fails(proofs, "apply-other", "the creation proof of StorageCluster example belongs to another operation", restore)
	unready := coreClusterProof(t, core, func(e *Evidence) { e.Ready, e.Partial = false, true })
	fails([]lifecycle.BlockEvidence{unready, image}, "apply-original", "the creation proof of StorageCluster example does not prove a ready cluster", restore)
	pool := coreRequest()
	pool.Action, pool.Pool = "pool", &Pool{Name: "volumes"}
	fails([]lifecycle.BlockEvidence{coreClusterProof(t, pool, nil), image}, "apply-original", "the creation proof of StorageCluster example names another request", restore)
	lost := coreRequest()
	lost.Bootstrap = "node-9"
	fails([]lifecycle.BlockEvidence{coreClusterProof(t, lost, nil), image}, "apply-original", "the creation proof of StorageCluster example names no bootstrap host among its hosts", restore)
	destroyed := coreClusterProof(t, core, nil)
	destroyed.Verb = reconciliation.Destroy
	other := coreClusterProof(t, core, nil)
	other.Kind, other.Block.Kind = string(api.StoragePool), string(api.StoragePool)
	for _, ignored := range []lifecycle.BlockEvidence{destroyed, other} {
		fails([]lifecycle.BlockEvidence{ignored, image}, "apply-original", lacks, lacksRemedy)
	}
}
