package ceph

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func ProvedSubvolume(proofs []lifecycle.BlockEvidence, name string) (path, filesystem, group string, err error) {
	found := false
	for _, proof := range proofs {
		if proof.Kind != string(api.CephSubvolume) || proof.Object != name || proof.Implementation != Implementation || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone ||
			proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
			continue
		}
		if found {
			return "", "", "", failure("lifecycle.state", "CephSubvolume "+name+" has ambiguous creation proofs", "restore the original apply evidence")
		}
		found = true
		r, err := DecodeRequest(proof.Block.Request)
		if err != nil || r.Action != "subvolume" || r.Subvolume == nil || r.Subvolume.Name != name {
			return "", "", "", failure("lifecycle.state", "the creation proof of CephSubvolume "+name+" names another request", "restore the original apply evidence")
		}
		e, err := decodeEvidence(proof.Evidence)
		r.Owner, r.FSID = e.Owner, FSIDFor(e.Owner, r.Cluster)
		if state, _ := EvaluateEvidence(proof.Evidence, r, proof.Block.RequestDigest, false); err != nil || state != reconciliation.EffectCompleted {
			return "", "", "", failure("lifecycle.state", "the creation proof of CephSubvolume "+name+" does not prove a ready subvolume at its reported path", "restore the original apply evidence")
		}
		path, filesystem, group = e.Resource.Path, r.Subvolume.Filesystem, r.Subvolume.Group
	}
	if !found {
		return "", "", "", failure("lifecycle.state", "CephSubvolume "+name+" lacks its applied creation proof", "apply CephSubvolume "+name+" first; a consumer binds the path its creation reported and never constructs one")
	}
	return path, filesystem, group, nil
}

type ProvedClusterScope struct {
	Authored  []AuxiliaryReference
	Bootstrap Host
	Machines  []string
	Scope     ClusterScope
}

func provedClusterProof(proofs []lifecycle.BlockEvidence, cluster string) (lifecycle.BlockEvidence, error) {
	var found *lifecycle.BlockEvidence
	for i, proof := range proofs {
		if proof.Kind != string(api.CephCluster) || proof.Object != cluster || proof.Implementation != Implementation || proof.Block.ID != ClusterBlockID(cluster) || proof.Verb != reconciliation.Apply ||
			proof.State != reconciliation.BlockDone || proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
			continue
		}
		if found != nil {
			return lifecycle.BlockEvidence{}, failure("lifecycle.state", "CephCluster "+cluster+" has ambiguous cluster creation proofs", "restore the original apply evidence")
		}
		found = &proofs[i]
	}
	if found == nil {
		return lifecycle.BlockEvidence{}, failure("lifecycle.state", "CephCluster "+cluster+" lacks its applied cluster creation proof", "apply CephCluster "+cluster+" first; an SMB resource takes its cluster's FSID, bootstrap host and images only from that proof")
	}
	return *found, nil
}

func ProvedCluster(proofs []lifecycle.BlockEvidence, cluster, owner string) (ProvedClusterScope, error) {
	const restore = "restore the original apply evidence"
	proof, err := provedClusterProof(proofs, cluster)
	if err != nil {
		return ProvedClusterScope{}, err
	}
	r, err := DecodeRequest(proof.Block.Request)
	if err != nil || r.Action != "cluster" || r.Cluster != cluster {
		return ProvedClusterScope{}, failure("lifecycle.state", "the creation proof of CephCluster "+cluster+" names another request", restore)
	}
	e, err := decodeEvidence(proof.Evidence)
	if err == nil && e.Owner != owner {
		return ProvedClusterScope{}, failure("lifecycle.state", "the creation proof of CephCluster "+cluster+" belongs to another operation", restore)
	}
	r.Owner, r.FSID = owner, FSIDFor(owner, cluster)
	if state, _ := EvaluateEvidence(proof.Evidence, r, proof.Block.RequestDigest, false); err != nil || owner == "" || state != reconciliation.EffectCompleted {
		return ProvedClusterScope{}, failure("lifecycle.state", "the creation proof of CephCluster "+cluster+" does not prove a ready cluster", restore)
	}
	at := slices.IndexFunc(r.Hosts, func(h Host) bool { return h.Name == r.Bootstrap })
	if at < 0 {
		return ProvedClusterScope{}, failure("lifecycle.state", "the creation proof of CephCluster "+cluster+" names no bootstrap host among its hosts", restore)
	}
	authored := slices.Clone(r.Auxiliary)
	if r.Image, err = ProvedImage(proofs, cluster, r.Image); err != nil {
		return ProvedClusterScope{}, err
	}
	if r.Auxiliary, err = ProvedAuxiliaryImages(proofs, cluster, r.Auxiliary); err != nil {
		return ProvedClusterScope{}, err
	}
	return ProvedClusterScope{Authored: authored, Bootstrap: r.Hosts[at], Machines: requestMachines(r), Scope: scopeOf(r)}, nil
}
