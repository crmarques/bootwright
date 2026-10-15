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
