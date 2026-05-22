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
	"go.yaml.in/yaml/v3"
)

// LoadNormalizeValidate is the canonical entry point used by the CLI.
// It loads YAML from `paths`, applies normalize defaults, runs the full
// validator, and returns the resulting State.
func LoadNormalizeValidate(paths []string) (v1alpha1.State, error) {
	state, err := Load(paths)
	if err != nil {
		return v1alpha1.State{}, err
	}
	Normalize(&state)
	if err := Validate(state); err != nil {
		return v1alpha1.State{}, err
	}
	return state, nil
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
	loadFiles, selectingEnv, resourceSelection, err := selectResourceFiles(files)
	if err != nil {
		return v1alpha1.State{}, err
	}
	var state v1alpha1.State
	for _, file := range loadFiles {
		if err := loadFile(file, &state); err != nil {
			return v1alpha1.State{}, err
		}
	}
	if len(state.Environments) == 0 &&
		len(state.Hosts) == 0 &&
		len(state.NetworkConfigs) == 0 &&
		len(state.InfraProviders) == 0 &&
		len(state.ClusterInfras) == 0 &&
		len(state.ContainerClusters) == 0 {
		return v1alpha1.State{}, errors.New("no Bootwright YAML documents found")
	}
	sortState(&state)
	if resourceSelection {
		if err := validateSelectedResourceReferences(state, files, loadFiles, selectingEnv); err != nil {
			return v1alpha1.State{}, err
		}
	}
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
		info, err := os.Stat(input)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", input, err)
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
		case v1alpha1.KindHost:
			var item v1alpha1.Host
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.Hosts = append(state.Hosts, item)
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
		case v1alpha1.KindClusterInfra:
			var item v1alpha1.ClusterInfra
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.ClusterInfras = append(state.ClusterInfras, item)
		case v1alpha1.KindContainerCluster:
			var item v1alpha1.ContainerCluster
			if err := decodeKnown(node, &item); err != nil {
				return fmt.Errorf("decode %s document %d: %w", path, index, err)
			}
			item.SourcePath = path
			state.ContainerClusters = append(state.ContainerClusters, item)
		case "":
			return fmt.Errorf("decode %s document %d: kind is required", path, index)
		default:
			return fmt.Errorf("decode %s document %d: unsupported kind %q", path, index, typeMeta.Kind)
		}
	}
	return nil
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
	sort.SliceStable(state.Hosts, sortByName(func(i, j int) bool {
		if state.Hosts[i].Metadata.Name == state.Hosts[j].Metadata.Name {
			return state.Hosts[i].SourcePath < state.Hosts[j].SourcePath
		}
		return state.Hosts[i].Metadata.Name < state.Hosts[j].Metadata.Name
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
	sort.SliceStable(state.ClusterInfras, sortByName(func(i, j int) bool {
		if state.ClusterInfras[i].Metadata.Name == state.ClusterInfras[j].Metadata.Name {
			return state.ClusterInfras[i].SourcePath < state.ClusterInfras[j].SourcePath
		}
		return state.ClusterInfras[i].Metadata.Name < state.ClusterInfras[j].Metadata.Name
	}))
	sort.SliceStable(state.ContainerClusters, sortByName(func(i, j int) bool {
		if state.ContainerClusters[i].Metadata.Name == state.ContainerClusters[j].Metadata.Name {
			return state.ContainerClusters[i].SourcePath < state.ContainerClusters[j].SourcePath
		}
		return state.ContainerClusters[i].Metadata.Name < state.ContainerClusters[j].Metadata.Name
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
