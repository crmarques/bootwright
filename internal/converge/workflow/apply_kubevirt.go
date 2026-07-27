package workflow

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
)

func kubeVirtHostClusterApplyCapabilities(state v1alpha1.State) (map[string][]CapabilityRef, error) {
	selected := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		selected[cluster.Metadata.Name] = true
	}
	providers := providerIndex(state)
	machines := machineIndex(state)
	provides, err := kubeVirtProviderExtensionCapabilities(state)
	if err != nil {
		return nil, err
	}
	resources, storageProviders, err := addonDeclaredResourceIndex(state)
	if err != nil {
		return nil, err
	}
	out := map[string][]CapabilityRef{}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				continue
			}
			provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
			if !ok || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || provider.Spec.KubeVirt == nil || provider.Spec.KubeVirt.HostClusterRef == nil || provider.Spec.KubeVirt.HostClusterRef.Name == "" {
				continue
			}
			parent := provider.Spec.KubeVirt.HostClusterRef.Name
			if !selected[parent] {
				continue
			}
			out[cluster.Metadata.Name] = appendUniqueCapability(out[cluster.Metadata.Name], clusterInstalledCapability(parent))
			caps := provides[parent]
			if len(caps) == 0 {
				return nil, fmt.Errorf("ContainerCluster/%s uses KubeVirt hostClusterRef %q but no selected ClusterAddon providing %q is bound to that host cluster",
					cluster.Metadata.Name, parent, v1alpha1.ClusterAddonProvidesKubeVirt)
			}
			for _, cap := range caps {
				out[cluster.Metadata.Name] = appendUniqueCapability(out[cluster.Metadata.Name], cap)
			}
			for _, addon := range kubeVirtProviderAddonDependencies(provider, resources[parent], storageProviders[parent]) {
				out[cluster.Metadata.Name] = appendUniqueCapability(out[cluster.Metadata.Name], addonAppliedCapability(parent, addon))
			}
		}
	}
	return out, nil
}

func kubeVirtHostClusterApplyCapabilitiesForMachines(state v1alpha1.State, machineNames []string) ([]CapabilityRef, error) {
	selected := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		selected[cluster.Metadata.Name] = true
	}
	providers := providerIndex(state)
	machines := machineIndex(state)
	provides, err := kubeVirtProviderExtensionCapabilities(state)
	if err != nil {
		return nil, err
	}
	resources, storageProviders, err := addonDeclaredResourceIndex(state)
	if err != nil {
		return nil, err
	}
	var out []CapabilityRef
	for _, machineName := range machineNames {
		machine, ok := machines[machineName]
		if !ok {
			continue
		}
		provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
		if !ok || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || provider.Spec.KubeVirt == nil || provider.Spec.KubeVirt.HostClusterRef == nil || provider.Spec.KubeVirt.HostClusterRef.Name == "" {
			continue
		}
		parent := provider.Spec.KubeVirt.HostClusterRef.Name
		if !selected[parent] {
			continue
		}
		out = appendUniqueCapability(out, clusterInstalledCapability(parent))
		caps := provides[parent]
		if len(caps) == 0 {
			return nil, fmt.Errorf("machine Machine/%s uses KubeVirt hostClusterRef %q but no selected ClusterAddon providing %q is bound to that host cluster",
				machineName, parent, v1alpha1.ClusterAddonProvidesKubeVirt)
		}
		for _, cap := range caps {
			out = appendUniqueCapability(out, cap)
		}
		for _, addon := range kubeVirtProviderAddonDependencies(provider, resources[parent], storageProviders[parent]) {
			out = appendUniqueCapability(out, addonAppliedCapability(parent, addon))
		}
	}
	return out, nil
}

type addonDeclaredResource struct {
	group     string
	kind      string
	name      string
	namespace string
}

type addonResourceOwner struct {
	addon    string
	resource addonDeclaredResource
}

const addonStorageClassKind = "StorageClass"

