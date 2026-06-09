package desiredstate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/graph"
	"go.yaml.in/yaml/v3"
)

// LoadNormalizeValidate is the canonical entry point used by the CLI. It
// validates the effective selected State. When an Environment declares
// spec.resources, only that allow-listed input set is effective.
func LoadNormalizeValidate(paths []string) (v1alpha1.State, error) {
	return LoadNormalizeValidateInputFiles(paths)
}

func LoadNormalizeValidateInputFiles(paths []string) (v1alpha1.State, error) {
	files, err := discoverFiles(paths)
	if err != nil {
		return v1alpha1.State{}, err
	}
	state, err := loadSelectedFiles(files)
	if err != nil {
		return v1alpha1.State{}, err
	}
	Normalize(&state)
	state = applyEnvironmentClusterSelection(state)
	if err := Validate(state); err != nil {
		return v1alpha1.State{}, err
	}
	return state, nil
}

// LoadedInputFiles returns the YAML files the canonical loader would decode
// for paths, after Environment spec.resources selection. Mutating runs use it
// to snapshot the exact input set they were launched from.
func LoadedInputFiles(paths []string) ([]string, error) {
	files, err := discoverFiles(paths)
	if err != nil {
		return nil, err
	}
	selected, _, _, err := selectResourceFiles(files)
	if err != nil {
		return nil, err
	}
	return selected, nil
}

func applyEnvironmentClusterSelection(state v1alpha1.State) v1alpha1.State {
	if len(state.Environments) != 1 {
		return state
	}
	env := state.Environments[0]
	if len(env.Spec.ContainerClusters) == 0 && len(env.Spec.StorageClusters) == 0 {
		return state
	}
	return stategraph.FilterStateToClusterRoots(state, env.Spec.ContainerClusters, env.Spec.StorageClusters)
}

// Load reads `-f` arguments (files or directories) and decodes either every
// discovered YAML file or the Environment-selected resource subset into a
// State. Unknown kinds and unknown fields are rejected at decode time so typos
// surface immediately rather than after normalize.
func Load(paths []string) (v1alpha1.State, error) {
	files, err := discoverFiles(paths)
	if err != nil {
		return v1alpha1.State{}, err
	}
	return loadSelectedFiles(files)
}

func loadSelectedFiles(files []string) (v1alpha1.State, error) {
	selectedFiles, selectingEnv, resourceSelection, err := selectResourceFiles(files)
	if err != nil {
		return v1alpha1.State{}, err
	}
	state, err := loadFiles(selectedFiles)
	if err != nil {
		return v1alpha1.State{}, err
	}
	if resourceSelection {
		if err := validateSelectedResourceReferences(state, files, selectedFiles, selectingEnv); err != nil {
			return v1alpha1.State{}, err
		}
	}
	return state, nil
}

func loadFiles(files []string) (v1alpha1.State, error) {
	var state v1alpha1.State
	for _, file := range files {
		if err := loadFile(file, &state); err != nil {
			return v1alpha1.State{}, err
		}
	}
	if len(state.Environments) == 0 &&
		len(state.Machines) == 0 &&
		len(state.MachineImages) == 0 &&
		len(state.MachineInstallProfiles) == 0 &&
		len(state.NetworkConfigs) == 0 &&
		len(state.InfraProviders) == 0 &&
		len(state.InfraComponents) == 0 &&
		len(state.ContainerClusters) == 0 &&
		len(state.StorageClusters) == 0 &&
		len(state.StoragePlacementPolicies) == 0 &&
		len(state.StoragePools) == 0 &&
		len(state.StorageFilesystems) == 0 &&
		len(state.StorageObjectGateways) == 0 &&
		len(state.StorageExports) == 0 &&
		len(state.ClusterAddons) == 0 &&
		len(state.ClusterAddonProfiles) == 0 &&
		len(state.ClusterAddonBindings) == 0 {
		return v1alpha1.State{}, errors.New("no Bootwright YAML documents found")
	}
	sortState(&state)
	return state, nil
}

// nonWorkspaceDirs is the set of directory names that never hold
// Bootwright YAML and are expensive to traverse. Hidden directories
// (anything starting with ".") are skipped separately by the WalkDir
// callback below — this set covers the non-hidden build/vendor outputs
// that commonly appear when bootwright is invoked from inside a larger
// repo.
var nonWorkspaceDirs = map[string]struct{}{
	"node_modules": {},
	"vendor":       {},
}

