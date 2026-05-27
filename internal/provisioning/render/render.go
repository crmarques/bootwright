package render

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedroot"
	"github.com/crmarques/bootwright/internal/safefs"
	"go.yaml.in/yaml/v3"
)

// File modes for everything the renderer writes. Rendered directories and
// runtime work dirs are owner-only because they hold rendered install-config
// copies; the YAML files themselves are written 0600 for the same reason.
const (
	localDirMode  os.FileMode = 0o700
	localFileMode os.FileMode = 0o600
)

// FileSystem is the side-effect surface render uses to materialize
// state to disk. The default implementation calls os.* directly; tests
// can substitute a recording implementation to assert security
// invariants (every directory is followed by an explicit Chmod, every
// file is written with localFileMode) without touching real disk.
//
// Mode arguments are honoured by both the os and recording
// implementations; callers MUST pass the documented constants
// (localDirMode / localFileMode) so the security boundary stays
// audit-able from one place.
type FileSystem interface {
	MkdirAll(path string, mode os.FileMode) error
	Chmod(path string, mode os.FileMode) error
	WriteAtomic(path string, data []byte, mode os.FileMode) error
	RemoveAll(path string) error
}

// osFS is the default FileSystem; calls into os/* directly and uses
// the atomic-rename pattern for file writes so half-written content
// never appears at the final path.
type osFS struct{}

