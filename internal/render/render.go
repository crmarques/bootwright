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

// Result is the set of paths and assets the renderer wrote.
type Result struct {
	EffectiveStatePath string
	LockPath           string
	InventoryPath      string
	VarsPath           string
	ArtifactsDir       string
	InstallerAssets    []InstallerAsset
	StorageAssets      []StorageAsset
}

// All writes effective-state, lock, Ansible inventory + vars, and
// per-cluster installer placeholders. `renderedDir` is the Bootwright context
// rendered directory, `clustersDir` is the context cluster directory, and
// `secretsDir` is the local secrets dir. Uses the
// default os-backed FileSystem; tests use AllOn to inject a substitute.
// Every render entry point fails before writing anything when the state
// carries unresolved names (see checkResolvedNames); Validate is the first
// enforcement line and rejects such state with field-level diagnostics.
func All(renderedDir, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return AllOn(defaultFS, renderedDir, clustersDir, secretsDir, state)
}

func AllWithPathOptions(renderedDir, clustersDir string, paths PathOptions, state v1alpha1.State) (Result, error) {
	return allOn(defaultFS, renderedDir, clustersDir, paths, state, nil)
}

func AllWithOwnershipRecordsAndPathOptions(renderedDir, clustersDir string, paths PathOptions, state v1alpha1.State, records []ownership.ResourceRecord) (Result, error) {
	return allOn(defaultFS, renderedDir, clustersDir, paths, state, records)
}

// Effective writes only the normalized effective-state snapshot. It is used by
// `bootwright render effective` so operators can inspect defaults without
// rendering installer, Ansible, or storage tool inputs.
func Effective(renderedDir string, state v1alpha1.State) (Result, error) {
	return EffectiveOn(defaultFS, renderedDir, state)
}

// EffectiveOn is Effective parameterised on FileSystem for tests.
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

// AllOn is All parameterised on FileSystem so tests can assert mode invariants
// without touching disk. Production callers use All.
func AllOn(fs FileSystem, renderedDir, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return allOn(fs, renderedDir, clustersDir, PathOptions{SecretsDir: secretsDir}, state, nil)
}

func allOn(fs FileSystem, renderedDir, clustersDir string, paths PathOptions, state v1alpha1.State, records []ownership.ResourceRecord) (Result, error) {
	if err := checkResolvedNames(state); err != nil {
		return Result{}, err
	}
	result := Result{
		EffectiveStatePath: filepath.Join(renderedDir, "effective-state.yaml"),
		LockPath:           filepath.Join(renderedDir, "bootwright.lock.yaml"),
		InventoryPath:      filepath.Join(renderedDir, "ansible", "inventory.yaml"),
		VarsPath:           filepath.Join(renderedDir, "ansible", "vars.yaml"),
		ArtifactsDir:       filepath.Join(renderedDir, "ansible", "artifacts"),
		InstallerAssets:    InstallerAssets(clustersDir, state),
		StorageAssets:      ceph.StorageAssets(renderedDir, state),
	}
	dirs := []string{renderedDir, filepath.Dir(result.InventoryPath), result.ArtifactsDir}
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
		{path: result.InventoryPath, value: inventory.InventoryWithOwnershipRecordsAndPathOptions(state, paths, records)},
		{path: result.VarsPath, value: inventory.VarsWithPathOptionsAndOwnership(state, paths, records)},
	}
	for _, w := range writes {
		if err := writeYAML(fs, w.path, w.value); err != nil {
			return result, err
		}
	}
	for _, ocp := range state.ContainerClusters {
		asset := installerAssetFor(result.InstallerAssets, ocp.Metadata.Name)
		installConfig, err := InstallerConfig(state, ocp)
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
		if err := writeInstallerManifests(fs, asset.InstallManifestsDir, InstallerManifests(ocp, PlaceholderInstallerSecrets(state, ocp))); err != nil {
			return result, err
		}
	}
	if err := writeStorageAssets(fs, result.StorageAssets, state, storageAssetWriteOptions{}); err != nil {
		return result, err
	}
	return result, nil
}

// ResolveInstaller writes install-config / agent-config with real
// secret material inlined under each cluster's runtime work dir.
// Placeholder copies under the rendered installer dir are left untouched.
// Uses the default os-backed FileSystem; tests use ResolveInstallerOn.
func ResolveInstaller(clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return ResolveInstallerForContext("test", clustersDir, secretsDir, state)
}

func ResolveInstallerForContext(contextName, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return ResolveInstallerOnForContext(defaultFS, contextName, clustersDir, secretsDir, state)
}

// ResolveInstallerOn is ResolveInstaller parameterised on FileSystem so
// tests can assert mode invariants on the secret-inlined work-dir
// writes without touching disk. Production callers use ResolveInstaller.
func ResolveInstallerOn(fs FileSystem, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return ResolveInstallerOnForContext(fs, "test", clustersDir, secretsDir, state)
}

