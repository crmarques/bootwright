package ceph

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
	BootstrapConfPath    string
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
		if v1alpha1.StorageClusterManaged(cluster) {
			asset.CephadmDir = filepath.Join(dir, "cephadm")
			asset.CephDir = filepath.Join(dir, "ceph")
			if cluster.Spec.Ceph != nil && CephadmBootstrapConf(cluster) != "" {
				asset.BootstrapConfPath = filepath.Join(dir, "cephadm", "bootstrap-ceph.conf")
			}
			asset.BootstrapSpecPath = filepath.Join(dir, "cephadm", "bootstrap-spec.yaml")
			asset.CoreServicesSpecPath = filepath.Join(dir, "cephadm", "core-services.yaml")
			asset.LateServicesSpecPath = filepath.Join(dir, "cephadm", "late-services.yaml")
			asset.OperationsPath = filepath.Join(dir, "ceph", "operations.yaml")
		}
		for _, attachment := range attachmentsByCluster[cluster.Metadata.Name] {
			containerCluster := attachment.Binding.Spec.ClusterRef.Name
			inputDir := filepath.Join(asset.DataFoundationDir, containerCluster, attachment.Addon.AddonRef.Name, attachment.Input.Name)
			asset.Attachments = append(asset.Attachments, StorageAttachmentAsset{
				AddonName:                  attachment.Addon.AddonRef.Name,
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

