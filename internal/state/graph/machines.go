package stategraph

import "github.com/crmarques/bootwright/api/v1alpha1"

func MachineWorkObjects(state v1alpha1.State, names []string) (provision, hosts map[string]bool) {
	provision = nameSet(names)
	hosts = map[string]bool{}
	for name := range provision {
		hosts[name] = true
	}
	owning := machineOwningClusterSet(state, provision)
	addSelectedServiceMachines(hosts, state, owning)
	addProviderHostMachines(hosts, state)
	return provision, hosts
}

func MachineOwningClusterRoots(state v1alpha1.State, machines map[string]bool) (container, storage []string) {
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Hosts {
			if machines[node.MachineRef.Name] {
				container = append(container, cluster.Metadata.Name)
				break
			}
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			if machines[node.MachineRef.Name] {
				storage = append(storage, cluster.Metadata.Name)
				break
			}
		}
	}
	return container, storage
}

func machineOwningClusterSet(state v1alpha1.State, machines map[string]bool) map[string]bool {
	container, storage := MachineOwningClusterRoots(state, machines)
	out := make(map[string]bool, len(container)+len(storage))
	for _, name := range container {
		out[name] = true
	}
	for _, name := range storage {
		out[name] = true
	}
	return out
}
