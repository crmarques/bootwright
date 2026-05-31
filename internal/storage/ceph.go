package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

type CephApplyResultOptions struct {
	State              v1alpha1.State
	ClustersDir        string
	StorageClusterName string
	ResultPath         string
}

type cephApplyResult struct {
	DataFoundation map[string]render.DataFoundationExternalSecrets `json:"dataFoundation,omitempty"`
}

type dataFoundationBindingContext struct {
	Binding string
	Storage string
	Cluster string
	Export  v1alpha1.StorageExport
	Secrets render.DataFoundationExternalSecrets
}

func PersistCephApplyResult(opts CephApplyResultOptions) (err error) {
	cluster, ok := storageClusterByName(opts.State, opts.StorageClusterName)
	if !ok || cluster.Spec.Ceph == nil {
		return fmt.Errorf("StorageCluster/%s not found", opts.StorageClusterName)
	}
	bindings := dataFoundationBindingContexts(opts.State, cluster.Metadata.Name)
	if len(bindings) == 0 {
		return nil
	}
	if strings.TrimSpace(opts.ClustersDir) == "" {
		return fmt.Errorf("clusters dir is required to persist Data Foundation storage attachment details")
	}
	if strings.TrimSpace(opts.ResultPath) == "" {
		return fmt.Errorf("storage apply result path is required")
	}
	data, readErr := os.ReadFile(opts.ResultPath)
	if readErr != nil {
		return fmt.Errorf("read storage apply result: %w", readErr)
	}
	defer func() {
		removeErr := os.Remove(opts.ResultPath)
		if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && err == nil {
			err = fmt.Errorf("remove storage apply result: %w", removeErr)
		}
	}()
	var result cephApplyResult
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decode storage apply result: %w", err)
	}
	for i := range bindings {
		bindings[i].Secrets = result.DataFoundation[bindings[i].Cluster]
	}
	for _, binding := range bindings {
		if missing := MissingDataFoundationSecrets(binding.Export, binding.Secrets); len(missing) > 0 {
			return fmt.Errorf("data foundation storage attachment %s/%s/%s missing generated credentials: %s", binding.Cluster, binding.Binding, binding.Storage, strings.Join(missing, ", "))
		}
		details := render.DataFoundationExternalDetailsJSON(opts.State, cluster, binding.Export, binding.Cluster, binding.Secrets)
		if err := SaveDataFoundationAttachmentDetails(opts.ClustersDir, binding.Cluster, binding.Binding, binding.Storage, details); err != nil {
			return err
		}
	}
	return nil
}

func dataFoundationBindingContexts(state v1alpha1.State, storageCluster string) []dataFoundationBindingContext {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		if export.Spec.StorageClusterRef.Name == storageCluster && export.Spec.DataFoundation != nil {
			exports[export.Metadata.Name] = export
		}
	}
	var out []dataFoundationBindingContext
	for _, binding := range state.ClusterAddonBindings {
		for _, storage := range binding.Spec.Storage {
			export, ok := exports[storage.ExportRef.Name]
			if !ok {
				continue
			}
			out = append(out, dataFoundationBindingContext{
				Binding: binding.Metadata.Name,
				Storage: storage.Name,
				Cluster: binding.Spec.ClusterRef.Name,
				Export:  export,
			})
		}
	}
	return out
}

func storageClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
}
