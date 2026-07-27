package render

import (
	"fmt"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/ceph"
	"github.com/crmarques/bootwright/internal/render/inventory"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func writeText(fs FileSystem, path string, content string) error {
	if err := fs.WriteAtomic(path, []byte(content), localFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeScript(fs FileSystem, path string, content string) error {
	if err := fs.WriteAtomic(path, []byte(content), localScriptMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func scriptOptionsFor(asset StorageAsset, state v1alpha1.State, cluster v1alpha1.StorageCluster) ceph.CephScriptOptions {
	provider := inventory.StorageCephProvider(state, cluster)
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
		BootstrapImage:       provider.Image,
		AcceptLicense:        provider.RequiresLicense && provider.Entitlement.License.Accepted,
		IBMCallHome:          provider.IBMCallHome,
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

func writeStorageAssets(fs FileSystem, assets []StorageAsset, state v1alpha1.State) error {
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
			if asset.ApplyLibPath != "" {
				if err := writeScript(fs, asset.ApplyLibPath, ceph.CephApplyLib()); err != nil {
					return err
				}
			}
			if asset.ApplyScriptPath != "" {
				script, err := ceph.CephApplyScript(state, cluster, scriptOptionsFor(asset, state, cluster))
				if err != nil {
					return err
				}
				if err := writeScript(fs, asset.ApplyScriptPath, script); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
