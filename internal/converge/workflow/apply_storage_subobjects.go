package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// Storage sub-object kinds. Each StoragePool/StorageFilesystem/StorageObjectGateway/
// StorageNFSExport/StorageExport is classified and (under --override) rebuilt
// independently of its owning StorageCluster, so each is its own object in the apply
// preflight and state-check, keyed "<Kind>/<cluster>.<name>".
const (
	storageSubObjectKindPool       = "StoragePool"
	storageSubObjectKindFilesystem = "StorageFilesystem"
	storageSubObjectKindGateway    = "StorageObjectGateway"
	storageSubObjectKindNFSExport  = "StorageNFSExport"
	storageSubObjectKindExport     = "StorageExport"
)

// IsStorageSubObjectKind reports whether kind is one of the independently-classified
// storage sub-object kinds. Callers (e.g. the apply data-loss warning) use it to pick
// the sub-object entries out of a mixed ObjectClassification set.
func IsStorageSubObjectKind(kind string) bool {
	switch kind {
	case storageSubObjectKindPool, storageSubObjectKindFilesystem, storageSubObjectKindGateway, storageSubObjectKindNFSExport, storageSubObjectKindExport:
		return true
	}
	return false
}

// storageSubObject identifies one StorageCluster sub-object for independent
// convergence classification.
type storageSubObject struct {
	Kind    string
	Cluster string
	Name    string
}

// resourceID is the converge-safety record key for this sub-object, e.g.
// "StoragePool/ceph-libvirt.rbd". It is distinct from the owning cluster's
// "StorageCluster/<cluster>" key, so the two classify independently.
func (s storageSubObject) resourceID() string {
	return s.Kind + "/" + s.Cluster + "." + s.Name
}

// storageSubObjects enumerates, in stable kind-then-declaration order, every
// desired-state sub-object owned by the named StorageCluster.
func storageSubObjects(state v1alpha1.State, cluster string) []storageSubObject {
	var out []storageSubObject
	for _, pool := range state.StoragePools {
		if pool.Spec.StorageClusterRef.Name == cluster {
			out = append(out, storageSubObject{storageSubObjectKindPool, cluster, pool.Metadata.Name})
		}
	}
	for _, fs := range state.StorageFilesystems {
		if fs.Spec.StorageClusterRef.Name == cluster {
			out = append(out, storageSubObject{storageSubObjectKindFilesystem, cluster, fs.Metadata.Name})
		}
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name == cluster {
			out = append(out, storageSubObject{storageSubObjectKindGateway, cluster, gw.Metadata.Name})
		}
	}
	for _, nfs := range state.StorageNFSExports {
		if nfs.Spec.StorageClusterRef.Name == cluster {
			out = append(out, storageSubObject{storageSubObjectKindNFSExport, cluster, nfs.Metadata.Name})
		}
	}
	for _, export := range state.StorageExports {
		if export.Spec.StorageClusterRef.Name == cluster {
			out = append(out, storageSubObject{storageSubObjectKindExport, cluster, export.Metadata.Name})
		}
	}
	return out
}

// storageSubObjectSpec returns the sub-object's own spec, the only input to its
// desired hash: each sub-object is compared against its own spec, so a change to one
// pool never drifts another or the cluster (which already excludes sub-objects from
// its hash, see storageClusterDesiredHashVars).
func storageSubObjectSpec(state v1alpha1.State, sub storageSubObject) any {
	switch sub.Kind {
	case storageSubObjectKindPool:
		for _, pool := range state.StoragePools {
			if pool.Spec.StorageClusterRef.Name == sub.Cluster && pool.Metadata.Name == sub.Name {
				return storagePoolHashSpec(state, pool)
			}
		}
	case storageSubObjectKindFilesystem:
		for _, fs := range state.StorageFilesystems {
			if fs.Spec.StorageClusterRef.Name == sub.Cluster && fs.Metadata.Name == sub.Name {
				return fs.Spec
			}
		}
	case storageSubObjectKindGateway:
		for _, gw := range state.StorageObjectGateways {
			if gw.Spec.StorageClusterRef.Name == sub.Cluster && gw.Metadata.Name == sub.Name {
				return gw.Spec
			}
		}
	case storageSubObjectKindNFSExport:
		for _, nfs := range state.StorageNFSExports {
			if nfs.Spec.StorageClusterRef.Name == sub.Cluster && nfs.Metadata.Name == sub.Name {
				return nfs.Spec
			}
		}
	case storageSubObjectKindExport:
		for _, export := range state.StorageExports {
			if export.Spec.StorageClusterRef.Name == sub.Cluster && export.Metadata.Name == sub.Name {
				return export.Spec
			}
		}
	}
	return nil
}