func (osFS) MkdirAll(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

func (osFS) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

func (osFS) WriteAtomic(path string, data []byte, mode os.FileMode) error {
	return safefs.AtomicWriteFile(path, data, mode)
}

func (osFS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// defaultFS is the FileSystem the package's exported entry points use
// when callers don't supply their own. Tests use AllOn /
// ResolveInstallerOn to inject a substitute.
var defaultFS FileSystem = osFS{}

// Result is the set of paths and assets the renderer wrote.
type Result struct {
	EffectiveStatePath string
	LockPath           string
	InventoryPath      string
	VarsPath           string
	ArtifactsDir       string
	InstallerAssets    []InstallerAsset
}

// All writes effective-state, lock, Ansible inventory + vars, and
// per-cluster installer placeholders. `renderedDir` is the Bootwright context
// rendered directory, `runtimeDir` is the local execution runtime directory,
// and `secretsDir` is the local secrets dir. Uses the
// default os-backed FileSystem; tests use AllOn to inject a substitute.
func All(renderedDir, runtimeDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return AllOn(defaultFS, renderedDir, runtimeDir, secretsDir, state)
}

// AllOn is All parameterised on FileSystem so tests can assert mode invariants
// without touching disk. Production callers use All.
func AllOn(fs FileSystem, renderedDir, runtimeDir, secretsDir string, state v1alpha1.State) (Result, error) {
	result := Result{
		EffectiveStatePath: filepath.Join(renderedDir, "effective-state.yaml"),
		LockPath:           filepath.Join(renderedDir, "bootwright.lock.yaml"),
		InventoryPath:      filepath.Join(renderedDir, "ansible", "inventory.yaml"),
		VarsPath:           filepath.Join(renderedDir, "ansible", "vars.yaml"),
		ArtifactsDir:       filepath.Join(renderedDir, "ansible", "artifacts"),
		InstallerAssets:    InstallerAssets(renderedDir, runtimeDir, state),
	}
	dirs := []string{renderedDir, filepath.Dir(result.InventoryPath), result.ArtifactsDir}
	for _, asset := range result.InstallerAssets {
		dirs = append(dirs, asset.Dir)
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
		{path: result.EffectiveStatePath, value: state},
		{path: result.LockPath, value: Lock(state)},
		{path: result.InventoryPath, value: Inventory(state, secretsDir)},
		{path: result.VarsPath, value: Vars(state)},
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
	return result, nil
}

// ensureLocalDir creates `dir` with localDirMode and tightens the mode
// even when the directory already existed. MkdirAll is a no-op (and
// leaves the existing mode untouched) when the path exists; without
// the explicit Chmod a directory created by a user umask of 0022
// would silently be 0755 and expose subsequent secret material. Keep
// this helper so the security boundary is named in one place.
func ensureLocalDir(fs FileSystem, dir string) error {
	if err := fs.MkdirAll(dir, localDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := fs.Chmod(dir, localDirMode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}

// ResolveInstaller writes install-config / agent-config with real
// secret material inlined under each cluster's runtime work dir.
// Placeholder copies under the rendered installer dir are left untouched.
// Uses the default os-backed FileSystem; tests use ResolveInstallerOn.
func ResolveInstaller(renderedDir, runtimeDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return ResolveInstallerOn(defaultFS, renderedDir, runtimeDir, secretsDir, state)
}

// ResolveInstallerOn is ResolveInstaller parameterised on FileSystem so
// tests can assert mode invariants on the secret-inlined work-dir
// writes without touching disk. Production callers use ResolveInstaller.
func ResolveInstallerOn(fs FileSystem, renderedDir, runtimeDir, secretsDir string, state v1alpha1.State) (Result, error) {
	result := Result{InstallerAssets: InstallerAssets(renderedDir, runtimeDir, state)}
	for _, ocp := range state.ContainerClusters {
		asset := installerAssetFor(result.InstallerAssets, ocp.Metadata.Name)
		secrets, err := LoadInstallerSecrets(state, ocp, secretsDir)
		if err != nil {
			return result, err
		}
		for _, dir := range runtimeInstallerDirs(runtimeDir, asset) {
			if err := ensureLocalDir(fs, dir); err != nil {
				return result, err
			}
		}
		installConfig, err := InstallerConfigWithSecrets(state, ocp, secrets)
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

func runtimeInstallerDirs(runtimeDir string, asset InstallerAsset) []string {
	clusterDir := filepath.Dir(asset.WorkDir)
	return []string{runtimeDir, clusterDir, asset.WorkDir}
}

func ToolInputs(outputDir, secretsDir string, state v1alpha1.State) (Result, error) {
	cleanOutputDir, err := managedroot.Ensure(outputDir, localDirMode)
	if err != nil {
		return Result{}, err
	}
	return ToolInputsOn(defaultFS, cleanOutputDir, secretsDir, state)
}

func ToolInputsOn(fs FileSystem, outputDir, secretsDir string, state v1alpha1.State) (Result, error) {
	result := Result{
		EffectiveStatePath: filepath.Join(outputDir, "effective-state.yaml"),
		LockPath:           filepath.Join(outputDir, "bootwright.lock.yaml"),
		InventoryPath:      filepath.Join(outputDir, "ansible", "inventory.yaml"),
		VarsPath:           filepath.Join(outputDir, "ansible", "vars.yaml"),
		ArtifactsDir:       filepath.Join(outputDir, "ansible", "artifacts"),
		InstallerAssets:    InstallerToolInputAssets(outputDir, state),
	}
	dirs := []string{outputDir, filepath.Dir(result.InventoryPath), result.ArtifactsDir}
	for _, asset := range result.InstallerAssets {
		dirs = append(dirs, asset.Dir)
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
		{path: result.EffectiveStatePath, value: state},
		{path: result.LockPath, value: Lock(state)},
		{path: result.InventoryPath, value: Inventory(state, secretsDir)},
		{path: result.VarsPath, value: Vars(state)},
	}
	for _, w := range writes {
		if err := writeYAML(fs, w.path, w.value); err != nil {
			return result, err
		}
	}
	for _, ocp := range state.ContainerClusters {
		asset := installerAssetFor(result.InstallerAssets, ocp.Metadata.Name)
		secrets, err := LoadInstallerSecrets(state, ocp, secretsDir)
		if err != nil {
			return result, err
		}
		installConfig, err := InstallerConfigWithSecrets(state, ocp, secrets)
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
