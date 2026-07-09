package cli

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

type clusterDisplay struct {
	descriptor string
	storage    bool
	host       string
}

func substrateLabel(provider string) string {
	switch provider {
	case v1alpha1.ProvisionerBareMetal:
		return "bare metal"
	case v1alpha1.ProvisionerKubeVirt:
		return "KubeVirt"
	case v1alpha1.ProvisionerLibvirt:
		return "libvirt"
	case v1alpha1.ProvisionerVSphere:
		return "vSphere"
	default:
		return provider
	}
}

func distributionLabel(distribution string) string {
	if distribution == v1alpha1.DistributionOKD {
		return "OKD"
	}
	return "OpenShift"
}

func containerDescriptor(distribution string, sub stateview.ClusterSubstrate) string {
	distro := distributionLabel(distribution)
	switch {
	case sub.Provider == "":
		return distro
	case sub.Provider == v1alpha1.ProvisionerKubeVirt && sub.Host != "":
		return distro + " · KubeVirt on " + sub.Host
	default:
		return distro + " · " + substrateLabel(sub.Provider)
	}
}

func storageDescriptor(cluster v1alpha1.StorageCluster) string {
	typ := strings.TrimSpace(cluster.Spec.Type)
	label := "Ceph storage"
	if typ != "" && typ != v1alpha1.StorageClusterTypeCeph {
		label = typ + " storage"
	}
	if v1alpha1.StorageClusterExternal(cluster) {
		label += " (external)"
	}
	return label
}

func substrateDescriptor(provider, host string) string {
	switch {
	case provider == "":
		return ""
	case provider == v1alpha1.ProvisionerKubeVirt && host != "":
		return "KubeVirt on " + host
	default:
		return substrateLabel(provider)
	}
}

func buildClusterDisplays(state v1alpha1.State) map[string]clusterDisplay {
	out := make(map[string]clusterDisplay, len(state.ContainerClusters)+len(state.StorageClusters))
	for _, cluster := range state.ContainerClusters {
		sub := stateview.ContainerClusterSubstrate(state, cluster)
		out[cluster.Metadata.Name] = clusterDisplay{
			descriptor: containerDescriptor(v1alpha1.DistributionType(cluster), sub),
			host:       sub.Host,
		}
	}
	for _, cluster := range state.StorageClusters {
		out[cluster.Metadata.Name] = clusterDisplay{
			descriptor: storageDescriptor(cluster),
			storage:    true,
		}
	}
	return out
}

func clusterGroupTitle(name string, displays map[string]clusterDisplay, fallbackKind string) string {
	if d, ok := displays[name]; ok && d.descriptor != "" {
		return name + " (" + d.descriptor + ")"
	}
	if fallbackKind != "" {
		return name + " (" + fallbackKind + ")"
	}
	return name
}

func orderClusterNames(names []string, displays map[string]clusterDisplay) []string {
	var storage, containers []string
	for _, name := range names {
		if d, ok := displays[name]; ok && d.storage {
			storage = append(storage, name)
		} else {
			containers = append(containers, name)
		}
	}
	sort.Strings(storage)
	sort.Strings(containers)

	present := make(map[string]bool, len(containers))
	for _, name := range containers {
		present[name] = true
	}
	childrenByHost := map[string][]string{}
	var roots []string
	for _, name := range containers {
		host := displays[name].host
		if host != "" && host != name && present[host] {
			childrenByHost[host] = append(childrenByHost[host], name)
			continue
		}
		roots = append(roots, name)
	}
	ordered := make([]string, 0, len(names))
	ordered = append(ordered, storage...)
	for _, root := range roots {
		ordered = append(ordered, root)
		ordered = append(ordered, childrenByHost[root]...)
	}
	return ordered
}
