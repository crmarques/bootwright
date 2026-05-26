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

type resourceKey struct {
	kind string
	name string
}

func selectResourceFiles(files []string) ([]string, v1alpha1.Environment, bool, error) {
	envs, err := scanEnvironments(files)
	if err != nil {
		return nil, v1alpha1.Environment{}, false, err
	}
	if len(envs) != 1 || envs[0].Spec.Resources == nil {
		return files, v1alpha1.Environment{}, false, nil
	}
	env := envs[0]
	selected := map[string]bool{env.SourcePath: true}
	out := []string{env.SourcePath}
	for i, ref := range env.Spec.Resources {
		path, err := resolveEnvironmentResourcePath(env, i, ref)
		if err != nil {
			return nil, env, true, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, env, true, fmt.Errorf("environment Environment/%s spec.resources[%d] %q: stat %s: %w", env.Metadata.Name, i, ref, path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, env, true, fmt.Errorf("environment Environment/%s spec.resources[%d] %q must not be a symlink", env.Metadata.Name, i, ref)
		}
		if info.IsDir() {
			return nil, env, true, fmt.Errorf("environment Environment/%s spec.resources[%d] %q must name a YAML file, got directory %s", env.Metadata.Name, i, ref, path)
		}
		if !isYAMLFile(path) {
			return nil, env, true, fmt.Errorf("environment Environment/%s spec.resources[%d] %q is not a .yaml or .yml file", env.Metadata.Name, i, ref)
		}
		if selected[path] {
			continue
		}
		selected[path] = true
		out = append(out, path)
	}
	sort.Strings(out)
	return out, env, true, nil
}

func scanEnvironments(files []string) ([]v1alpha1.Environment, error) {
	var envs []v1alpha1.Environment
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for index := 1; ; index++ {
			var node yaml.Node
			err := decoder.Decode(&node)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("decode %s document %d: %w", file, index, err)
			}
			if isZeroNode(node) {
				continue
			}
			var typeMeta v1alpha1.TypeMeta
			if err := node.Decode(&typeMeta); err != nil {
				return nil, fmt.Errorf("decode %s document %d metadata: %w", file, index, err)
			}
			if typeMeta.APIVersion != v1alpha1.APIVersion || typeMeta.Kind != v1alpha1.KindEnvironment {
				continue
			}
			var env v1alpha1.Environment
			if err := decodeKnown(node, &env); err != nil {
				return nil, fmt.Errorf("decode %s document %d: %w", file, index, err)
			}
			env.SourcePath = file
			envs = append(envs, env)
		}
	}
	return envs, nil
}

func resolveEnvironmentResourcePath(env v1alpha1.Environment, index int, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("environment Environment/%s spec.resources[%d] must not be empty", env.Metadata.Name, index)
	}
	if strings.TrimSpace(value) != value {
		return "", fmt.Errorf("environment Environment/%s spec.resources[%d] %q must not contain leading or trailing whitespace", env.Metadata.Name, index, value)
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("environment Environment/%s spec.resources[%d] %q must be relative to the Environment file", env.Metadata.Name, index, value)
	}
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("environment Environment/%s spec.resources[%d] %q must stay within the Environment file directory", env.Metadata.Name, index, value)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(env.SourcePath), clean)), nil
}