func addonDeclaredResourceIndex(state v1alpha1.State) (map[string][]addonResourceOwner, map[string][]string, error) {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return nil, nil, err
	}
	resources := map[string][]addonResourceOwner{}
	storageProviders := map[string][]string{}
	for _, binding := range plans {
		for _, extension := range binding.Addons {
			for _, resource := range clusterAddonDeclaredResources(extension.Extension) {
				if resource.kind == "" || resource.name == "" {
					continue
				}
				resources[binding.Cluster] = append(resources[binding.Cluster], addonResourceOwner{addon: extension.Name, resource: resource})
			}
			if extensionProvides(extension.Extension, v1alpha1.ClusterAddonProvidesDataFoundation) {
				storageProviders[binding.Cluster] = appendUniqueString(storageProviders[binding.Cluster], extension.Name)
			}
		}
	}
	return resources, storageProviders, nil
}

func clusterAddonDeclaredResources(extension v1alpha1.ClusterAddon) []addonDeclaredResource {
	var out []addonDeclaredResource
	for _, check := range extension.Spec.Readiness.Checks {
		if exists := check.ResourceExists; exists != nil {
			out = append(out, addonDeclaredResource{
				group:     addonResourceAPIGroup(exists.APIVersion),
				kind:      exists.Kind,
				name:      exists.Name,
				namespace: exists.Namespace,
			})
		}
		if condition := check.Condition; condition != nil {
			out = append(out, addonDeclaredResource{
				group:     addonResourceAPIGroup(condition.APIVersion),
				kind:      condition.Kind,
				name:      condition.Name,
				namespace: condition.Namespace,
			})
		}
	}
	if extension.Spec.OLM != nil {
		for _, resource := range extension.Spec.OLM.CustomResources {
			metadata, _ := resource["metadata"].(map[string]any)
			out = append(out, addonDeclaredResource{
				group:     addonResourceAPIGroup(addonResourceField(resource, "apiVersion")),
				kind:      addonResourceField(resource, "kind"),
				name:      addonResourceField(metadata, "name"),
				namespace: addonResourceField(metadata, "namespace"),
			})
		}
	}
	return out
}

func addonResourceAPIGroup(apiVersion string) string {
	slash := strings.Index(apiVersion, "/")
	if slash < 0 {
		return ""
	}
	return apiVersion[:slash]
}

func addonResourceField(in map[string]any, key string) string {
	if in == nil {
		return ""
	}
	value, ok := in[key].(string)
	if !ok {
		return ""
	}
	return value
}

func kubeVirtProviderAddonDependencies(provider v1alpha1.InfraProvider, owners []addonResourceOwner, storageProviders []string) []string {
	var out []string
	if profile := provider.Spec.KubeVirt; profile != nil && profile.StorageClassRef != nil && profile.StorageClassRef.Name != "" {
		for _, addon := range addonsProvidingStorageClass(owners, storageProviders, profile.StorageClassRef.Name) {
			out = appendUniqueString(out, addon)
		}
	}
	for _, attachment := range provider.Spec.NetworkAttachments {
		if attachment.KubeVirt == nil || attachment.KubeVirt.NetworkRef.Name == "" {
			continue
		}
		for _, addon := range addonsProvidingNetworkAttachment(owners, attachment.KubeVirt.NetworkRef) {
			out = appendUniqueString(out, addon)
		}
	}
	return out
}

func addonsProvidingStorageClass(owners []addonResourceOwner, storageProviders []string, name string) []string {
	var out []string
	for _, owner := range owners {
		if owner.resource.kind == addonStorageClassKind && owner.resource.name == name {
			out = appendUniqueString(out, owner.addon)
		}
	}
	if len(out) > 0 {
		return out
	}
	return storageProviders
}

func addonsProvidingNetworkAttachment(owners []addonResourceOwner, ref v1alpha1.KubeVirtNetworkRef) []string {
	var out []string
	for _, owner := range owners {
		resource := owner.resource
		if resource.name != ref.Name || resource.kind != ref.EffectiveKind() || resource.group != ref.EffectiveAPIGroup() {
			continue
		}
		if ref.Namespace != "" && resource.namespace != "" && resource.namespace != ref.Namespace {
			continue
		}
		out = appendUniqueString(out, owner.addon)
	}
	return out
}