// storagePoolHashSpec returns the pool's own spec, plus — when the pool references a
// StoragePlacementPolicy — the resolved policy spec. The pool references its policy by
// name, and the renderer folds the policy's replication and failure domain into the
// live pool, so hashing only pool.Spec would hide an edit to the referenced policy
// from state-check and the apply drift gate. An empty or unresolved placementPolicyRef
// keeps the pool.Spec-only payload (current behavior).
func storagePoolHashSpec(state v1alpha1.State, pool v1alpha1.StoragePool) any {
	if pool.Spec.PlacementPolicyRef.Name == "" {
		return pool.Spec
	}
	for _, policy := range state.StoragePlacementPolicies {
		if policy.Metadata.Name == pool.Spec.PlacementPolicyRef.Name {
			return struct {
				Pool            v1alpha1.StoragePoolSpec            `json:"pool"`
				PlacementPolicy v1alpha1.StoragePlacementPolicySpec `json:"placementPolicy"`
			}{Pool: pool.Spec, PlacementPolicy: policy.Spec}
		}
	}
	return pool.Spec
}

// storageSubObjectDesiredHash hashes the sub-object's own spec plus its identity, in
// the same "sha256:<hex>" form as ApplyTaskDesiredHash. Both the record written after
// apply and the classification before the next apply use this single function, so a
// re-apply with unchanged desired state classifies as match.
func storageSubObjectDesiredHash(state v1alpha1.State, sub storageSubObject) (string, error) {
	payload := struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Cluster    string `json:"cluster"`
		Name       string `json:"name"`
		Spec       any    `json:"spec,omitempty"`
	}{
		APIVersion: v1alpha1.APIVersion,
		Kind:       sub.Kind,
		Cluster:    sub.Cluster,
		Name:       sub.Name,
		Spec:       storageSubObjectSpec(state, sub),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode storage sub-object desired hash input: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// classifyStorageSubObject classifies one sub-object against its durable
// converge-safety record, mirroring classifyApplyTaskState for objects with no
// backing apply task.
func classifyStorageSubObject(state v1alpha1.State, sub storageSubObject, runsDir string) (ConvergeSafetyClassification, error) {
	desiredHash, err := storageSubObjectDesiredHash(state, sub)
	if err != nil {
		return "", err
	}
	record, found, err := LoadConvergeSafetyRecord(runsDir, sub.resourceID())
	if err != nil {
		return "", err
	}
	if !found {
		return ConvergeSafetyMissing, nil
	}
	return ClassifyConvergeSafety(record, desiredHash, ConvergeSafetyOwner), nil
}

// MarkStorageSubObjectsConvergeSafety writes a converge-safety record for every
// sub-object of the named StorageCluster after its apply task reconciles, so the next
// apply's preflight and state-check report sub-object drift (the 5th-pool worked
// example: the cluster and existing pools read match, a new pool reads missing). It
// runs regardless of dataFoundation bindings — unlike PersistCephApplyResult, which
// only writes attachment secrets — because every storage cluster has sub-objects to
// classify.
func MarkStorageSubObjectsConvergeSafety(runsDir, contextName, runID string, state v1alpha1.State, cluster string, status ConvergeSafetyStatus, now time.Time) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	for _, sub := range storageSubObjects(state, cluster) {
		desiredHash, err := storageSubObjectDesiredHash(state, sub)
		if err != nil {
			return err
		}
		record := ConvergeSafetyRecord{
			APIVersion:   ConvergeSafetyAPIVersion,
			ResourceID:   sub.resourceID(),
			ResourceKind: sub.Kind,
			TaskID:       "storage." + cluster,
			TaskKind:     ApplyTaskKindStorageCluster,
			DesiredHash:  desiredHash,
			Owner: ConvergeSafetyOwnerIdentity{
				Manager: ConvergeSafetyOwner,
				Context: effectiveContextName(contextName),
			},
			Observation: ConvergeSafetyObservation{
				Classification: ConvergeSafetyUnknown,
				Reason:         "storage sub-object reconciled through the cephadm operations loop; external probe classification is not available",
			},
			Status:    status,
			RunID:     runID,
			UpdatedAt: now.UTC(),
		}
		if err := SaveConvergeSafetyRecord(runsDir, record); err != nil {
			return err
		}
	}
	return nil
}

// RemoveStorageSubObjectsConvergeSafety deletes the converge-safety records for every
// sub-object of the named StorageCluster (a no-op when absent). Destroy calls it so a
// torn-down cluster's pools/filesystems reclassify as missing and a later apply
// recreates them, instead of a stale record reporting match/drift for a pool that the
// rm-cluster --zap-osds already removed.
func RemoveStorageSubObjectsConvergeSafety(runsDir string, state v1alpha1.State, cluster string) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	for _, sub := range storageSubObjects(state, cluster) {
		if err := RemoveConvergeSafetyRecord(runsDir, sub.resourceID()); err != nil {
			return err
		}
	}
	return nil
}