func ResolveInstallerOnForContext(fs FileSystem, contextName, clustersDir, secretsDir string, state v1alpha1.State) (Result, error) {
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

func ToolInputs(outputDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return ToolInputsForContext("test", outputDir, secretsDir, state)
}

func ToolInputsForContext(contextName, outputDir, secretsDir string, state v1alpha1.State) (Result, error) {
	cleanOutputDir, err := managedroot.Ensure(outputDir, localDirMode)
	if err != nil {
		return Result{}, err
	}
	return ToolInputsOnForContext(defaultFS, contextName, cleanOutputDir, secretsDir, state)
}

func ToolInputsOnForContext(fs FileSystem, contextName, outputDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return toolInputsOn(fs, outputDir, state, toolInputsParams{
		secretsDir: secretsDir,
		installerSecrets: func(s v1alpha1.State, ocp v1alpha1.ContainerCluster) (installer.InstallerSecrets, error) {
			return installer.LoadInstallerSecretsForContext(contextName, s, ocp, secretsDir)
		},
		storageOpts: storageAssetWriteOptions{ContextName: contextName, ExternalDetailsSecretsDir: secretsDir},
	})
}

// ToolInputsPortable renders the tool-input bundle to outputDir from an
// arbitrary desired state with NO context and NO secrets directory: every
// secret reference renders as a "{{ secret <name>[.<role>] }}" substitution
// token a downstream secrets manager rehydrates. It powers `bootwright render
// --input-dir`. Unlike ToolInputsForContext it writes no Bootwright ownership
// marker, so the output stays a plain, relocatable artifact set.
func ToolInputsPortable(outputDir string, state v1alpha1.State) (Result, error) {
	cleanOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve output dir %s: %w", outputDir, err)
	}
	return ToolInputsPortableOn(defaultFS, cleanOutputDir, state)
}

// ToolInputsPortableOn is ToolInputsPortable parameterised on FileSystem so
// tests can assert mode invariants without touching disk.
func ToolInputsPortableOn(fs FileSystem, outputDir string, state v1alpha1.State) (Result, error) {
	// Fail fast — before any write — on install-config secret material that has
	// no portable token form, so an unsupported cluster never yields a partial
	// or silently-incomplete bundle.
	for _, ocp := range state.ContainerClusters {
		if err := installer.CheckPortableSupport(state, ocp); err != nil {
			return Result{}, err
		}
	}
	return toolInputsOn(fs, outputDir, state, toolInputsParams{
		secretsDir: secret.PlaceholderSecretsDir,
		installerSecrets: func(s v1alpha1.State, ocp v1alpha1.ContainerCluster) (installer.InstallerSecrets, error) {
			return installer.PortableInstallerSecrets(s, ocp), nil
		},
		storageOpts: storageAssetWriteOptions{SecretPlaceholders: true},
	})
}

// toolInputsParams parameterises the shared tool-input render core over its two
// modes: context (real material inlined) and portable ({{ secret }} tokens).
type toolInputsParams struct {
	// secretsDir feeds inventory/vars path resolution: a real context secrets
	// directory, or secret.PlaceholderSecretsDir for the portable token render.
	secretsDir string
	// installerSecrets supplies the per-cluster install-config/manifest secrets.
	installerSecrets func(v1alpha1.State, v1alpha1.ContainerCluster) (installer.InstallerSecrets, error)
	// storageOpts controls external-cluster-details: a real secrets directory
	// inlines the imported JSON; an empty value emits the SecretRef placeholder.
	storageOpts storageAssetWriteOptions
}

func toolInputsOn(fs FileSystem, outputDir string, state v1alpha1.State, params toolInputsParams) (Result, error) {
	if err := checkResolvedNames(state); err != nil {
		return Result{}, err
	}
	result := Result{
		EffectiveStatePath: filepath.Join(outputDir, "effective-state.yaml"),
		LockPath:           filepath.Join(outputDir, "bootwright.lock.yaml"),
		InventoryPath:      filepath.Join(outputDir, "ansible", "inventory.yaml"),
		VarsPath:           filepath.Join(outputDir, "ansible", "vars.yaml"),
		ArtifactsDir:       filepath.Join(outputDir, "ansible", "artifacts"),
		InstallerAssets:    installer.InstallerToolInputAssets(outputDir, state),
		StorageAssets:      ceph.StorageAssets(outputDir, state),
	}
	dirs := []string{outputDir, filepath.Dir(result.InventoryPath), result.ArtifactsDir}
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
		{path: result.InventoryPath, value: inventory.Inventory(state, params.secretsDir)},
		{path: result.VarsPath, value: inventory.VarsWithSecretsDir(state, params.secretsDir)},
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
	if err := writeStorageAssets(fs, result.StorageAssets, state, params.storageOpts); err != nil {
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
