package render

import (
	"fmt"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/ceph"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
)

func writeText(fs FileSystem, path string, content string) error {
	if err := fs.WriteAtomic(path, []byte(content), localFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// writeScript writes an executable (0755) generated apply script. Unlike
// writeText/writeYAML it does not carry secret material, so it is written with
// the exec bit set; the owner-only parent dir still gates access.
func writeScript(fs FileSystem, path string, content string) error {
	if err := fs.WriteAtomic(path, []byte(content), localScriptMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// scriptOptionsFor derives the apply script's file references as paths relative
// to the script's own directory (storage/<cluster>/), so the emitted bundle is
// relocatable. An empty source path yields an empty reference (step omitted).
func scriptOptionsFor(asset StorageAsset) ceph.CephScriptOptions {
	rel := func(path string) string {
		if path == "" {
			return ""
		}
		if r, err := filepath.Rel(asset.Dir, path); err == nil {
			return r
		}
		return path
	}
	return ceph.CephScriptOptions{
		LibFile:              rel(asset.ApplyLibPath),
		BootstrapConfFile:    rel(asset.BootstrapConfPath),
		BootstrapSpecFile:    rel(asset.BootstrapSpecPath),
		CoreServicesSpecFile: rel(asset.CoreServicesSpecPath),
		LateServicesSpecFile: rel(asset.LateServicesSpecPath),
	}
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
	// SecretPlaceholders makes the portable render represent a from-secret
	// external-cluster-details document as a "{{ secret <name> }}" token (the
	// whole rook JSON is substituted downstream) instead of the at-apply
	// SecretRef placeholder the context render uses.
	SecretPlaceholders bool
}

func writeStorageAssets(fs FileSystem, assets []StorageAsset, state v1alpha1.State, opts storageAssetWriteOptions) error {
	for _, asset := range assets {
		cluster, ok := stateview.ClusterByName(state, asset.StorageClusterName)
		if !ok {
			continue
		}
		if v1alpha1.StorageClusterManaged(cluster) && cluster.Spec.Ceph != nil {
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
			// The native-CLI apply bundle: a helper library plus an apply.sh
			// that reproduces the same Ceph objects (pools, EC profiles, CRUSH
			// rules, cephfs, rgw, nfs, mgr modules, config) using cephadm/ceph
			// commands. Both consume the same CephOperations document written
			// above, so they can never drift from what `bootwright apply` runs.
			if asset.ApplyLibPath != "" {
				if err := writeScript(fs, asset.ApplyLibPath, ceph.CephApplyLib()); err != nil {
					return err
				}
			}
			if asset.ApplyScriptPath != "" {
				if err := writeScript(fs, asset.ApplyScriptPath, ceph.CephApplyScript(state, cluster, scriptOptionsFor(asset))); err != nil {
					return err
				}
			}
		}
		for _, attachmentAsset := range asset.Attachments {
			attachment, ok := ceph.StorageAttachmentByName(state, attachmentAsset.ContainerClusterName, attachmentAsset.AddonName, attachmentAsset.InputName)
			if !ok {
				continue
			}
			exportRef := ceph.AddonInputStorageExportRef(attachment)
			export, ok := stateview.ExportByName(state, exportRef.Name)
			if !ok {
				continue
			}
			externalDetails := dataFoundationExternalDetailsManifest(state, cluster, export, attachment, attachmentAsset.ContainerClusterName, opts.SecretPlaceholders)
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

func dataFoundationExternalDetailsManifest(state v1alpha1.State, cluster v1alpha1.StorageCluster, export v1alpha1.StorageExport, attachment ceph.StorageAttachment, containerCluster string, secretPlaceholders bool) map[string]any {
	if source := datafoundation.ExternalDetailsSourceFromSecret(export); source != "" {
		// The portable render tokenizes the whole rook external-details JSON as a
		// substitutable secret; the context render leaves the at-apply SecretRef
		// placeholder for bootwright apply to fill from the named secret.
		if secretPlaceholders {
			return ceph.DataFoundationExternalDetailsRawJSONManifest(attachment, secret.SecretPlaceholder(source, ""), source)
		}
		return ceph.DataFoundationExternalDetailsRefPlaceholderManifest(attachment, source)
	}
	if datafoundation.ExternalDetailsSourceSSH(export) != nil {
		return ceph.DataFoundationExternalDetailsRefPlaceholderManifest(attachment, "sshExecution")
	}
	return ceph.DataFoundationExternalDetailsManifest(state, cluster, export, attachment, containerCluster, datafoundation.ExternalSecrets{})
}
