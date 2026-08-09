package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

const (
	storageSubObjectKindPool       = "StoragePool"
	storageSubObjectKindFilesystem = "StorageFilesystem"
	storageSubObjectKindGateway    = "StorageObjectGateway"
	storageSubObjectKindNFSExport  = "StorageNFSExport"
	storageSubObjectKindExport     = "StorageExport"
)

func IsStorageSubObjectKind(kind string) bool {
	switch kind {
	case storageSubObjectKindPool, storageSubObjectKindFilesystem, storageSubObjectKindGateway, storageSubObjectKindNFSExport, storageSubObjectKindExport:
		return true
	}
	return false
}

type storageSubObject struct {
	Kind    string
	Cluster string
	Name    string
}

func (s storageSubObject) resourceID() string {
	return s.Kind + "/" + s.Cluster + "." + s.Name
}

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

func storageSubObjectDesiredHash(state v1alpha1.State, sub storageSubObject) (string, error) {
	data, err := storageSubObjectDesiredHashInput(state, sub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func storageSubObjectDesiredHashInput(state v1alpha1.State, sub storageSubObject) ([]byte, error) {
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
		return nil, fmt.Errorf("encode storage sub-object desired hash input: %w", err)
	}
	return data, nil
}

func storageSubObjectStructuralSpec(state v1alpha1.State, sub storageSubObject) any {
	switch sub.Kind {
	case storageSubObjectKindPool:
		for _, pool := range state.StoragePools {
			if pool.Spec.StorageClusterRef.Name == sub.Cluster && pool.Metadata.Name == sub.Name {
				failureDomain := ""
				if pool.Spec.Ceph.Type == v1alpha1.StoragePoolTypeErasureCode {
					for _, cluster := range state.StorageClusters {
						if cluster.Metadata.Name == sub.Cluster && cluster.Spec.Ceph != nil {
							failureDomain = topology.StoragePoolFailureDomain(state, cluster, pool)
							break
						}
					}
				}
				return struct {
					Type          string                           `json:"type,omitempty"`
					Erasure       *v1alpha1.StoragePoolErasureCode `json:"erasure,omitempty"`
					FailureDomain string                           `json:"failureDomain,omitempty"`
				}{Type: pool.Spec.Ceph.Type, Erasure: pool.Spec.Ceph.ErasureCoded, FailureDomain: failureDomain}
			}
		}
	case storageSubObjectKindFilesystem:
		for _, fs := range state.StorageFilesystems {
			if fs.Spec.StorageClusterRef.Name == sub.Cluster && fs.Metadata.Name == sub.Name {
				return struct {
					MetadataPool    string `json:"metadataPool"`
					DefaultDataPool string `json:"defaultDataPool"`
				}{
					MetadataPool:    fs.Spec.CephFS.MetadataPoolRef.Name,
					DefaultDataPool: cephFSDefaultDataPool(fs.Spec.CephFS),
				}
			}
		}
	case storageSubObjectKindGateway, storageSubObjectKindNFSExport, storageSubObjectKindExport:
		return struct {
			Kind    string `json:"kind"`
			Cluster string `json:"cluster"`
			Name    string `json:"name"`
		}{Kind: sub.Kind, Cluster: sub.Cluster, Name: sub.Name}
	}
	return nil
}

func cephFSDefaultDataPool(spec v1alpha1.StorageCephFSSpec) string {
	for _, ref := range spec.DataPoolRefs {
		if ref.Default {
			return ref.Name
		}
	}
	if len(spec.DataPoolRefs) > 0 {
		return spec.DataPoolRefs[0].Name
	}
	return ""
}

func storageSubObjectStructuralHash(state v1alpha1.State, sub storageSubObject) (string, error) {
	structural := storageSubObjectStructuralSpec(state, sub)
	if structural == nil {
		return "", nil
	}
	payload := struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Cluster    string `json:"cluster"`
		Name       string `json:"name"`
		Structural any    `json:"structural"`
	}{
		APIVersion: v1alpha1.APIVersion,
		Kind:       sub.Kind,
		Cluster:    sub.Cluster,
		Name:       sub.Name,
		Structural: structural,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode storage sub-object structural hash input: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func storageSubObjectReconcilableDrift(state v1alpha1.State, sub storageSubObject, runsDir string) (bool, error) {
	structuralHash, err := storageSubObjectStructuralHash(state, sub)
	if err != nil {
		return false, err
	}
	desiredHash, err := storageSubObjectDesiredHash(state, sub)
	if err != nil {
		return false, err
	}
	record, found, err := LoadConvergeSafetyRecord(runsDir, sub.resourceID())
	if err != nil || !found {
		return false, err
	}
	if structuralHash == "" {
		return false, nil
	}
	return IsReconcilableDrift(record, desiredHash, structuralHash), nil
}

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
	return classifyStorageSubObjectWithRecord(state, sub, runsDir, record, desiredHash)
}

func classifyStorageSubObjectWithRecord(state v1alpha1.State, sub storageSubObject, runsDir string, record ConvergeSafetyRecord, desiredHash string) (ConvergeSafetyClassification, error) {
	class := ClassifyConvergeSafety(record, desiredHash, ConvergeSafetyOwner)
	if class == ConvergeSafetyMissing || class == ConvergeSafetyForeign || record.HashSchema != ConvergeHashSchema-1 {
		return class, nil
	}
	taskID := "storage." + sub.Cluster
	if record.ResourceID != sub.resourceID() || record.TaskID != taskID || record.TaskKind != ApplyTaskKindStorageCluster {
		return ConvergeSafetyUnknown, &LegacyConvergenceEvidenceError{ResourceID: sub.resourceID(), Cause: fmt.Errorf("the converge record has ambiguous resource or task identity")}
	}
	input, err := storageSubObjectDesiredHashInput(state, sub)
	if err != nil {
		return ConvergeSafetyUnknown, err
	}
	matched, err := successfulInputSnapshotMatches(runsDir, record.RunID, sub.resourceID(), taskID, ApplyTaskKindStorageCluster, successfulTaskStatus(record.Status), record.HashSchema, input)
	if err != nil {
		return ConvergeSafetyUnknown, &LegacyConvergenceEvidenceError{ResourceID: sub.resourceID(), Cause: err}
	}
	if matched {
		return ConvergeSafetyMatch, nil
	}
	return ConvergeSafetyDrift, nil
}

func MarkStorageSubObjectsConvergeSafety(runsDir, contextName, runID string, state v1alpha1.State, cluster string, status ConvergeSafetyStatus, now time.Time) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(cluster) == "" {
		return nil
	}
	for _, sub := range storageSubObjects(state, cluster) {
		input, err := storageSubObjectDesiredHashInput(state, sub)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(input)
		desiredHash := "sha256:" + hex.EncodeToString(sum[:])
		structuralHash, err := storageSubObjectStructuralHash(state, sub)
		if err != nil {
			return err
		}
		taskID := "storage." + cluster
		if err := saveSuccessfulInputSnapshot(runsDir, runID, sub.resourceID(), taskID, ApplyTaskKindStorageCluster, successfulTaskStatus(status), ConvergeHashSchema, input); err != nil {
			return err
		}
		record := ConvergeSafetyRecord{
			APIVersion:     ConvergeSafetyAPIVersion,
			ResourceID:     sub.resourceID(),
			ResourceKind:   sub.Kind,
			TaskID:         taskID,
			TaskKind:       ApplyTaskKindStorageCluster,
			DesiredHash:    desiredHash,
			StructuralHash: structuralHash,
			HashSchema:     ConvergeHashSchema,
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
