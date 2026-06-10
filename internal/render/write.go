package render

import (
	"fmt"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/ceph"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func writeText(fs FileSystem, path string, content string) error {
	if err := fs.WriteAtomic(path, []byte(content), localFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeYAML(fs FileSystem, path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := fs.WriteAtomic(path, data, localFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeYAMLDocuments(fs FileSystem, path string, values []any) error {
	var data []byte
	for i, value := range values {
		chunk, err := yaml.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", path, err)
		}
		if i > 0 {
			data = append(data, []byte("---\n")...)
		}
		data = append(data, chunk...)
	}
	if err := fs.WriteAtomic(path, data, localFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

type storageAssetWriteOptions struct {
	ContextName               string
	ExternalDetailsSecretsDir string
}

func writeStorageAssets(fs FileSystem, assets []StorageAsset, state v1alpha1.State, opts storageAssetWriteOptions) error {
	for _, asset := range assets {
		cluster, ok := topology.ClusterByName(state, asset.StorageClusterName)
		if !ok {
			continue
		}
		if ceph.StorageClusterManaged(cluster) && cluster.Spec.Ceph != nil {
			if asset.BootstrapConfPath != "" {
				if err := writeText(fs, asset.BootstrapConfPath, ceph.CephadmBootstrapConf(cluster)); err != nil {
					return err
				}
			}
			if err := writeYAMLDocuments(fs, asset.BootstrapSpecPath, ceph.CephadmBootstrapSpec(state, cluster)); err != nil {
				return err
			}
			if err := writeYAMLDocuments(fs, asset.CoreServicesSpecPath, ceph.CephadmCoreServicesSpec(state, cluster)); err != nil {
				return err
			}
			if err := writeYAMLDocuments(fs, asset.LateServicesSpecPath, ceph.CephadmLateServicesSpec(state, cluster)); err != nil {
				return err
			}
			if err := writeYAML(fs, asset.OperationsPath, ceph.CephOperations(state, cluster)); err != nil {
				return err
			}
		}
		for _, attachmentAsset := range asset.Attachments {
			attachment, ok := ceph.StorageAttachmentByName(state, attachmentAsset.ContainerClusterName, attachmentAsset.AddonName, attachmentAsset.InputName)
			if !ok {
				continue
			}
			exportRef := ceph.AddonInputStorageExportRef(attachment)
			export, ok := topology.ExportByName(state, exportRef.Name)
			if !ok {
				continue
			}
			externalDetails := dataFoundationExternalDetailsManifest(state, cluster, export, attachment, attachmentAsset.ContainerClusterName)
			fromSecret := datafoundation.ExternalDetailsSourceFromSecret(export)
			if fromSecret != "" && opts.ExternalDetailsSecretsDir != "" {
				detailsJSON, err := datafoundation.LoadExternalDetailsSecretJSONForContext(opts.ContextName, state, opts.ExternalDetailsSecretsDir, fromSecret)
				if err != nil {
					return err
				}
				externalDetails = ceph.DataFoundationExternalDetailsRawJSONManifest(attachment, detailsJSON, fromSecret)
			}
			if err := writeYAML(fs, attachmentAsset.ExternalClusterDetailsPath, externalDetails); err != nil {
				return err
			}
			if err := writeYAML(fs, attachmentAsset.StorageClusterPath, ceph.DataFoundationStorageClusterManifest()); err != nil {
				return err
			}
			if err := writeYAML(fs, attachmentAsset.StorageSystemPath, ceph.DataFoundationStorageSystemManifest()); err != nil {
				return err
			}
		}
	}
	return nil
}

func dataFoundationExternalDetailsManifest(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, attachment ceph.StorageAttachment, containerCluster string) map[string]any {
	if source := datafoundation.ExternalDetailsSourceFromSecret(export); source != "" {
		return ceph.DataFoundationExternalDetailsRefPlaceholderManifest(attachment, source)
	}
	if datafoundation.ExternalDetailsSourceSSH(export) != nil {
		return ceph.DataFoundationExternalDetailsRefPlaceholderManifest(attachment, "sshExecution")
	}
	return ceph.DataFoundationExternalDetailsManifest(state, cluster, export, attachment, containerCluster, datafoundation.ExternalSecrets{})
}
