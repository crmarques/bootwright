package render

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/safefs"
	"go.yaml.in/yaml/v3"
)

// File modes for everything the renderer writes. State directories
// and per-cluster work dirs are owner-only because they hold rendered
// install-config copies (which inline pull-secret material after
// ResolveInstaller); the YAML files themselves are written 0600 for
// the same reason.
const (
	stateDirMode  os.FileMode = 0o700
	stateFileMode os.FileMode = 0o600
)

// FileSystem is the side-effect surface render uses to materialize
// state to disk. The default implementation calls os.* directly; tests
// can substitute a recording implementation to assert security
// invariants (every directory is followed by an explicit Chmod, every
// file is written with stateFileMode) without touching real disk.
//
// Mode arguments are honoured by both the os and recording
// implementations; callers MUST pass the documented constants
// (stateDirMode / stateFileMode) so the security boundary stays
// audit-able from one place.
type FileSystem interface {
	MkdirAll(path string, mode os.FileMode) error
	Chmod(path string, mode os.FileMode) error
	WriteAtomic(path string, data []byte, mode os.FileMode) error
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

// All writes effective-state, lock, Ansible inventory + vars, and per-
// cluster install-config + agent-config (placeholders for secrets).
// `stateDir` is the bootwright state directory; `secretsDir` is the
// local secrets dir. Uses the default os-backed FileSystem; tests use
// AllOn to inject a substitute.
func All(stateDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return AllOn(defaultFS, stateDir, secretsDir, state)
}

// AllOn is All parameterised on FileSystem so tests can assert mode
// invariants without touching disk. Production callers use All.
func AllOn(fs FileSystem, stateDir, secretsDir string, state v1alpha1.State) (Result, error) {
	result := Result{
		EffectiveStatePath: filepath.Join(stateDir, "effective-state.yaml"),
		LockPath:           filepath.Join(stateDir, "bootwright.lock.yaml"),
		InventoryPath:      filepath.Join(stateDir, "ansible", "inventory.yaml"),
		VarsPath:           filepath.Join(stateDir, "ansible", "vars.yaml"),
		ArtifactsDir:       filepath.Join(stateDir, "ansible", "artifacts"),
		InstallerAssets:    InstallerAssets(stateDir, state),
	}
	dirs := []string{stateDir, filepath.Dir(result.InventoryPath), result.ArtifactsDir}
	for _, asset := range result.InstallerAssets {
		dirs = append(dirs, asset.Dir)
	}
	for _, dir := range dirs {
		if err := ensureStateDir(fs, dir); err != nil {
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
	}
	return result, nil
}

// ensureStateDir creates `dir` with stateDirMode and tightens the mode
// even when the directory already existed. MkdirAll is a no-op (and
// leaves the existing mode untouched) when the path exists; without
// the explicit Chmod a directory created by a user umask of 0022
// would silently be 0755 and expose subsequent secret material. Keep
// this helper so the security boundary is named in one place.
func ensureStateDir(fs FileSystem, dir string) error {
	if err := fs.MkdirAll(dir, stateDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := fs.Chmod(dir, stateDirMode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}

// ResolveInstaller writes install-config / agent-config with real
// secret material inlined under each cluster's runtime work dir.
// Placeholder copies under the state installer dir are left untouched.
// Uses the default os-backed FileSystem; tests use ResolveInstallerOn.
func ResolveInstaller(stateDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return ResolveInstallerOn(defaultFS, stateDir, secretsDir, state)
}

// ResolveInstallerOn is ResolveInstaller parameterised on FileSystem so
// tests can assert mode invariants on the secret-inlined work-dir
// writes without touching disk. Production callers use ResolveInstaller.
func ResolveInstallerOn(fs FileSystem, stateDir, secretsDir string, state v1alpha1.State) (Result, error) {
	result := Result{InstallerAssets: InstallerAssets(stateDir, state)}
	for _, ocp := range state.ContainerClusters {
		asset := installerAssetFor(result.InstallerAssets, ocp.Metadata.Name)
		secrets, err := LoadInstallerSecrets(state, ocp, secretsDir)
		if err != nil {
			return result, err
		}
		if err := ensureStateDir(fs, asset.WorkDir); err != nil {
			return result, err
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
	}
	return result, nil
}

func ToolInputs(outputDir, secretsDir string, state v1alpha1.State) (Result, error) {
	return ToolInputsOn(defaultFS, outputDir, secretsDir, state)
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
		if err := ensureStateDir(fs, dir); err != nil {
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
	if err := fs.WriteAtomic(path, data, stateFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
