package render

import (
	"fmt"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/managedroot"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render/ceph"
	"github.com/crmarques/bootwright/internal/render/installer"
	"github.com/crmarques/bootwright/internal/render/inventory"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

type Result struct {
	EffectiveStatePath string
	LockPath           string
	InventoryPath      string
	VarsPath           string
	ArtifactsDir       string
	InstallerAssets    []InstallerAsset
	StorageAssets      []StorageAsset
}

func All(renderedDir, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return AllOn(defaultFS, renderedDir, clustersDir, secretsDir, state)
}

func AllWithPathOptions(renderedDir, clustersDir string, paths PathOptions, state v1alpha1.State) (Result, error) {
	return allOn(defaultFS, renderedDir, clustersDir, paths, state, nil)
}

func AllWithOwnershipRecordsAndPathOptions(renderedDir, clustersDir string, paths PathOptions, state v1alpha1.State, records []ownership.ResourceRecord) (Result, error) {
	return allOn(defaultFS, renderedDir, clustersDir, paths, state, records)
}

func RunInputs(renderDir string, paths PathOptions, state v1alpha1.State, records []ownership.ResourceRecord) (Result, error) {
	return runInputsOn(defaultFS, renderDir, paths, state, records)
}

func runInputsOn(fs FileSystem, baseDir string, paths PathOptions, state v1alpha1.State, records []ownership.ResourceRecord) (Result, error) {
	if err := checkResolvedNames(state); err != nil {
		return Result{}, err
	}
	result := Result{
		InventoryPath: filepath.Join(baseDir, "ansible", "inventory.yaml"),
		VarsPath:      filepath.Join(baseDir, "ansible", "vars.yaml"),
		ArtifactsDir:  filepath.Join(baseDir, "ansible", "artifacts"),
		StorageAssets: ceph.StorageAssets(baseDir, state),
	}
	dirs := []string{baseDir, filepath.Dir(result.InventoryPath), result.ArtifactsDir}
	for _, asset := range result.StorageAssets {
		dirs = append(dirs, asset.Directories()...)
	}
	for _, dir := range dirs {
		if err := ensureLocalDir(fs, dir); err != nil {
			return result, err
		}
	}
	if err := writeYAML(fs, result.InventoryPath, inventory.InventoryWithOwnershipRecordsAndPathOptions(state, paths, records)); err != nil {
		return result, err
	}
	if err := writeYAML(fs, result.VarsPath, inventory.VarsWithPathOptionsAndOwnership(state, paths, records)); err != nil {
		return result, err
	}
	if err := writeStorageAssets(fs, result.StorageAssets, state); err != nil {
		return result, err
	}
	return result, nil
}

func Effective(renderedDir string, state v1alpha1.State) (Result, error) {
	return EffectiveOn(defaultFS, renderedDir, state)
}

func EffectiveOn(fs FileSystem, renderedDir string, state v1alpha1.State) (Result, error) {
	if err := checkResolvedNames(state); err != nil {
		return Result{}, err
	}
	result := Result{EffectiveStatePath: filepath.Join(renderedDir, "effective-state.yaml")}
	if err := ensureLocalDir(fs, renderedDir); err != nil {
		return result, err
	}
	if err := writeYAML(fs, result.EffectiveStatePath, EffectiveState(state)); err != nil {
		return result, err
	}
	return result, nil
}

func AllOn(fs FileSystem, renderedDir, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return allOn(fs, renderedDir, clustersDir, PathOptions{SecretsDir: secretsDir}, state, nil)
}

func allOn(fs FileSystem, renderedDir, clustersDir string, paths PathOptions, state v1alpha1.State, records []ownership.ResourceRecord) (Result, error) {
	return renderCore(fs, renderedDir, state, renderParams{
		installerAssets: func(s v1alpha1.State) []InstallerAsset { return InstallerAssets(clustersDir, s) },
		inventory: func(s v1alpha1.State) any {
			return inventory.InventoryWithOwnershipRecordsAndPathOptions(s, paths, records)
		},
		vars: func(s v1alpha1.State) any {
			return inventory.VarsWithPathOptionsAndOwnership(s, paths, records)
		},
		installerSecrets: func(s v1alpha1.State, ocp v1alpha1.ContainerCluster) (installer.InstallerSecrets, error) {
			return PlaceholderInstallerSecrets(s, ocp), nil
		},
	})
}

func ResolveInstallerForContext(contextName, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return ResolveInstallerOnForContext(defaultFS, contextName, clustersDir, secretsDir, state)
}

func ResolveInstallerOnForContext(fs FileSystem, contextName, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	if err := checkResolvedNames(state); err != nil {
		return Result{}, err
	}
	result := Result{InstallerAssets: InstallerAssets(clustersDir, state)}
	for _, ocp := range state.ContainerClusters {
		asset := installerAssetFor(result.InstallerAssets, ocp.Metadata.Name)
		secrets, err := installer.LoadInstallerSecretsForContext(contextName, state, ocp, secretsDir)
		if err != nil {
			return result, err
		}
		for _, dir := range runtimeInstallerDirs(asset) {
			if err := ensureLocalDir(fs, dir); err != nil {
				return result, err
			}
		}
		installConfig, err := installer.InstallerConfigWithSecrets(state, ocp, secrets)
		if err != nil {
			return result, err
		}
		if err := writeYAML(fs, asset.EffectiveInstallConfigPath, installConfig); err != nil {
			return result, err
		}
		agentConfig, err := AgentConfig(state, ocp)
		if err != nil {
			return result, err
		}
		if err := writeYAML(fs, asset.EffectiveAgentConfigPath, agentConfig); err != nil {
			return result, err
		}
		if err := writeInstallerManifests(fs, asset.EffectiveInstallManifestsDir, InstallerManifests(ocp, secrets)); err != nil {
			return result, err
		}
	}
	return result, nil
}

func runtimeInstallerDirs(asset InstallerAsset) []string {
	clusterRuntimeDir := filepath.Dir(asset.WorkDir)
	return []string{asset.ClusterDir, clusterRuntimeDir, asset.WorkDir, asset.ClusterSecretsDir}
}

func ToolInputsForContext(contextName, outputDir, secretsDir string, state v1alpha1.State) (Result, error) {
	cleanOutputDir, err := managedroot.Ensure(outputDir, localDirMode)
	if err != nil {
		return Result{}, err
	}
	return ToolInputsOnForContext(defaultFS, contextName, cleanOutputDir, secretsDir, state)
}

func ToolInputsOnForContext(fs FileSystem, contextName, outputDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return renderCore(fs, outputDir, state, renderParams{
		installerAssets: func(s v1alpha1.State) []InstallerAsset { return installer.InstallerToolInputAssets(outputDir, s) },
		inventory:       func(s v1alpha1.State) any { return inventory.Inventory(s, secretsDir) },
		vars:            func(s v1alpha1.State) any { return inventory.VarsWithSecretsDir(s, secretsDir) },
		installerSecrets: func(s v1alpha1.State, ocp v1alpha1.ContainerCluster) (installer.InstallerSecrets, error) {
			return installer.LoadInstallerSecretsForContext(contextName, s, ocp, secretsDir)
		},
	})
}

func ToolInputsPortable(outputDir string, state v1alpha1.State) (Result, error) {
	cleanOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve output dir %s: %w", outputDir, err)
	}
	return ToolInputsPortableOn(defaultFS, cleanOutputDir, state)
}

