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
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"go.yaml.in/yaml/v3"
)

type resourceKey struct {
	kind string
	name string
}

func selectResourceFiles(files []string) ([]string, v1alpha1.Environment, bool, error) {
	return selectResourceFilesCollecting(files, nil)
}

func selectResourceFilesCollecting(files []string, skipped *[]error) ([]string, v1alpha1.Environment, bool, error) {
	envs, err := scanEnvironmentsCollecting(files, skipped)
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
			files, err := environmentResourceDirectoryFiles(env, i, ref, path)
			if err != nil {
				return nil, env, true, err
			}
			for _, file := range files {
				if selected[file] {
					continue
				}
				selected[file] = true
				out = append(out, file)
			}
			continue
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

func environmentResourceDirectoryFiles(env v1alpha1.Environment, index int, ref, path string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(path, func(file string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			base := filepath.Base(file)
			atRoot := filepath.Clean(file) == filepath.Clean(path)
			if !atRoot {
				if strings.HasPrefix(base, ".") {
					return filepath.SkipDir
				}
				if _, skip := nonWorkspaceDirs[base]; skip {
					return filepath.SkipDir
				}
				if _, skip := ansibleContentDirs[base]; skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if isYAMLFile(file) {
				return fmt.Errorf("file %s must not be a symlink", file)
			}
			return nil
		}
		if !isYAMLFile(file) {
			return nil
		}
		files = append(files, filepath.Clean(file))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("environment Environment/%s spec.resources[%d] %q: walk %s: %w", env.Metadata.Name, index, ref, path, err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("environment Environment/%s spec.resources[%d] %q directory contains no .yaml or .yml files", env.Metadata.Name, index, ref)
	}
	return files, nil
}

func scanEnvironments(files []string) ([]v1alpha1.Environment, error) {
	return scanEnvironmentsCollecting(files, nil)
}

func scanEnvironmentsCollecting(files []string, skipped *[]error) ([]v1alpha1.Environment, error) {
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
				if !mayContainEnvironmentKind(data) {
					break
				}
				streamErr := fmt.Errorf("decode %s document %d: %w", file, index, err)
				if skipped == nil {
					return nil, streamErr
				}
				*skipped = append(*skipped, streamErr)
				break
			}
			if isZeroNode(node) {
				continue
			}
			var typeMeta v1alpha1.TypeMeta
			if err := node.Decode(&typeMeta); err != nil {
				metaErr := fmt.Errorf("decode %s document %d metadata: %w", file, index, err)
				if skipped == nil {
					return nil, metaErr
				}
				*skipped = append(*skipped, metaErr)
				continue
			}
			if typeMeta.APIVersion != v1alpha1.APIVersion || typeMeta.Kind != v1alpha1.KindEnvironment {
				continue
			}
			var env v1alpha1.Environment
			if err := decodeKnown(node, &env); err != nil {
				envErr := fmt.Errorf("decode %s document %d: %w", file, index, err)
				if skipped == nil {
					return nil, envErr
				}
				*skipped = append(*skipped, envErr)
				continue
			}
			env.SourcePath = file
			envs = append(envs, env)
		}
	}
	return envs, nil
}

