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
	BootstrapConfPath    string
	BootstrapSpecPath    string
	CoreServicesSpecPath string
	LateServicesSpecPath string
	OperationsPath       string
	ApplyScriptPath      string
	ApplyLibPath         string
}

func (a StorageAsset) Directories() []string {
	var dirs []string
	for _, dir := range []string{a.Dir, a.CephadmDir, a.CephDir} {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func StorageAssets(baseDir string, state v1alpha1.State) []StorageAsset {
	var assets []StorageAsset
	for _, cluster := range state.StorageClusters {
		dir := filepath.Join(baseDir, "storage", cluster.Metadata.Name)
		asset := StorageAsset{
			StorageClusterName: cluster.Metadata.Name,
			Dir:                dir,
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
			asset.ApplyScriptPath = filepath.Join(dir, "apply.sh")
			asset.ApplyLibPath = filepath.Join(dir, "lib.sh")
		}
		assets = append(assets, asset)
	}
	return assets
}