func validateSelectedResourceReferences(state v1alpha1.State, discoveredFiles, selectedFiles []string, env v1alpha1.Environment) error {
	selected := selectedResourceKeys(state)
	inventory := scanResourceInventory(uniqueFiles(discoveredFiles, selectedFiles))
	var errs []string
	require := func(owner, kind, name string) {
		if name == "" {
			return
		}
		key := resourceKey{kind: kind, name: name}
		if selected[key] {
			return
		}
		path, ok := inventory[key]
		if !ok {
			return
		}
		errs = append(errs, fmt.Sprintf("Environment/%s spec.resources excludes %s/%s required by %s; add %q",
			env.Metadata.Name, kind, name, owner, relativeResourcePath(env.SourcePath, path)))
	}
	for _, p := range state.InfraProviders {
		for _, mp := range p.Spec.MachineProfiles {
			if mp.Libvirt != nil {
				require(fmt.Sprintf("InfraProvider/%s spec.machineProfiles[%s].libvirt.hostRef", p.Metadata.Name, mp.Name),
					v1alpha1.KindHost, mp.Libvirt.HostRef.Name)
			}
		}
	}
	for _, component := range state.InfraComponents {
		if server := component.Spec.ArtifactServer; server != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.artifactServer.hostRef", component.Metadata.Name),
				v1alpha1.KindHost, server.HostRef.Name)
		}
		if lb := component.Spec.LoadBalancer; lb != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.loadBalancer.hostRef", component.Metadata.Name),
				v1alpha1.KindHost, lb.HostRef.Name)
		}
		if proxy := component.Spec.Proxy; proxy != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.proxy.hostRef", component.Metadata.Name),
				v1alpha1.KindHost, proxy.HostRef.Name)
		}
		if dns := component.Spec.NameResolution; dns != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.nameResolution.hostRef", component.Metadata.Name),
				v1alpha1.KindHost, dns.HostRef.Name)
		}
		if registry := component.Spec.Registry; registry != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.registry.hostRef", component.Metadata.Name),
				v1alpha1.KindHost, registry.HostRef.Name)
		}
	}
	for _, ci := range state.ClusterInfras {
		for _, machine := range ci.Spec.Components.Machines {
			require(fmt.Sprintf("ClusterInfra/%s spec.components.machines[%s].from.provider", ci.Metadata.Name, machine.Name),
				v1alpha1.KindInfraProvider, machine.From.Provider)
			require(fmt.Sprintf("ClusterInfra/%s spec.components.machines[%s].networkConfig.ref", ci.Metadata.Name, machine.Name),
				v1alpha1.KindNetworkConfig, machine.NetworkConfig.Ref.Name)
		}
	}
	for _, ocp := range state.ContainerClusters {
		for i, node := range ocp.Spec.Nodes {
			require(fmt.Sprintf("ContainerCluster/%s spec.nodes[%d].machineRef.clusterInfra", ocp.Metadata.Name, i),
				v1alpha1.KindClusterInfra, node.MachineRef.ClusterInfra)
		}
	}
	for _, env := range state.Environments {
		for _, entry := range env.Spec.InfraComponents.Proxies {
			require(fmt.Sprintf("Environment/%s spec.infraComponents.proxies[%s].componentRef", env.Metadata.Name, entry.Name),
				v1alpha1.KindInfraComponent, entry.ComponentRef.Name)
		}
		for _, entry := range env.Spec.InfraComponents.NameResolution {
			require(fmt.Sprintf("Environment/%s spec.infraComponents.nameResolution[%s].componentRef", env.Metadata.Name, entry.Name),
				v1alpha1.KindInfraComponent, entry.ComponentRef.Name)
		}
		for _, entry := range env.Spec.InfraComponents.ArtifactServers {
			require(fmt.Sprintf("Environment/%s spec.infraComponents.artifactServers[%s].componentRef", env.Metadata.Name, entry.Name),
				v1alpha1.KindInfraComponent, entry.ComponentRef.Name)
		}
		for _, entry := range env.Spec.InfraComponents.Registries {
			require(fmt.Sprintf("Environment/%s spec.infraComponents.registries[%s].componentRef", env.Metadata.Name, entry.Name),
				v1alpha1.KindInfraComponent, entry.ComponentRef.Name)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	sort.Strings(errs)
	return errors.New(strings.Join(errs, "; "))
}

func selectedResourceKeys(state v1alpha1.State) map[resourceKey]bool {
	out := map[resourceKey]bool{}
	for _, env := range state.Environments {
		out[resourceKey{kind: v1alpha1.KindEnvironment, name: env.Metadata.Name}] = true
	}
	for _, h := range state.Hosts {
		out[resourceKey{kind: v1alpha1.KindHost, name: h.Metadata.Name}] = true
	}
	for _, n := range state.NetworkConfigs {
		out[resourceKey{kind: v1alpha1.KindNetworkConfig, name: n.Metadata.Name}] = true
	}
	for _, p := range state.InfraProviders {
		out[resourceKey{kind: v1alpha1.KindInfraProvider, name: p.Metadata.Name}] = true
	}
	for _, c := range state.InfraComponents {
		out[resourceKey{kind: v1alpha1.KindInfraComponent, name: c.Metadata.Name}] = true
	}
	for _, ci := range state.ClusterInfras {
		out[resourceKey{kind: v1alpha1.KindClusterInfra, name: ci.Metadata.Name}] = true
	}
	for _, ocp := range state.ContainerClusters {
		out[resourceKey{kind: v1alpha1.KindContainerCluster, name: ocp.Metadata.Name}] = true
	}
	return out
}

func scanResourceInventory(files []string) map[resourceKey]string {
	out := map[resourceKey]string{}
	for _, file := range files {
		scanResourceInventoryFile(file, out)
	}
	return out
}

func scanResourceInventoryFile(file string, out map[resourceKey]string) {
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			return
		}
		if isZeroNode(node) {
			continue
		}
		var meta struct {
			APIVersion string            `yaml:"apiVersion"`
			Kind       string            `yaml:"kind"`
			Metadata   v1alpha1.Metadata `yaml:"metadata"`
		}
		if err := node.Decode(&meta); err != nil {
			continue
		}
		if meta.APIVersion != v1alpha1.APIVersion || !knownResourceKind(meta.Kind) || meta.Metadata.Name == "" {
			continue
		}
		key := resourceKey{kind: meta.Kind, name: meta.Metadata.Name}
		if _, exists := out[key]; !exists {
			out[key] = file
		}
	}
}

func knownResourceKind(kind string) bool {
	switch kind {
	case v1alpha1.KindEnvironment,
		v1alpha1.KindHost,
		v1alpha1.KindNetworkConfig,
		v1alpha1.KindInfraProvider,
		v1alpha1.KindClusterInfra,
		v1alpha1.KindContainerCluster:
		return true
	default:
		return false
	}
}

func uniqueFiles(groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		for _, file := range group {
			if seen[file] {
				continue
			}
			seen[file] = true
			out = append(out, file)
		}
	}
	return out
}

func relativeResourcePath(envSourcePath, path string) string {
	rel, err := filepath.Rel(filepath.Dir(envSourcePath), path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.ToSlash(rel)
}