func discoverFiles(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("at least one -f path is required")
	}
	seen := map[string]bool{}
	var files []string
	for _, input := range paths {
		if strings.TrimSpace(input) == "" {
			return nil, errors.New("empty -f path is not allowed")
		}
		info, err := os.Lstat(input)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", input, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("input source %s must not be a symlink", input)
		}
		if !info.IsDir() {
			if !isYAMLFile(input) {
				return nil, fmt.Errorf("%s is not a .yaml or .yml file", input)
			}
			clean := filepath.Clean(input)
			if !seen[clean] {
				seen[clean] = true
				files = append(files, clean)
			}
			continue
		}
		err = filepath.WalkDir(input, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				base := filepath.Base(path)
				atRoot := filepath.Clean(path) == filepath.Clean(input)
				if !atRoot {
					if strings.HasPrefix(base, ".") {
						return filepath.SkipDir
					}
					if _, skip := nonWorkspaceDirs[base]; skip {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if isYAMLFile(path) {
					return fmt.Errorf("input file %s must not be a symlink", path)
				}
				return nil
			}
			if !isYAMLFile(path) {
				return nil
			}
			clean := filepath.Clean(path)
			if !seen[clean] {
				seen[clean] = true
				files = append(files, clean)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", input, err)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, errors.New("no .yaml or .yml files found")
	}
	return files, nil
}

func loadFile(path string, state *v1alpha1.State) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for index := 1; ; index++ {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("decode %s document %d: %w", path, index, err)
		}
		if isZeroNode(node) {
			continue
		}
		var typeMeta v1alpha1.TypeMeta
		if err := node.Decode(&typeMeta); err != nil {
			return fmt.Errorf("decode %s document %d metadata: %w", path, index, err)
		}
		if typeMeta.APIVersion == "" {
			return fmt.Errorf("decode %s document %d: apiVersion is required", path, index)
		}
		if typeMeta.APIVersion != v1alpha1.APIVersion {
			if isExtensionManifestFile(path) {
				continue
			}
			return fmt.Errorf("decode %s document %d: unsupported apiVersion %q", path, index, typeMeta.APIVersion)
		}
		if !mappingHasKey(node, "metadata") {
			return fmt.Errorf("decode %s document %d: metadata is required", path, index)
		}
		if !mappingHasKey(node, "spec") {
			return fmt.Errorf("decode %s document %d: spec is required", path, index)
		}
		switch typeMeta.Kind {
		case v1alpha1.KindEnvironment:
			var item v1alpha1.Environment
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.Environments = append(state.Environments, item)
		case v1alpha1.KindMachine:
			var item v1alpha1.Machine
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.Machines = append(state.Machines, item)
		case v1alpha1.KindMachineImage:
			var item v1alpha1.MachineImage
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.MachineImages = append(state.MachineImages, item)
		case v1alpha1.KindMachineInstallProfile:
			var item v1alpha1.MachineInstallProfile
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.MachineInstallProfiles = append(state.MachineInstallProfiles, item)
		case v1alpha1.KindNetworkConfig:
			var item v1alpha1.NetworkConfig
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.NetworkConfigs = append(state.NetworkConfigs, item)
		case v1alpha1.KindInfraProvider:
			var item v1alpha1.InfraProvider
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.InfraProviders = append(state.InfraProviders, item)
		case v1alpha1.KindInfraComponent:
			var item v1alpha1.InfraComponent
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.InfraComponents = append(state.InfraComponents, item)
		case v1alpha1.KindContainerCluster:
			var item v1alpha1.ContainerCluster
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.ContainerClusters = append(state.ContainerClusters, item)
		case v1alpha1.KindStorageCluster:
			var item v1alpha1.StorageCluster
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.StorageClusters = append(state.StorageClusters, item)
		case v1alpha1.KindStoragePlacementPolicy:
			var item v1alpha1.StoragePlacementPolicy
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.StoragePlacementPolicies = append(state.StoragePlacementPolicies, item)
		case v1alpha1.KindStoragePool:
			var item v1alpha1.StoragePool
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.StoragePools = append(state.StoragePools, item)
		case v1alpha1.KindStorageFilesystem:
			var item v1alpha1.StorageFilesystem
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.StorageFilesystems = append(state.StorageFilesystems, item)
		case v1alpha1.KindStorageObjectGateway:
			var item v1alpha1.StorageObjectGateway
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.StorageObjectGateways = append(state.StorageObjectGateways, item)
		case v1alpha1.KindStorageExport:
			var item v1alpha1.StorageExport
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.StorageExports = append(state.StorageExports, item)
		case v1alpha1.KindClusterAddon:
			var item v1alpha1.ClusterAddon
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.ClusterAddons = append(state.ClusterAddons, item)
		case v1alpha1.KindClusterAddonProfile:
			var item v1alpha1.ClusterAddonProfile
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.ClusterAddonProfiles = append(state.ClusterAddonProfiles, item)
		case v1alpha1.KindClusterAddonBinding:
			var item v1alpha1.ClusterAddonBinding
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.ClusterAddonBindings = append(state.ClusterAddonBindings, item)
		case "":
			return fmt.Errorf("decode %s document %d: kind is required", path, index)
		default:
			return fmt.Errorf("decode %s document %d: unsupported kind %q", path, index, typeMeta.Kind)
		}
	}
	return nil
}