func mayContainEnvironmentKind(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "kind:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
		if i := strings.Index(value, "#"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		value = strings.Trim(value, `"'`)
		if value == v1alpha1.KindEnvironment {
			return true
		}
	}
	return false
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
		if p.Spec.Libvirt != nil {
			require(fmt.Sprintf("InfraProvider/%s spec.libvirt.machineRef", p.Metadata.Name),
				v1alpha1.KindMachine, p.Spec.Libvirt.MachineRef.Name)
		}
		for _, profile := range providerProfiles(p) {
			if profile.FailureDomainRef.Name != "" && p.Spec.Type == v1alpha1.ProvisionerVSphere {
				require(fmt.Sprintf("InfraProvider/%s spec.%s.machineProfiles[%s].failureDomainRef", p.Metadata.Name, p.Spec.Type, profile.Name),
					v1alpha1.KindInfraProvider, p.Metadata.Name)
			}
		}
	}
	for _, image := range state.MachineInstallProfiles {
		if image.Spec.Installer.Anaconda != nil {
			require(fmt.Sprintf("MachineInstallProfile/%s spec.installer.anaconda.imageRef", image.Metadata.Name),
				v1alpha1.KindMachineImage, image.Spec.Installer.Anaconda.ImageRef.Name)
		}
	}
	for _, machine := range state.Machines {
		require(fmt.Sprintf("Machine/%s spec.substrate.providerRef", machine.Metadata.Name),
			v1alpha1.KindInfraProvider, machine.Spec.Substrate.ProviderRef.Name)
		if machine.Spec.OS.InstallProfileRef.Name != "" {
			require(fmt.Sprintf("Machine/%s spec.os.installProfileRef", machine.Metadata.Name),
				v1alpha1.KindMachineInstallProfile, machine.Spec.OS.InstallProfileRef.Name)
		}
		if machine.Spec.Network.Config.NetworkConfigRef.Name != "" {
			require(fmt.Sprintf("Machine/%s spec.network.config.networkConfigRef", machine.Metadata.Name),
				v1alpha1.KindNetworkConfig, machine.Spec.Network.Config.NetworkConfigRef.Name)
		}
	}
	for _, component := range state.InfraComponents {
		if server := component.Spec.ArtifactServer; server != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.artifactServer.machineRef", component.Metadata.Name),
				v1alpha1.KindMachine, server.MachineRef.Name)
		}
		if lb := component.Spec.LoadBalancer; lb != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.loadBalancer.machineRef", component.Metadata.Name),
				v1alpha1.KindMachine, lb.MachineRef.Name)
		}
		if proxy := component.Spec.Proxy; proxy != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.proxy.machineRef", component.Metadata.Name),
				v1alpha1.KindMachine, proxy.MachineRef.Name)
		}
		if dns := component.Spec.NameResolution; dns != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.nameResolution.machineRef", component.Metadata.Name),
				v1alpha1.KindMachine, dns.MachineRef.Name)
		}
		if registry := component.Spec.Registry; registry != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.registry.machineRef", component.Metadata.Name),
				v1alpha1.KindMachine, registry.MachineRef.Name)
		}
		if ntp := component.Spec.NTP; ntp != nil {
			require(fmt.Sprintf("InfraComponent/%s spec.ntp.machineRef", component.Metadata.Name),
				v1alpha1.KindMachine, ntp.MachineRef.Name)
		}
	}
	for _, ocp := range state.ContainerClusters {
		for i, node := range ocp.Spec.Nodes {
			require(fmt.Sprintf("ContainerCluster/%s spec.nodes[%d].machineRef", ocp.Metadata.Name, i),
				v1alpha1.KindMachine, node.MachineRef.Name)
		}
	}
	for _, set := range state.ClusterAddonProfiles {
		for i, ref := range set.Spec.ProfileRefs {
			require(fmt.Sprintf("ClusterAddonProfile/%s spec.profileRefs[%d]", set.Metadata.Name, i),
				v1alpha1.KindClusterAddonProfile, ref.Name)
		}
		for i, ref := range set.Spec.AddonRefs {
			require(fmt.Sprintf("ClusterAddonProfile/%s spec.addonRefs[%d]", set.Metadata.Name, i),
				v1alpha1.KindClusterAddon, ref.Name)
		}
	}
	for _, binding := range state.ClusterAddonBindings {
		for i, ref := range binding.Spec.ProfileRefs {
			require(fmt.Sprintf("ClusterAddonBinding/%s spec.profileRefs[%d]", binding.Metadata.Name, i),
				v1alpha1.KindClusterAddonProfile, ref.Name)
		}
		for i, ref := range binding.Spec.AddonRefs {
			require(fmt.Sprintf("ClusterAddonBinding/%s spec.addonRefs[%d]", binding.Metadata.Name, i),
				v1alpha1.KindClusterAddon, ref.Name)
		}
		for i, config := range binding.Spec.AddonConfigs {
			require(fmt.Sprintf("ClusterAddonBinding/%s spec.addonConfigs[%d].addonRef", binding.Metadata.Name, i),
				v1alpha1.KindClusterAddon, config.AddonRef.Name)
		}
		require(fmt.Sprintf("ClusterAddonBinding/%s spec.clusterRef", binding.Metadata.Name),
			v1alpha1.KindContainerCluster, binding.Spec.ClusterRef.Name)
	}
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		require(fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s input[%s].value", effect.Binding.Metadata.Name, effect.Addon.AddonRef.Name, effect.Input.Name),
			v1alpha1.KindStorageExport, addoninputs.LocalObjectReferenceValue(effect.Input).Name)
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		require(fmt.Sprintf("StorageCluster/%s spec.ceph.entitlementRef", cluster.Metadata.Name),
			v1alpha1.KindEntitlement, cluster.Spec.Ceph.EntitlementRef.Name)
		for i, node := range cluster.Spec.Ceph.Topology.Nodes {
			require(fmt.Sprintf("StorageCluster/%s spec.ceph.topology.nodes[%d].machineRef", cluster.Metadata.Name, i),
				v1alpha1.KindMachine, node.MachineRef.Name)
		}
	}
	for _, profile := range state.MachineInstallProfiles {
		if profile.Spec.Installer.Anaconda == nil {
			continue
		}
		if cdn := profile.Spec.Installer.Anaconda.PackageSource.GetFromSubscription(); cdn != nil {
			require(fmt.Sprintf("MachineInstallProfile/%s spec.installer.anaconda.packageSource.fromSubscription.entitlementRef", profile.Metadata.Name),
				v1alpha1.KindEntitlement, cdn.EntitlementRef.Name)
		}
	}
	for _, policy := range state.StoragePlacementPolicies {
		require(fmt.Sprintf("StoragePlacementPolicy/%s spec.storageClusterRef", policy.Metadata.Name),
			v1alpha1.KindStorageCluster, policy.Spec.StorageClusterRef.Name)
	}
	for _, pool := range state.StoragePools {
		require(fmt.Sprintf("StoragePool/%s spec.storageClusterRef", pool.Metadata.Name),
			v1alpha1.KindStorageCluster, pool.Spec.StorageClusterRef.Name)
		require(fmt.Sprintf("StoragePool/%s spec.placementPolicyRef", pool.Metadata.Name),
			v1alpha1.KindStoragePlacementPolicy, pool.Spec.PlacementPolicyRef.Name)
	}
	for _, fs := range state.StorageFilesystems {
		require(fmt.Sprintf("StorageFilesystem/%s spec.storageClusterRef", fs.Metadata.Name),
			v1alpha1.KindStorageCluster, fs.Spec.StorageClusterRef.Name)
		require(fmt.Sprintf("StorageFilesystem/%s spec.cephfs.metadataPoolRef", fs.Metadata.Name),
			v1alpha1.KindStoragePool, fs.Spec.CephFS.MetadataPoolRef.Name)
		for i, ref := range fs.Spec.CephFS.DataPoolRefs {
			require(fmt.Sprintf("StorageFilesystem/%s spec.cephfs.dataPoolRefs[%d]", fs.Metadata.Name, i),
				v1alpha1.KindStoragePool, ref.Name)
		}
	}
	for _, gateway := range state.StorageObjectGateways {
		require(fmt.Sprintf("StorageObjectGateway/%s spec.storageClusterRef", gateway.Metadata.Name),
			v1alpha1.KindStorageCluster, gateway.Spec.StorageClusterRef.Name)
	}
	for _, export := range state.StorageExports {
		require(fmt.Sprintf("StorageExport/%s spec.storageClusterRef", export.Metadata.Name),
			v1alpha1.KindStorageCluster, export.Spec.StorageClusterRef.Name)
		if df := export.Spec.DataFoundation; df != nil {
			require(fmt.Sprintf("StorageExport/%s spec.dataFoundation.rbdPoolRef", export.Metadata.Name),
				v1alpha1.KindStoragePool, df.RBDPoolRef.Name)
			require(fmt.Sprintf("StorageExport/%s spec.dataFoundation.filesystemRef", export.Metadata.Name),
				v1alpha1.KindStorageFilesystem, df.FilesystemRef.Name)
			require(fmt.Sprintf("StorageExport/%s spec.dataFoundation.objectGatewayRef", export.Metadata.Name),
				v1alpha1.KindStorageObjectGateway, df.ObjectGatewayRef.Name)
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
		for _, entry := range env.Spec.InfraComponents.NTP {
			require(fmt.Sprintf("Environment/%s spec.infraComponents.ntp[%s].componentRef", env.Metadata.Name, entry.Name),
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
	for _, accessor := range v1alpha1.AuthoredKindAccessors() {
		for _, name := range accessor.Names(state) {
			out[resourceKey{kind: accessor.Kind, name: name}] = true
		}
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
	for _, accessor := range v1alpha1.AuthoredKindAccessors() {
		if accessor.Kind == kind {
			return true
		}
	}
	return false
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
