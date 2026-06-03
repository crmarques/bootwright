package render

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type StorageAsset struct {
	StorageClusterName   string
	Dir                  string
	CephadmDir           string
	CephDir              string
	DataFoundationDir    string
	BootstrapSpecPath    string
	CoreServicesSpecPath string
	LateServicesSpecPath string
	OperationsPath       string
	Attachments          []StorageAttachmentAsset
}

type StorageAttachmentAsset struct {
	AddonName                  string
	InputName                  string
	ContainerClusterName       string
	Dir                        string
	ExternalClusterDetailsPath string
	StorageClusterPath         string
	StorageSystemPath          string
}

type storageAssetWriteOptions struct {
	ExternalDetailsSecretsDir string
}

func (a StorageAsset) Directories() []string {
	var dirs []string
	for _, dir := range []string{a.Dir, a.CephadmDir, a.CephDir, a.DataFoundationDir} {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	for _, attachment := range a.Attachments {
		dirs = append(dirs, attachment.Dir)
	}
	return dirs
}

func StorageAssets(baseDir string, state v1alpha1.State) []StorageAsset {
	attachmentsByCluster := storageAttachmentsByStorageCluster(state)
	var assets []StorageAsset
	for _, cluster := range state.StorageClusters {
		dir := filepath.Join(baseDir, "storage", cluster.Metadata.Name)
		asset := StorageAsset{
			StorageClusterName: cluster.Metadata.Name,
			Dir:                dir,
			DataFoundationDir:  filepath.Join(dir, "data-foundation"),
		}
		if storageClusterManaged(cluster) {
			asset.CephadmDir = filepath.Join(dir, "cephadm")
			asset.CephDir = filepath.Join(dir, "ceph")
			asset.BootstrapSpecPath = filepath.Join(dir, "cephadm", "bootstrap-spec.yaml")
			asset.CoreServicesSpecPath = filepath.Join(dir, "cephadm", "core-services.yaml")
			asset.LateServicesSpecPath = filepath.Join(dir, "cephadm", "late-services.yaml")
			asset.OperationsPath = filepath.Join(dir, "ceph", "operations.yaml")
		}
		for _, attachment := range attachmentsByCluster[cluster.Metadata.Name] {
			containerCluster := attachment.Binding.Spec.ClusterRef.Name
			inputDir := filepath.Join(asset.DataFoundationDir, containerCluster, attachment.Addon.Name, attachment.Input.Name)
			asset.Attachments = append(asset.Attachments, StorageAttachmentAsset{
				AddonName:                  attachment.Addon.Name,
				InputName:                  attachment.Input.Name,
				ContainerClusterName:       containerCluster,
				Dir:                        inputDir,
				ExternalClusterDetailsPath: filepath.Join(inputDir, "rook-ceph-external-cluster-details.yaml"),
				StorageClusterPath:         filepath.Join(inputDir, "ocs-external-storagecluster.yaml"),
				StorageSystemPath:          filepath.Join(inputDir, "ocs-external-storagesystem.yaml"),
			})
		}
		assets = append(assets, asset)
	}
	return assets
}

func writeStorageAssets(fs FileSystem, assets []StorageAsset, state v1alpha1.State, opts storageAssetWriteOptions) error {
	for _, asset := range assets {
		cluster, ok := storageClusterByName(state, asset.StorageClusterName)
		if !ok {
			continue
		}
		if storageClusterManaged(cluster) && cluster.Spec.Ceph != nil {
			if err := writeYAMLDocuments(fs, asset.BootstrapSpecPath, cephadmBootstrapSpec(state, cluster)); err != nil {
				return err
			}
			if err := writeYAMLDocuments(fs, asset.CoreServicesSpecPath, cephadmCoreServicesSpec(state, cluster)); err != nil {
				return err
			}
			if err := writeYAMLDocuments(fs, asset.LateServicesSpecPath, cephadmLateServicesSpec(state, cluster)); err != nil {
				return err
			}
			if err := writeYAML(fs, asset.OperationsPath, cephOperations(state, cluster)); err != nil {
				return err
			}
		}
		for _, attachmentAsset := range asset.Attachments {
			attachment, ok := storageAttachmentByName(state, attachmentAsset.ContainerClusterName, attachmentAsset.AddonName, attachmentAsset.InputName)
			if !ok {
				continue
			}
			exportRef := addonInputStorageExportRef(attachment)
			export, ok := storageExportByName(state, exportRef.Name)
			if !ok {
				continue
			}
			externalDetails := dataFoundationExternalDetailsManifest(state, cluster, export, attachment, attachmentAsset.ContainerClusterName)
			externalDetailsRef := addonInputExternalDetailsRef(attachment)
			if externalDetailsRef.Name != "" && opts.ExternalDetailsSecretsDir != "" {
				detailsJSON, err := LoadDataFoundationExternalDetailsJSON(state, opts.ExternalDetailsSecretsDir, externalDetailsRef)
				if err != nil {
					return err
				}
				externalDetails = DataFoundationExternalDetailsRawJSONManifest(attachment, detailsJSON, externalDetailsRef.Name)
			}
			if err := writeYAML(fs, attachmentAsset.ExternalClusterDetailsPath, externalDetails); err != nil {
				return err
			}
			if err := writeYAML(fs, attachmentAsset.StorageClusterPath, dataFoundationStorageClusterManifest()); err != nil {
				return err
			}
			if err := writeYAML(fs, attachmentAsset.StorageSystemPath, dataFoundationStorageSystemManifest()); err != nil {
				return err
			}
		}
	}
	return nil
}

func storageClusterManaged(cluster v1alpha1.StorageCluster) bool {
	return cluster.Spec.Management == "" || cluster.Spec.Management == v1alpha1.StorageClusterManagementManaged
}