func isExtensionManifestFile(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(path)), "/") {
		if part == "manifests" {
			return true
		}
	}
	return false
}

func mappingHasKey(node yaml.Node, key string) bool {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = *node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

func decodeKnown(node yaml.Node, value any) error {
	data, err := yaml.Marshal(&node)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(value)
}

func sortState(state *v1alpha1.State) {
	sortByName := func(less func(i, j int) bool) func(i, j int) bool { return less }

	sort.SliceStable(state.Environments, sortByName(func(i, j int) bool {
		if state.Environments[i].Metadata.Name == state.Environments[j].Metadata.Name {
			return state.Environments[i].SourcePath < state.Environments[j].SourcePath
		}
		return state.Environments[i].Metadata.Name < state.Environments[j].Metadata.Name
	}))
	sort.SliceStable(state.Machines, sortByName(func(i, j int) bool {
		if state.Machines[i].Metadata.Name == state.Machines[j].Metadata.Name {
			return state.Machines[i].SourcePath < state.Machines[j].SourcePath
		}
		return state.Machines[i].Metadata.Name < state.Machines[j].Metadata.Name
	}))
	sort.SliceStable(state.MachineImages, sortByName(func(i, j int) bool {
		if state.MachineImages[i].Metadata.Name == state.MachineImages[j].Metadata.Name {
			return state.MachineImages[i].SourcePath < state.MachineImages[j].SourcePath
		}
		return state.MachineImages[i].Metadata.Name < state.MachineImages[j].Metadata.Name
	}))
	sort.SliceStable(state.MachineInstallProfiles, sortByName(func(i, j int) bool {
		if state.MachineInstallProfiles[i].Metadata.Name == state.MachineInstallProfiles[j].Metadata.Name {
			return state.MachineInstallProfiles[i].SourcePath < state.MachineInstallProfiles[j].SourcePath
		}
		return state.MachineInstallProfiles[i].Metadata.Name < state.MachineInstallProfiles[j].Metadata.Name
	}))
	sort.SliceStable(state.NetworkConfigs, sortByName(func(i, j int) bool {
		if state.NetworkConfigs[i].Metadata.Name == state.NetworkConfigs[j].Metadata.Name {
			return state.NetworkConfigs[i].SourcePath < state.NetworkConfigs[j].SourcePath
		}
		return state.NetworkConfigs[i].Metadata.Name < state.NetworkConfigs[j].Metadata.Name
	}))
	sort.SliceStable(state.InfraProviders, sortByName(func(i, j int) bool {
		if state.InfraProviders[i].Metadata.Name == state.InfraProviders[j].Metadata.Name {
			return state.InfraProviders[i].SourcePath < state.InfraProviders[j].SourcePath
		}
		return state.InfraProviders[i].Metadata.Name < state.InfraProviders[j].Metadata.Name
	}))
	sort.SliceStable(state.InfraComponents, sortByName(func(i, j int) bool {
		if state.InfraComponents[i].Metadata.Name == state.InfraComponents[j].Metadata.Name {
			return state.InfraComponents[i].SourcePath < state.InfraComponents[j].SourcePath
		}
		return state.InfraComponents[i].Metadata.Name < state.InfraComponents[j].Metadata.Name
	}))
	sort.SliceStable(state.ContainerClusters, sortByName(func(i, j int) bool {
		if state.ContainerClusters[i].Metadata.Name == state.ContainerClusters[j].Metadata.Name {
			return state.ContainerClusters[i].SourcePath < state.ContainerClusters[j].SourcePath
		}
		return state.ContainerClusters[i].Metadata.Name < state.ContainerClusters[j].Metadata.Name
	}))
	sort.SliceStable(state.StorageClusters, sortByName(func(i, j int) bool {
		if state.StorageClusters[i].Metadata.Name == state.StorageClusters[j].Metadata.Name {
			return state.StorageClusters[i].SourcePath < state.StorageClusters[j].SourcePath
		}
		return state.StorageClusters[i].Metadata.Name < state.StorageClusters[j].Metadata.Name
	}))
	sort.SliceStable(state.StoragePlacementPolicies, sortByName(func(i, j int) bool {
		if state.StoragePlacementPolicies[i].Metadata.Name == state.StoragePlacementPolicies[j].Metadata.Name {
			return state.StoragePlacementPolicies[i].SourcePath < state.StoragePlacementPolicies[j].SourcePath
		}
		return state.StoragePlacementPolicies[i].Metadata.Name < state.StoragePlacementPolicies[j].Metadata.Name
	}))
	sort.SliceStable(state.StoragePools, sortByName(func(i, j int) bool {
		if state.StoragePools[i].Metadata.Name == state.StoragePools[j].Metadata.Name {
			return state.StoragePools[i].SourcePath < state.StoragePools[j].SourcePath
		}
		return state.StoragePools[i].Metadata.Name < state.StoragePools[j].Metadata.Name
	}))
	sort.SliceStable(state.StorageFilesystems, sortByName(func(i, j int) bool {
		if state.StorageFilesystems[i].Metadata.Name == state.StorageFilesystems[j].Metadata.Name {
			return state.StorageFilesystems[i].SourcePath < state.StorageFilesystems[j].SourcePath
		}
		return state.StorageFilesystems[i].Metadata.Name < state.StorageFilesystems[j].Metadata.Name
	}))
	sort.SliceStable(state.StorageObjectGateways, sortByName(func(i, j int) bool {
		if state.StorageObjectGateways[i].Metadata.Name == state.StorageObjectGateways[j].Metadata.Name {
			return state.StorageObjectGateways[i].SourcePath < state.StorageObjectGateways[j].SourcePath
		}
		return state.StorageObjectGateways[i].Metadata.Name < state.StorageObjectGateways[j].Metadata.Name
	}))
	sort.SliceStable(state.StorageExports, sortByName(func(i, j int) bool {
		if state.StorageExports[i].Metadata.Name == state.StorageExports[j].Metadata.Name {
			return state.StorageExports[i].SourcePath < state.StorageExports[j].SourcePath
		}
		return state.StorageExports[i].Metadata.Name < state.StorageExports[j].Metadata.Name
	}))
	sort.SliceStable(state.ClusterAddons, sortByName(func(i, j int) bool {
		if state.ClusterAddons[i].Metadata.Name == state.ClusterAddons[j].Metadata.Name {
			return state.ClusterAddons[i].SourcePath < state.ClusterAddons[j].SourcePath
		}
		return state.ClusterAddons[i].Metadata.Name < state.ClusterAddons[j].Metadata.Name
	}))
	sort.SliceStable(state.ClusterAddonProfiles, sortByName(func(i, j int) bool {
		if state.ClusterAddonProfiles[i].Metadata.Name == state.ClusterAddonProfiles[j].Metadata.Name {
			return state.ClusterAddonProfiles[i].SourcePath < state.ClusterAddonProfiles[j].SourcePath
		}
		return state.ClusterAddonProfiles[i].Metadata.Name < state.ClusterAddonProfiles[j].Metadata.Name
	}))
	sort.SliceStable(state.ClusterAddonBindings, sortByName(func(i, j int) bool {
		if state.ClusterAddonBindings[i].Metadata.Name == state.ClusterAddonBindings[j].Metadata.Name {
			return state.ClusterAddonBindings[i].SourcePath < state.ClusterAddonBindings[j].SourcePath
		}
		return state.ClusterAddonBindings[i].Metadata.Name < state.ClusterAddonBindings[j].Metadata.Name
	}))
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func isZeroNode(node yaml.Node) bool {
	if node.Kind == 0 {
		return true
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 0 {
		return true
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		child := node.Content[0]
		return child.Kind == yaml.ScalarNode && child.Tag == "!!null"
	}
	return false
}
