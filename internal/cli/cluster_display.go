package cli

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// clusterDisplay holds the human descriptor and ordering metadata for one
// cluster, derived once from desired state and shared by every CLI surface
// (apply frame, summary, status, inventory) so they all describe a cluster the
// same way. In a mixed fleet this is what lets a bare-metal OpenShift parent, a
// KubeVirt-hosted child, and a Ceph cluster read differently at a glance.
type clusterDisplay struct {
	descriptor string // e.g. "OpenShift · KubeVirt on dc1-metal-ocp", "Ceph storage"
	storage    bool   // true for StorageClusters
	host       string // KubeVirt host cluster, used to order children after their parent
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

// containerDescriptor renders "<distro> · <substrate>" and, for KubeVirt,
// "<distro> · KubeVirt on <host>" so a bare-metal parent and a KubeVirt-hosted
// child are distinguishable rather than both reading as "ContainerCluster".
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

// substrateDescriptor formats a provisioner (+ KubeVirt host) without a
// distribution prefix, for surfaces (cluster list/access) that already name the
// cluster's install mode/method and only need the substrate appended.
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

// buildClusterDisplays precomputes the descriptor and ordering metadata for
// every declared cluster. The result is keyed by cluster name so ledger-fed
// surfaces (which know only the name) can describe a cluster the same way the
// state-fed surfaces do.
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

// clusterGroupTitle builds the per-cluster group heading "name (descriptor)".
// It prefers the desired-state descriptor; when state is unavailable it falls
// back to the ledger-derived kind word so the heading is never empty.
func clusterGroupTitle(name string, displays map[string]clusterDisplay, fallbackKind string) string {
	if d, ok := displays[name]; ok && d.descriptor != "" {
		return name + " (" + d.descriptor + ")"
	}
	if fallbackKind != "" {
		return name + " (" + fallbackKind + ")"
	}
	return name
}

// orderClusterNames returns the given cluster names in a topology-aware order:
// storage clusters first, then each container "root" (a bare-metal/standalone
// cluster) immediately followed by the KubeVirt children it hosts. This matches
// apply/teardown reality (a child cannot install before its host) instead of
// the alphabetical order that would print a child above its own parent. Order
// is deterministic: roots and children are alphabetical within their group.
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
