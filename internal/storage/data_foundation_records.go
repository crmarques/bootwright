package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
)

const dataFoundationAddonSecretRelativeDir = "addons"

func DataFoundationAttachmentDetailsPath(clustersDir, cluster, addon, input string) string {
	return filepath.Join(clustersDir, cluster, "secrets", dataFoundationAddonSecretRelativeDir, addon, "inputs", input, "external-cluster-details.json")
}

func LoadDataFoundationAttachmentDetails(clustersDir, cluster, addon, input string) (string, bool, error) {
	path := DataFoundationAttachmentDetailsPath(clustersDir, cluster, addon, input)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read Data Foundation storage attachment details: %w", err)
	}
	details, err := datafoundation.NormalizeExternalDetailsJSON(addon+"/"+input, path, data)
	if err != nil {
		return "", true, err
	}
	return details, true, nil
}

func SaveDataFoundationAttachmentDetails(clustersDir, cluster, addon, input, detailsJSON string) error {
	path := DataFoundationAttachmentDetailsPath(clustersDir, cluster, addon, input)
	details, err := datafoundation.NormalizeExternalDetailsJSON(addon+"/"+input, path, []byte(detailsJSON))
	if err != nil {
		return err
	}
	if err := safefs.WriteFileEnsuringDir(path, append([]byte(details), '\n'), 0o600); err != nil {
		return fmt.Errorf("write Data Foundation storage attachment details: %w", err)
	}
	return nil
}

func MissingDataFoundationSecrets(export v1alpha1.StorageExport, secrets datafoundation.ExternalSecrets) []string {
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