func kubeVirtHostClusterReadiness(state v1alpha1.State) (map[string][]CapabilityRef, map[string][]string, error) {
	selected := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		selected[cluster.Metadata.Name] = true
	}
	providers := providerIndex(state)
	machines := machineIndex(state)
	provides, err := kubeVirtProviderExtensionCapabilities(state)
	if err != nil {
		return nil, nil, err
	}
	perHost := map[string][]CapabilityRef{}
	hostsByCluster := map[string][]string{}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				continue
			}
			provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
			if !ok || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || provider.Spec.KubeVirt == nil || provider.Spec.KubeVirt.HostClusterRef == nil || provider.Spec.KubeVirt.HostClusterRef.Name == "" {
				continue
			}
			parent := provider.Spec.KubeVirt.HostClusterRef.Name
			hostsByCluster[cluster.Metadata.Name] = appendUniqueString(hostsByCluster[cluster.Metadata.Name], parent)
			if _, done := perHost[parent]; done {
				continue
			}
			if !selected[parent] {
				perHost[parent] = nil
				continue
			}
			caps := []CapabilityRef{clusterInstalledCapability(parent)}
			addonCaps := provides[parent]
			if len(addonCaps) == 0 {
				return nil, nil, fmt.Errorf("ContainerCluster uses KubeVirt hostClusterRef %q but no selected ClusterAddon providing %q is bound to that host cluster",
					parent, v1alpha1.ClusterAddonProvidesKubeVirt)
			}
			for _, cap := range addonCaps {
				caps = appendUniqueCapability(caps, cap)
			}
			perHost[parent] = caps
		}
	}
	return perHost, hostsByCluster, nil
}

func kubeVirtProviderExtensionCapabilities(state v1alpha1.State) (map[string][]CapabilityRef, error) {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return nil, err
	}
	out := map[string][]CapabilityRef{}
	for _, binding := range plans {
		for _, extension := range binding.Addons {
			if !extensionProvides(extension.Extension, v1alpha1.ClusterAddonProvidesKubeVirt) {
				continue
			}
			out[binding.Cluster] = appendUniqueCapability(out[binding.Cluster], addonProvidesCapability(binding.Cluster, v1alpha1.ClusterAddonProvidesKubeVirt))
		}
	}
	return out, nil
}

func extensionProvides(extension v1alpha1.ClusterAddon, capability string) bool {
	for _, item := range extension.Spec.Provides {
		if item == capability {
			return true
		}
	}
	return false
}

func kubeVirtResourceKey(profile *v1alpha1.InfraProviderKubeVirt, clusterName, machineName string) string {
	owner := "external"
	if profile.HostClusterRef != nil && profile.HostClusterRef.Name != "" {
		owner = profile.HostClusterRef.Name
	} else if profile.KubeconfigRef != nil && profile.KubeconfigRef.Name != "" {
		owner = "kubeconfig:" + profile.KubeconfigRef.Name
	}
	return "kubevirt:" + owner + ":" + profile.Namespace + ":vm:" + clusterName + "-" + machineName
}

func providerIndex(state v1alpha1.State) map[string]v1alpha1.InfraProvider {
	out := make(map[string]v1alpha1.InfraProvider, len(state.InfraProviders))
	for _, provider := range state.InfraProviders {
		out[provider.Metadata.Name] = provider
	}
	return out
}

func machineIndex(state v1alpha1.State) map[string]v1alpha1.Machine {
	out := make(map[string]v1alpha1.Machine, len(state.Machines))
	for _, machine := range state.Machines {
		out[machine.Metadata.Name] = machine
	}
	return out
}

func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func appendUniqueCapability(items []CapabilityRef, value CapabilityRef) []CapabilityRef {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func applyNodeBootResourceKeys(state v1alpha1.State, clusterName string, machineNames []string) []string {
	providers := providerIndex(state)
	machines := machineIndex(state)
	if _, ok := containerClusterByName(state, clusterName); !ok {
		return nil
	}
	var out []string
	for _, machineName := range machineNames {
		machine, ok := machines[machineName]
		if !ok {
			continue
		}
		provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
		if ok && provider.Spec.Type == v1alpha1.ProvisionerKubeVirt && provider.Spec.KubeVirt != nil {
			out = appendUniqueString(out, kubeVirtResourceKey(provider.Spec.KubeVirt, clusterName, machineName))
			continue
		}
		if ok && provider.Spec.Type == v1alpha1.ProvisionerVSphere && provider.Spec.VSphere != nil {
			out = appendUniqueString(out, vsphereResourceKey(provider, machine))
			continue
		}
		out = appendUniqueString(out, applyNodeRedfishResource(state, clusterName, machineName))
	}
	return out
}

func containerClusterByName(state v1alpha1.State, name string) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.ContainerCluster{}, false
}
