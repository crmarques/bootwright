package inventory

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/ownership"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// Inventory builds the Ansible inventory tree per ADR-0002 § role
// taxonomy. Hosts that back a profile-based machine substrate land in
// `bootwright_infra_hosts`: libvirt uses its provider host, while KubeVirt
// and vSphere use localhost because VM operations run through a kubeconfig
// or the vCenter API. Bare-metal machines are reached through BMCs. Hosts
// that back provider setup or BMC services land in `bootwright_provider_hosts`.
// Hosts that back managed InfraComponent services (LB, DNS, proxy, registry,
// artifacts, NTP) land in `bootwright_infra_component_hosts`. A host can live
// in several groups. The OCP-install and agent-node layers run on localhost.
//
// Two groups instead of one is deliberate: the machine-infra layer
// playbook targets `bootwright_infra_hosts` directly and no longer
// needs to filter hosts by machineRef in its task body. Provider and
// InfraComponent layers target their own host groups for service convergence.
func Inventory(state v1alpha1.State, secretsDir string) map[string]any {
	return InventoryWithPathOptions(state, PathOptions{SecretsDir: secretsDir})
}

func InventoryWithPathOptions(state v1alpha1.State, paths PathOptions) map[string]any {
	return InventoryWithLocalityPolicyAndOwnershipRecordsAndPathOptions(state, paths, locality.DefaultPolicy, nil)
}

func InventoryWithLocalityPolicy(state v1alpha1.State, secretsDir string, localPolicy locality.Policy) map[string]any {
	return InventoryWithLocalityPolicyAndOwnershipRecordsAndPathOptions(state, PathOptions{SecretsDir: secretsDir}, localPolicy, nil)
}

func InventoryWithOwnershipRecords(state v1alpha1.State, secretsDir string, records []ownership.ResourceRecord) map[string]any {
	return InventoryWithOwnershipRecordsAndPathOptions(state, PathOptions{SecretsDir: secretsDir}, records)
}

func InventoryWithOwnershipRecordsAndPathOptions(state v1alpha1.State, paths PathOptions, records []ownership.ResourceRecord) map[string]any {
	return InventoryWithLocalityPolicyAndOwnershipRecordsAndPathOptions(state, paths, locality.DefaultPolicy, records)
}

func InventoryWithLocalityPolicyAndOwnershipRecords(state v1alpha1.State, secretsDir string, localPolicy locality.Policy, records []ownership.ResourceRecord) map[string]any {
	return InventoryWithLocalityPolicyAndOwnershipRecordsAndPathOptions(state, PathOptions{SecretsDir: secretsDir}, localPolicy, records)
}

func InventoryWithLocalityPolicyAndOwnershipRecordsAndPathOptions(state v1alpha1.State, paths PathOptions, localPolicy locality.Policy, records []ownership.ResourceRecord) map[string]any {
	infraHostSet := infraReferencedHosts(state)
	providerHostSet := providerReferencedHosts(state)
	infraComponentHostSet := infraComponentReferencedHosts(state)
	bootHostSet := bootReferencedHosts(state)
	ocpHostSet := ocpReferencedHosts(state)
	storageHostSet := storageReferencedHosts(state)
	storageHostGroups := storageClusterHostSets(state)
	machineTaskHostSet, machineTaskGroups := machineTaskHostSets(state)
	recorded := ownershipInventory(records)
	infraHostSet = mergeHostSets(infraHostSet, recorded.InfraHosts)
	providerHostSet = mergeHostSets(providerHostSet, recorded.ProviderHosts)
	infraComponentHostSet = mergeHostSets(infraComponentHostSet, recorded.InfraComponentHosts)
	storageHostSet = mergeHostSets(storageHostSet, recorded.StorageHosts)
	allHostSet := mergeHostSets(mergeHostSets(mergeHostSets(mergeHostSets(infraHostSet, providerHostSet), infraComponentHostSet), bootHostSet), ocpHostSet)
	agentNodeHostSet, agentNodeGroups := agentNodeHostSets(state)

	var env *v1alpha1.Environment
	if len(state.Environments) > 0 {
		env = &state.Environments[0]
	}

	hosts := map[string]any{}
	for _, name := range sortedHostSet(allHostSet) {
		h, ok := stateview.Machine(state, name)
		if !ok || h.Spec.Access.SSH == nil {
			continue
		}
		hosts[name] = machineInventoryEntry(h, env, paths, localPolicy)
	}
	for _, cluster := range ManagedStorageClusters(state) {
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			hosts[storageInventoryHostName(cluster, node.MachineRef.Name)] = storageNodeInventoryEntry(state, cluster, node, env, paths, localPolicy)
		}
	}
	for name, entry := range recorded.Hosts {
		current, ok := hosts[name].(map[string]any)
		if !ok {
			hosts[name] = entry
			continue
		}
		for key, value := range entry {
			if _, exists := current[key]; !exists {
				current[key] = value
			}
		}
	}
	if len(ocpHostSet) > 0 {
		hosts["localhost"] = localmachineInventoryEntry()
	}
	for _, cluster := range state.ContainerClusters {
		for _, machineName := range clusterMachineNames(cluster) {
			hostName := AgentNodeHostName(cluster.Metadata.Name, machineName)
			entry := localmachineInventoryEntry()
			entry["bootwright_agent_node_cluster_name"] = cluster.Metadata.Name
			entry["bootwright_agent_node_machine_name"] = machineName
			hosts[hostName] = entry
		}
	}
	for name, entry := range machineTaskHostEntries(state, env, paths, localPolicy) {
		hosts[name] = entry
	}
	children := map[string]any{
		GroupProviderHosts:       map[string]any{"hosts": hostsAsEmptyMap(providerHostSet)},
		GroupInfraComponentHosts: map[string]any{"hosts": hostsAsEmptyMap(infraComponentHostSet)},
		GroupInfraHosts:          map[string]any{"hosts": hostsAsEmptyMap(infraHostSet)},
		GroupBootHosts:           map[string]any{"hosts": hostsAsEmptyMap(bootHostSet)},
		GroupControllerHosts:     map[string]any{"hosts": hostsAsEmptyMap(ocpHostSet)},
		GroupOCPHosts:            map[string]any{"hosts": hostsAsEmptyMap(ocpHostSet)},
		GroupAgentNodeHosts:      map[string]any{"hosts": hostsAsEmptyMap(agentNodeHostSet)},
		GroupMachineTaskHosts:    map[string]any{"hosts": hostsAsEmptyMap(machineTaskHostSet)},
		GroupStorageHosts:        map[string]any{"hosts": hostsAsEmptyMap(storageHostSet)},
	}
	for group, set := range agentNodeGroups {
		children[group] = map[string]any{"hosts": hostsAsEmptyMap(set)}
	}
	for group, set := range machineTaskGroups {
		children[group] = map[string]any{"hosts": hostsAsEmptyMap(set)}
	}
	for group, set := range storageHostGroups {
		children[group] = map[string]any{"hosts": hostsAsEmptyMap(set)}
	}
	return map[string]any{
		"all": map[string]any{
			"hosts":    hosts,
			"children": children,
		},
	}
}

