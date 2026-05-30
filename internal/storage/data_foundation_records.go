package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/fs"
)

const dataFoundationBindingRecordRelativeDir = "storage-bindings"

type DataFoundationBindingRecord struct {
	StorageCluster string                               `json:"storageCluster"`
	Binding        string                               `json:"binding"`
	Cluster        string                               `json:"cluster"`
	Secrets        render.DataFoundationExternalSecrets `json:"secrets"`
}

func DataFoundationBindingRecordPath(clustersDir, cluster, binding string) string {
	return filepath.Join(clustersDir, cluster, "runtime", dataFoundationBindingRecordRelativeDir, binding+".json")
}

func LoadDataFoundationBindingRecord(clustersDir, cluster, binding string) (DataFoundationBindingRecord, bool, error) {
	path := DataFoundationBindingRecordPath(clustersDir, cluster, binding)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DataFoundationBindingRecord{}, false, nil
	}
	if err != nil {
		return DataFoundationBindingRecord{}, false, fmt.Errorf("read Data Foundation storage binding record: %w", err)
	}
	var record DataFoundationBindingRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return DataFoundationBindingRecord{}, true, fmt.Errorf("decode Data Foundation storage binding record %s: %w", path, err)
	}
	return record, true, nil
}

func SaveDataFoundationBindingRecord(clustersDir string, record DataFoundationBindingRecord) error {
	path := DataFoundationBindingRecordPath(clustersDir, record.Cluster, record.Binding)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Data Foundation storage binding record directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod Data Foundation storage binding record directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Data Foundation storage binding record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write Data Foundation storage binding record: %w", err)
	}
	return nil
}

func MissingDataFoundationSecrets(export v1alpha1.StorageExport, secrets render.DataFoundationExternalSecrets) []string {
	var missing []string
	addIfEmpty := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	addIfEmpty("admin-secret", secrets.AdminSecret)
	addIfEmpty("fsid", secrets.FSID)
	addIfEmpty("mon-secret", secrets.MonSecret)
	addIfEmpty("healthchecker key", secrets.HealthcheckerKey)
	addIfEmpty("RBD node key", secrets.RBDNodeKey)
	addIfEmpty("RBD provisioner key", secrets.RBDProvisionerKey)
	addIfEmpty("CephFS node key", secrets.CephFSNodeKey)
	addIfEmpty("CephFS provisioner key", secrets.CephFSProvisionerKey)
	if export.Spec.DataFoundation != nil && export.Spec.DataFoundation.ObjectGatewayRef.Name != "" {
		addIfEmpty("RGW access key", secrets.RGWAccessKey)
		addIfEmpty("RGW secret key", secrets.RGWSecretKey)
	}
	return missing
}