func ToolInputsPortableOn(fs FileSystem, outputDir string, state v1alpha1.State) (Result, error) {
	for _, ocp := range state.ContainerClusters {
		if err := installer.CheckPortableSupport(state, ocp); err != nil {
			return Result{}, err
		}
	}
	return renderCore(fs, outputDir, state, renderParams{
		installerAssets: func(s v1alpha1.State) []InstallerAsset { return installer.InstallerToolInputAssets(outputDir, s) },
		inventory:       func(s v1alpha1.State) any { return inventory.Inventory(s, secret.PlaceholderSecretsDir) },
		vars:            func(s v1alpha1.State) any { return inventory.VarsWithSecretsDir(s, secret.PlaceholderSecretsDir) },
		installerSecrets: func(s v1alpha1.State, ocp v1alpha1.ContainerCluster) (installer.InstallerSecrets, error) {
			return installer.PortableInstallerSecrets(s, ocp), nil
		},
	})
}

type renderParams struct {
	installerAssets  func(v1alpha1.State) []InstallerAsset
	inventory        func(v1alpha1.State) any
	vars             func(v1alpha1.State) any
	installerSecrets func(v1alpha1.State, v1alpha1.ContainerCluster) (installer.InstallerSecrets, error)
}

func renderCore(fs FileSystem, baseDir string, state v1alpha1.State, params renderParams) (Result, error) {
	if err := checkResolvedNames(state); err != nil {
		return Result{}, err
	}
	result := Result{
		EffectiveStatePath: filepath.Join(baseDir, "effective-state.yaml"),
		LockPath:           filepath.Join(baseDir, "bootwright.lock.yaml"),
		InventoryPath:      filepath.Join(baseDir, "ansible", "inventory.yaml"),
		VarsPath:           filepath.Join(baseDir, "ansible", "vars.yaml"),
		ArtifactsDir:       filepath.Join(baseDir, "ansible", "artifacts"),
		InstallerAssets:    params.installerAssets(state),
		StorageAssets:      ceph.StorageAssets(baseDir, state),
	}
	dirs := []string{baseDir, filepath.Dir(result.InventoryPath), result.ArtifactsDir}
	for _, asset := range result.InstallerAssets {
		dirs = append(dirs, asset.Dir)
	}
	for _, asset := range result.StorageAssets {
		dirs = append(dirs, asset.Directories()...)
	}
	for _, dir := range dirs {
		if err := ensureLocalDir(fs, dir); err != nil {
			return result, err
		}
	}
	writes := []struct {
		path  string
		value any
	}{
		{path: result.EffectiveStatePath, value: EffectiveState(state)},
		{path: result.LockPath, value: Lock(state)},
		{path: result.InventoryPath, value: params.inventory(state)},
		{path: result.VarsPath, value: params.vars(state)},
	}
	for _, w := range writes {
		if err := writeYAML(fs, w.path, w.value); err != nil {
			return result, err
		}
	}
	for _, ocp := range state.ContainerClusters {
		asset := installerAssetFor(result.InstallerAssets, ocp.Metadata.Name)
		secrets, err := params.installerSecrets(state, ocp)
		if err != nil {
			return result, err
		}
		installConfig, err := installer.InstallerConfigWithSecrets(state, ocp, secrets)
		if err != nil {
			return result, err
		}
		if err := writeYAML(fs, asset.InstallConfigPath, installConfig); err != nil {
			return result, err
		}
		agentConfig, err := AgentConfig(state, ocp)
		if err != nil {
			return result, err
		}
		if err := writeYAML(fs, asset.AgentConfigPath, agentConfig); err != nil {
			return result, err
		}
		if err := writeInstallerManifests(fs, asset.InstallManifestsDir, InstallerManifests(ocp, secrets)); err != nil {
			return result, err
		}
	}
	if err := writeStorageAssets(fs, result.StorageAssets, state); err != nil {
		return result, err
	}
	return result, nil
}

func installerAssetFor(assets []InstallerAsset, name string) InstallerAsset {
	for _, a := range assets {
		if a.ClusterName == name {
			return a
		}
	}
	return InstallerAsset{}
}

func writeInstallerManifests(fs FileSystem, dir string, manifests []InstallerManifest) error {
	if err := fs.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	if len(manifests) == 0 {
		return nil
	}
	if err := ensureLocalDir(fs, dir); err != nil {
		return err
	}
	for _, manifest := range manifests {
		if err := writeYAML(fs, filepath.Join(dir, manifest.FileName), manifest.Object); err != nil {
			return err
		}
	}
	return nil
}