// Inventory group names emitted by Inventory(). Exported so callers
// reasoning about Ansible `--limit` (e.g. workflow.Run skipping an
// invocation that would target only empty groups) don't have to
// hardcode the strings.
const (
	GroupProviderHosts       = "bootwright_provider_hosts"
	GroupInfraComponentHosts = "bootwright_infra_component_hosts"
	GroupInfraHosts          = "bootwright_infra_hosts"
	GroupBootHosts           = "bootwright_boot_hosts"
	GroupControllerHosts     = "bootwright_controller_hosts"
	GroupOCPHosts            = "bootwright_ocp_hosts"
	GroupAgentNodeHosts      = "bootwright_agent_node_hosts"
	GroupMachineTaskHosts    = "bootwright_machine_task_hosts"
	GroupStorageHosts        = "bootwright_storage_hosts"
)

func AgentNodeGroupName(clusterName string) string {
	return GroupAgentNodeHosts + "_" + inventoryGroupToken(clusterName)
}

func AgentNodeHostName(clusterName, machineName string) string {
	return clusterName + "__" + machineName
}

func MachineInfraGroupName(clusterName string) string {
	return GroupMachineTaskHosts + "_container_" + inventoryGroupToken(clusterName)
}

func ManagedOSGroupName(clusterName string) string {
	return GroupMachineTaskHosts + "_storage_" + inventoryGroupToken(clusterName)
}

func MachineInfraHostName(clusterName, machineName string) string {
	return "machine__container__" + clusterName + "__" + machineName
}

func ManagedOSHostName(clusterName, machineName string) string {
	return "machine__storage__" + clusterName + "__" + machineName
}

func machineInventoryEntry(h v1alpha1.Machine, env *v1alpha1.Environment, paths PathOptions, localPolicy locality.Policy) map[string]any {
	sshAddress := v1alpha1.MachineSSHAddress(h)
	entry := map[string]any{
		"ansible_host":         sshAddress,
		"bootwright_host_name": h.Metadata.Name,
	}
	if locality.IsControllerLocalMachine(h, localPolicy) {
		entry["ansible_connection"] = "local"
		return entry
	}
	if h.Spec.Access.SSH.User != "" {
		entry["ansible_user"] = h.Spec.Access.SSH.User
	}
	if path := secret.ResolveSSHPrivateKeyPath(h.Spec.Access.SSH.KeyRef.Name, env, paths.SecretsDir); path != "" {
		entry["ansible_ssh_private_key_file"] = path
	}
	if path := machineKnownHostsPath(h, env, paths); path != "" {
		entry["ansible_ssh_common_args"] = sshCommonArgs(path)
	}
	return entry
}

