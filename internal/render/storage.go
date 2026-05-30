package render

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type StorageAsset struct {
	StorageClusterName string
	Dir                string
	CephadmDir         string
	CephDir            string
	DataFoundationDir  string
	BootstrapSpecPath  string
	ServicesSpecPath   string
	OperationsPath     string
	Bindings           []StorageBindingAsset
}

type StorageBindingAsset struct {
	BindingName                string
	ContainerClusterName       string
	Dir                        string
	ExternalClusterDetailsPath string
	StorageClusterPath         string
	StorageSystemPath          string
}

func (a StorageAsset) Directories() []string {
	dirs := []string{a.Dir, a.CephadmDir, a.CephDir, a.DataFoundationDir}
	for _, binding := range a.Bindings {
		dirs = append(dirs, binding.Dir)
	}
	return dirs
}

func StorageAssets(baseDir string, state v1alpha1.State) []StorageAsset {
	bindingsByCluster := storageBindingsByStorageCluster(state)
	var assets []StorageAsset
	for _, cluster := range state.StorageClusters {
		dir := filepath.Join(baseDir, "storage", cluster.Metadata.Name)
		asset := StorageAsset{
			StorageClusterName: cluster.Metadata.Name,
			Dir:                dir,
			CephadmDir:         filepath.Join(dir, "cephadm"),
			CephDir:            filepath.Join(dir, "ceph"),
			DataFoundationDir:  filepath.Join(dir, "data-foundation"),
			BootstrapSpecPath:  filepath.Join(dir, "cephadm", "bootstrap-spec.yaml"),
			ServicesSpecPath:   filepath.Join(dir, "cephadm", "services.yaml"),
			OperationsPath:     filepath.Join(dir, "ceph", "operations.yaml"),
		}
		for _, binding := range bindingsByCluster[cluster.Metadata.Name] {
			for _, containerCluster := range binding.Spec.ClusterSelector.Names {
				bindingDir := filepath.Join(asset.DataFoundationDir, binding.Metadata.Name, containerCluster)
				asset.Bindings = append(asset.Bindings, StorageBindingAsset{
					BindingName:                binding.Metadata.Name,
					ContainerClusterName:       containerCluster,
					Dir:                        bindingDir,
					ExternalClusterDetailsPath: filepath.Join(bindingDir, "rook-ceph-external-cluster-details.yaml"),
					StorageClusterPath:         filepath.Join(bindingDir, "ocs-external-storagecluster.yaml"),
					StorageSystemPath:          filepath.Join(bindingDir, "ocs-external-storagesystem.yaml"),
				})
			}
		}
		assets = append(assets, asset)
	}
	return assets
}

func writeStorageAssets(fs FileSystem, assets []StorageAsset, state v1alpha1.State) error {
	for _, asset := range assets {
		cluster, ok := storageClusterByName(state, asset.StorageClusterName)
		if !ok || cluster.Spec.Ceph == nil {
			continue
		}
		if err := writeYAMLDocuments(fs, asset.BootstrapSpecPath, cephadmBootstrapSpec(state, cluster)); err != nil {
			return err
		}
		if err := writeYAMLDocuments(fs, asset.ServicesSpecPath, cephadmServicesSpec(state, cluster)); err != nil {
			return err
		}
		if err := writeYAML(fs, asset.OperationsPath, cephOperations(state, cluster)); err != nil {
			return err
		}
		for _, bindingAsset := range asset.Bindings {
			binding, ok := storageBindingByName(state, bindingAsset.BindingName)
			if !ok {
				continue
			}
			export, ok := storageExportByName(state, binding.Spec.StorageExportRef.Name)
			if !ok || export.Spec.DataFoundation == nil {
				continue
			}
			if err := writeYAML(fs, bindingAsset.ExternalClusterDetailsPath, dataFoundationExternalDetailsManifest(state, cluster, export, binding, bindingAsset.ContainerClusterName)); err != nil {
				return err
			}
			if err := writeYAML(fs, bindingAsset.StorageClusterPath, dataFoundationStorageClusterManifest(binding)); err != nil {
				return err
			}
			if err := writeYAML(fs, bindingAsset.StorageSystemPath, dataFoundationStorageSystemManifest(binding)); err != nil {
				return err
			}
		}
	}
	return nil
}
