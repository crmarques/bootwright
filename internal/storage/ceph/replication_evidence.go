package ceph

import (
	"encoding/json"
	"maps"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type ReplicatedImageEvidence struct {
	Name        string `json:"name"`
	GlobalID    string `json:"globalID"`
	PrimaryID   string `json:"primaryID"`
	SecondaryID string `json:"secondaryID"`
	Synced      bool   `json:"synced"`
}
type ReplicationEvidence struct {
	Version       string                    `json:"version"`
	Request       string                    `json:"request"`
	Owner         string                    `json:"owner"`
	PrimaryFSID   string                    `json:"primaryFSID"`
	SecondaryFSID string                    `json:"secondaryFSID"`
	Object        string                    `json:"object"`
	Owned         bool                      `json:"owned"`
	Exists        bool                      `json:"exists"`
	Ready         bool                      `json:"ready"`
	Images        []ReplicatedImageEvidence `json:"images"`
}

func EvaluateReplicationEvidence(data []byte, r ReplicationRequest, owner, digest string) (reconciliation.EffectState, error) {
	bad := func() (reconciliation.EffectState, error) {
		return reconciliation.EffectUnknown, failure("lifecycle.unknown", "replication evidence does not prove its two original clusters and image identities", "")
	}
	e, err := reconciliation.DecodeEvidence[ReplicationEvidence](data, 16384, "Ceph replication")
	fields, ferr := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, 16384, "Ceph replication members")
	if err != nil || ferr != nil || !closedMembers(fields, "version", "request", "owner", "primaryFSID", "secondaryFSID", "object", "owned", "exists", "ready", "images") || e.Version != ReplicationEvidenceVersion || owner == "" || digest == "" || e.Owner != owner || e.Request != digest || e.Object != r.Object || e.PrimaryFSID != FSIDFor(owner, r.Primary.Cluster) || e.SecondaryFSID != FSIDFor(owner, r.Secondary.Cluster) || len(e.Images) != len(r.Images) || e.Ready && (!e.Owned || !e.Exists) || e.Exists && !e.Owned {
		return bad()
	}
	entries, entryErr := reconciliation.DecodeEvidence[[]map[string]json.RawMessage](fields["images"], 16384, "replicated image members")
	if entryErr != nil || len(entries) != len(e.Images) {
		return bad()
	}
	for _, entry := range entries {
		if !closedMembers(entry, "name", "globalID", "primaryID", "secondaryID", "synced") {
			return bad()
		}
	}
	for i, image := range e.Images {
		if image.Name != r.Images[i].Name || image.GlobalID != "" && !zoneID.MatchString(image.GlobalID) || image.PrimaryID != "" && !rbdImageID.MatchString(image.PrimaryID) || image.SecondaryID != "" && !rbdImageID.MatchString(image.SecondaryID) || image.Synced && (image.GlobalID == "" || image.PrimaryID == "" || image.SecondaryID == "") || e.Ready && !image.Synced {
			return bad()
		}
	}
	if e.Ready {
		return reconciliation.EffectCompleted, nil
	}
	if !e.Exists && !e.Owned {
		return reconciliation.EffectNoEffect, nil
	}
	return reconciliation.EffectPartial, nil
}
func replicationRefusals() map[string]error {
	out := map[string]error{}
	for _, reason := range []string{"request", "artifact", "ownership", "evidence", "material", "transport", "remote-refusal", "adapter-exception", "native-command", "native-timeout", "native-output", "unknown", "foreign-cluster", "foreign-pool", "pool-identity", "foreign-image", "image-identity", "replication-identity", "replication-unavailable", "replication-token"} {
		out[reason] = failure("lifecycle.unknown", "the Ceph replication adapter could not prove its "+reason+" boundary", "inspect the retained operation evidence and both clusters; restore peer reachability or the original owned state, then retry the same apply; Bootwright does not adopt foreign pools, images, peers or bootstrap credentials")
	}
	maps.Copy(out, sshPolicyRefusals())
	return out
}
