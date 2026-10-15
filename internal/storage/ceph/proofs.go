package ceph

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func ProvedSubvolume(proofs []lifecycle.BlockEvidence, name string) (path, filesystem, group string, err error) {
	found := false
	for _, proof := range proofs {
		if proof.Kind != string(api.StorageSubvolume) || proof.Object != name || proof.Implementation != Implementation || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone ||
			proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
			continue
		}
		if found {
			return "", "", "", failure("lifecycle.state", "StorageSubvolume "+name+" has ambiguous creation proofs", "restore the original apply evidence")
		}
		found = true
		r, err := DecodeRequest(proof.Block.Request)
		if err != nil || r.Action != "subvolume" || r.Subvolume == nil || r.Subvolume.Name != name {
			return "", "", "", failure("lifecycle.state", "the creation proof of StorageSubvolume "+name+" names another request", "restore the original apply evidence")
		}
		e, err := decodeEvidence(proof.Evidence)
		r.Owner, r.FSID = e.Owner, FSIDFor(e.Owner, r.Cluster)
		if state, _ := EvaluateEvidence(proof.Evidence, r, proof.Block.RequestDigest, false); err != nil || state != reconciliation.EffectCompleted {
			return "", "", "", failure("lifecycle.state", "the creation proof of StorageSubvolume "+name+" does not prove a ready subvolume at its reported path", "restore the original apply evidence")
		}
		path, filesystem, group = e.Resource.Path, r.Subvolume.Filesystem, r.Subvolume.Group
	}
	if !found {
		return "", "", "", failure("lifecycle.state", "StorageSubvolume "+name+" lacks its applied creation proof", "apply StorageSubvolume "+name+" first; a consumer binds the path its creation reported and never constructs one")
	}
	return path, filesystem, group, nil
}
