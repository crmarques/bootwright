package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/runtime/fs"
)

const dataFoundationAttachmentSecretRelativeDir = "storage-attachments"

func DataFoundationAttachmentDetailsPath(clustersDir, cluster, binding, storage string) string {
	return filepath.Join(clustersDir, cluster, "secrets", dataFoundationAttachmentSecretRelativeDir, binding, storage, "external-cluster-details.json")
}

func LoadDataFoundationAttachmentDetails(clustersDir, cluster, binding, storage string) (string, bool, error) {
	path := DataFoundationAttachmentDetailsPath(clustersDir, cluster, binding, storage)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read Data Foundation storage attachment details: %w", err)
	}
	details, err := render.NormalizeDataFoundationExternalDetailsJSON(binding+"/"+storage, path, data)
	if err != nil {
		return "", true, err
	}
	return details, true, nil
}

func SaveDataFoundationAttachmentDetails(clustersDir, cluster, binding, storage, detailsJSON string) error {
	path := DataFoundationAttachmentDetailsPath(clustersDir, cluster, binding, storage)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Data Foundation storage attachment details directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod Data Foundation storage attachment details directory: %w", err)
	}
	details, err := render.NormalizeDataFoundationExternalDetailsJSON(binding+"/"+storage, path, []byte(detailsJSON))
	if err != nil {
		return err
	}
	if err := safefs.AtomicWriteFile(path, append([]byte(details), '\n'), 0o600); err != nil {
		return fmt.Errorf("write Data Foundation storage attachment details: %w", err)
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
