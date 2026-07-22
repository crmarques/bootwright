package workflow

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

func ApplyTaskConnectedMachines(tasks []ApplyTask) map[string]bool {
	connected := map[string]bool{}
	for _, task := range tasks {
		if task.Entry.Kind == ApplyTaskKindClusterAddon {
			for _, machine := range inventoryHostMachineNames(task.State) {
				connected[machine] = true
			}
			continue
		}
		if strings.TrimSpace(task.Limit) == "" {
			continue
		}
		hostMachines := inventoryHostMachineNames(task.State)
		groups := render.HostGroupMembers(task.State)
		for _, host := range limitTargetHosts(task.Limit, groups) {
			if machine := hostMachines[host]; machine != "" {
				connected[machine] = true
			}
		}
	}
	return connected
}

func limitTargetHosts(limit string, groups map[string][]string) []string {
	var hosts []string
	for _, token := range strings.Split(limit, ":") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if members, ok := groups[token]; ok {
			hosts = append(hosts, members...)
			continue
		}
		hosts = append(hosts, token)
	}
	return hosts
}

func inventoryHostMachineNames(state v1alpha1.State) map[string]string {
	out := map[string]string{}
	for _, machine := range state.Machines {
		if machine.Spec.Access.SSH == nil {
			continue
		}
		out[machine.Metadata.Name] = machine.Metadata.Name
	}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name == "" {
				continue
			}
			out[render.StorageNodeHostName(cluster.Metadata.Name, node.MachineRef.Name)] = node.MachineRef.Name
		}
	}
	return out
}
