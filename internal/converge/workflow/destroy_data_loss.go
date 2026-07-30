package workflow

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

type DestroyDataLoss struct {
	Zapped   []string
	Released []string
}

func EvaluateDestroyDataLoss(state v1alpha1.State, storageScopeNames []string, scope DestroySafetyScope) DestroyDataLoss {
	var out DestroyDataLoss
	if scope.TearsClusters {
		out.Zapped = append(out.Zapped, storageScopeNames...)
	}
	if scope.TearsMachines {
		for _, name := range storageScopeNames {
			cluster, ok := storageClusterByName(state, name)
			if !ok || len(StorageClusterProviderOwnedOSDMachines(state, cluster)) == 0 {
				continue
			}
			out.Released = append(out.Released, name)
		}
	}
	sort.Strings(out.Zapped)
	sort.Strings(out.Released)
	return out
}

func storageClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
}

func StorageClusterProviderOwnedOSDMachines(state v1alpha1.State, cluster v1alpha1.StorageCluster) []string {
	if cluster.Spec.Ceph == nil || !v1alpha1.StorageClusterManaged(cluster) {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		machine, ok := topology.NodeMachine(state, cluster, node.Name)
		if !ok || seen[machine.Metadata.Name] {
			continue
		}
		if !machineSubstrateIsProviderOwned(state, machine) {
			continue
		}
		seen[machine.Metadata.Name] = true
		out = append(out, machine.Metadata.Name)
	}
	sort.Strings(out)
	return out
}

func machineSubstrateIsProviderOwned(state v1alpha1.State, machine v1alpha1.Machine) bool {
	if !v1alpha1.MachineRequiresSubstrate(machine) {
		return false
	}
	for _, provider := range state.InfraProviders {
		if provider.Metadata.Name != machine.Spec.Substrate.ProviderRef.Name {
			continue
		}
		return provider.Spec.Type != v1alpha1.ProvisionerBareMetal
	}
	return false
}

func (d DestroyDataLoss) Planned() bool {
	return len(d.Zapped) > 0 || len(d.Released) > 0
}

func (d DestroyDataLoss) Clusters() []string {
	out := UnionClusterNames(d.Zapped, d.Released)
	sort.Strings(out)
	return out
}

func (d DestroyDataLoss) Warning() string {
	var parts []string
	if len(d.Zapped) > 0 {
		parts = append(parts, "storage cluster(s) "+strings.Join(d.Zapped, ", ")+" are torn down with cephadm rm-cluster --zap-osds, which destroys ALL OSD DATA, and their declared devices are wiped (wipefs + sgdisk --zap-all)")
	}
	if len(d.Released) > 0 {
		parts = append(parts, "the provider-owned machines backing storage cluster(s) "+strings.Join(d.Released, ", ")+" are deleted together with their disks, which destroys ALL OSD DATA on them")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ") + ". This is irreversible."
}

func (d DestroyDataLoss) Consequence() string {
	switch {
	case len(d.Zapped) > 0 && len(d.Released) > 0:
		return "storage cluster(s) " + strings.Join(d.Clusters(), ", ") + " are torn down with cephadm rm-cluster --zap-osds and the provider-owned machines backing their OSDs are deleted with their disks"
	case len(d.Zapped) > 0:
		return "storage cluster(s) " + strings.Join(d.Zapped, ", ") + " are torn down with cephadm rm-cluster --zap-osds and their declared devices wiped"
	case len(d.Released) > 0:
		return "the provider-owned machines backing storage cluster(s) " + strings.Join(d.Released, ", ") + " are deleted with their disks, destroying their OSD data"
	}
	return ""
}
