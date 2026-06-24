package workflow

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
)

// ApplyTaskConnectedMachines returns the set of Machine names the planned apply
// tasks will open a managed (strict-host-key-checked) SSH connection to. It
// resolves every task's Ansible --limit against that task's rendered inventory
// and maps the targeted hosts back to the Machines they back.
//
// The SSH host-trust readiness check uses it to require a trust record only for
// machines a scheduled task actually connects to. A scoped run can pull an
// object into the plan state purely as a render reference: `apply --stage base
// --clusters <ocp>` pulls a managed StorageCluster in so the container clusters'
// data-foundation attachment renders, and the base storage task only SSHes the
// cephadm seed. The cluster's provided-OS arbiter is reached only by the deps
// prereqs phase, so it is out of this run's scope and must not block the base
// run on missing trust. When deps is in scope its group-limited task selects the
// arbiter and it is required again.
//
// The mapping mirrors the Ansible --limit the run will actually pass, so it
// never drops a host a scheduled task connects to: a missed connection would
// surface as a strict-host-key failure mid-run instead of a preflight check.
func ApplyTaskConnectedMachines(tasks []ApplyTask) map[string]bool {
	connected := map[string]bool{}
	for _, task := range tasks {
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

// limitTargetHosts resolves an Ansible --limit pattern to inventory host names.
// Planner task limits are a single group or host token; a colon-joined pattern
// is split defensively. A token that names an inventory group expands to its
// members; any other token is taken as a literal host name.
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

// inventoryHostMachineNames maps each inventory host name to the Machine it
// backs. Provider, infra-component, infra, boot, and OCP groups key a host by
// its Machine name; managed StorageCluster nodes key by a derived per-node host
// name (render.StorageNodeHostName) that carries the Machine name. Hosts with no
// backing Machine (localhost, agent-node and machine-task pseudo-hosts) are
// absent and resolve to no Machine, which is correct: those tasks do not open a
// managed SSH connection to a host-trust-relevant Machine.
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
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			if node.MachineRef.Name == "" {
				continue
			}
			out[render.StorageNodeHostName(cluster.Metadata.Name, node.MachineRef.Name)] = node.MachineRef.Name
		}
	}
	return out
}