func machineKnownHostsPath(h v1alpha1.Machine, env *v1alpha1.Environment, paths PathOptions) string {
	if h.Spec.Access.SSH == nil {
		return ""
	}
	if h.Spec.Access.SSH.KnownHostsRef.Name != "" {
		return secret.ResolvePath(h.Spec.Access.SSH.KnownHostsRef.Name, env, paths.SecretsDir)
	}
	return sshtrust.KnownHostsPathForSecrets(paths.trustSecretsDir())
}

func sshCommonArgs(knownHostsPath string) string {
	return shellQuoteArgs([]string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + knownHostsPath})
}

func localmachineInventoryEntry() map[string]any {
	return map[string]any{
		"ansible_connection":   "local",
		"ansible_host":         "localhost",
		"bootwright_host_name": "localhost",
	}
}

func agentNodeHostSets(state v1alpha1.State) (map[string]bool, map[string]map[string]bool) {
	all := map[string]bool{}
	byGroup := map[string]map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		groupName := AgentNodeGroupName(cluster.Metadata.Name)
		group := map[string]bool{}
		for _, machineName := range clusterMachineNames(cluster) {
			hostName := AgentNodeHostName(cluster.Metadata.Name, machineName)
			all[hostName] = true
			group[hostName] = true
		}
		byGroup[groupName] = group
	}
	return all, byGroup
}

func machineTaskHostSets(state v1alpha1.State) (map[string]bool, map[string]map[string]bool) {
	all := map[string]bool{}
	byGroup := map[string]map[string]bool{}
	add := func(groupName, hostName string) {
		all[hostName] = true
		group := byGroup[groupName]
		if group == nil {
			group = map[string]bool{}
			byGroup[groupName] = group
		}
		group[hostName] = true
	}
	for _, cluster := range state.ContainerClusters {
		ci, err := clusterInstallForOCP(state, cluster)
		if err != nil {
			continue
		}
		for _, machine := range ci.Machines {
			if machineHostRef(state, machine) == "" {
				continue
			}
			add(MachineInfraGroupName(cluster.Metadata.Name), MachineInfraHostName(cluster.Metadata.Name, machine.Name))
		}
	}
	for _, cluster := range ManagedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		for _, machine := range ci.Machines {
			rawMachine, ok := stateview.Machine(state, machine.Name)
			if !ok || !v1alpha1.MachineInstallsOS(rawMachine) || machineHostRef(state, machine) == "" {
				continue
			}
			add(ManagedOSGroupName(cluster.Metadata.Name), ManagedOSHostName(cluster.Metadata.Name, machine.Name))
		}
	}
	return all, byGroup
}

func machineTaskHostEntries(state v1alpha1.State, env *v1alpha1.Environment, paths PathOptions, localPolicy locality.Policy) map[string]any {
	out := map[string]any{}
	add := func(hostName, clusterName, machineName string, machine v1alpha1.InstallMachine) {
		providerHost := machineHostRef(state, machine)
		if providerHost == "" {
			return
		}
		var entry map[string]any
		if providerMachine, ok := stateview.Machine(state, providerHost); ok {
			if providerMachine.Spec.Access.SSH == nil {
				return
			}
			entry = machineInventoryEntry(providerMachine, env, paths, localPolicy)
		} else if providerHost == "localhost" {
			// API-native substrates (kubevirt, vsphere) run machine tasks
			// on the controller; no Machine object backs the localhost ref.
			entry = localmachineInventoryEntry()
		} else {
			return
		}
		entry["bootwright_machine_task_cluster_name"] = clusterName
		entry["bootwright_machine_task_machine_name"] = machineName
		entry["bootwright_machine_task_provider_host_name"] = providerHost
		out[hostName] = entry
	}
	for _, cluster := range state.ContainerClusters {
		ci, err := clusterInstallForOCP(state, cluster)
		if err != nil {
			continue
		}
		for _, machine := range ci.Machines {
			add(MachineInfraHostName(cluster.Metadata.Name, machine.Name), cluster.Metadata.Name, machine.Name, machine)
		}
	}
	for _, cluster := range ManagedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		for _, machine := range ci.Machines {
			rawMachine, ok := stateview.Machine(state, machine.Name)
			if !ok || !v1alpha1.MachineInstallsOS(rawMachine) {
				continue
			}
			add(ManagedOSHostName(cluster.Metadata.Name, machine.Name), cluster.Metadata.Name, machine.Name, machine)
		}
	}
	return out
}

func clusterMachineNames(cluster v1alpha1.ContainerCluster) []string {
	seen := map[string]bool{}
	var names []string
	for _, node := range cluster.Spec.Hosts {
		if node.MachineRef.Name == "" || seen[node.MachineRef.Name] {
			continue
		}
		seen[node.MachineRef.Name] = true
		names = append(names, node.MachineRef.Name)
	}
	sort.Strings(names)
	return names
}

func inventoryGroupToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func hostsAsEmptyMap(set map[string]bool) map[string]any {
	out := map[string]any{}
	for _, name := range sortedHostSet(set) {
		out[name] = map[string]any{}
	}
	return out
}

func sortedHostSet(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for n := range set {
		if n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}
