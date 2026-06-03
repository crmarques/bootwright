package render

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/runtime/sshtrust"
	"github.com/crmarques/bootwright/internal/state/graph"
)

// Inventory builds the Ansible inventory tree per ADR-0002 § role
// taxonomy. Hosts that back a profile-based machine substrate land in
// `bootwright_infra_hosts`: libvirt uses its provider host, and KubeVirt uses
// localhost because VM operations run through a kubeconfig. Bare-metal machines
// are reached through BMCs, and vSphere guests remain remote by design. Hosts
// that back provider setup or BMC services land in `bootwright_provider_hosts`.
// Hosts that back managed InfraComponent services (LB, DNS, proxy, registry,
// artifacts, NTP) land in `bootwright_infra_component_hosts`. A host can live
// in several groups. The OCP-install and agent-node layers run on localhost.
//
// Two groups instead of one is deliberate: the cluster_infra layer
// playbook targets `bootwright_infra_hosts` directly and no longer
// needs to filter hosts by hostRef in its task body. Provider and
// InfraComponent layers target their own host groups for service convergence.
func Inventory(state v1alpha1.State, secretsDir string) map[string]any {
	return InventoryWithLocalityPolicy(state, secretsDir, locality.DefaultPolicy)
}

func InventoryWithLocalityPolicy(state v1alpha1.State, secretsDir string, localPolicy locality.Policy) map[string]any {
	infraHostSet := infraReferencedHosts(state)
	providerHostSet := providerReferencedHosts(state)
	infraComponentHostSet := infraComponentReferencedHosts(state)
	bootHostSet := bootReferencedHosts(state)
	ocpHostSet := ocpReferencedHosts(state)
	storageHostSet := storageReferencedHosts(state)
	storageHostGroups := storageClusterHostSets(state)
	allHostSet := mergeHostSets(mergeHostSets(mergeHostSets(mergeHostSets(infraHostSet, providerHostSet), infraComponentHostSet), bootHostSet), ocpHostSet)
	agentNodeHostSet, agentNodeGroups := agentNodeHostSets(state)

	var env *v1alpha1.Environment
	if len(state.Environments) > 0 {
		env = &state.Environments[0]
	}

	hosts := map[string]any{}
	for _, name := range sortedHostSet(allHostSet) {
		h, ok := findHost(state, name)
		if !ok || h.Spec.SSH == nil {
			continue
		}
		hosts[name] = hostInventoryEntry(h, env, secretsDir, localPolicy)
	}
	for _, cluster := range managedStorageClusters(state) {
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			hosts[storageInventoryHostName(cluster, node.Name)] = storageNodeInventoryEntry(state, cluster, node, env, secretsDir, localPolicy)
		}
	}
	if len(ocpHostSet) > 0 {
		hosts["localhost"] = localhostInventoryEntry()
	}
	for _, cluster := range state.ContainerClusters {
		for _, machineName := range clusterMachineNames(cluster) {
			hostName := AgentNodeHostName(cluster.Metadata.Name, machineName)
			entry := localhostInventoryEntry()
			entry["bootwright_agent_node_cluster_name"] = cluster.Metadata.Name
			entry["bootwright_agent_node_machine_name"] = machineName
			hosts[hostName] = entry
		}
	}
	children := map[string]any{
		GroupProviderHosts:       map[string]any{"hosts": hostsAsEmptyMap(providerHostSet)},
		GroupInfraComponentHosts: map[string]any{"hosts": hostsAsEmptyMap(infraComponentHostSet)},
		GroupInfraHosts:          map[string]any{"hosts": hostsAsEmptyMap(infraHostSet)},
		GroupBootHosts:           map[string]any{"hosts": hostsAsEmptyMap(bootHostSet)},
		GroupControllerHosts:     map[string]any{"hosts": hostsAsEmptyMap(ocpHostSet)},
		GroupOCPHosts:            map[string]any{"hosts": hostsAsEmptyMap(ocpHostSet)},
		GroupAgentNodeHosts:      map[string]any{"hosts": hostsAsEmptyMap(agentNodeHostSet)},
		GroupStorageHosts:        map[string]any{"hosts": hostsAsEmptyMap(storageHostSet)},
	}
	for group, set := range agentNodeGroups {
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
	GroupStorageHosts        = "bootwright_storage_hosts"
)

// HostGroupCounts returns the number of hosts in each inventory child
// group for the given state. Used to detect an ansible-playbook
// invocation that would target only empty groups (which fails with
// "no hosts to target") and skip it instead. Controller and OCP-install
// groups contain localhost when clusters are loaded.
func HostGroupCounts(state v1alpha1.State) map[string]int {
	agentNodeHostSet, agentNodeGroups := agentNodeHostSets(state)
	storageHostGroups := storageClusterHostSets(state)
	out := map[string]int{
		GroupInfraHosts:          len(infraReferencedHosts(state)),
		GroupProviderHosts:       len(providerReferencedHosts(state)),
		GroupInfraComponentHosts: len(infraComponentReferencedHosts(state)),
		GroupBootHosts:           len(bootReferencedHosts(state)),
		GroupControllerHosts:     len(ocpReferencedHosts(state)),
		GroupOCPHosts:            len(ocpReferencedHosts(state)),
		GroupAgentNodeHosts:      len(agentNodeHostSet),
		GroupStorageHosts:        len(storageReferencedHosts(state)),
	}
	for group, set := range agentNodeGroups {
		out[group] = len(set)
	}
	for group, set := range storageHostGroups {
		out[group] = len(set)
	}
	return out
}

func HostGroupMembers(state v1alpha1.State) map[string][]string {
	ocpHosts := sortedHostSet(ocpReferencedHosts(state))
	agentNodeHostSet, agentNodeGroups := agentNodeHostSets(state)
	storageHostGroups := storageClusterHostSets(state)
	out := map[string][]string{
		GroupInfraHosts:          sortedHostSet(infraReferencedHosts(state)),
		GroupProviderHosts:       sortedHostSet(providerReferencedHosts(state)),
		GroupInfraComponentHosts: sortedHostSet(infraComponentReferencedHosts(state)),
		GroupBootHosts:           sortedHostSet(bootReferencedHosts(state)),
		GroupControllerHosts:     ocpHosts,
		GroupOCPHosts:            ocpHosts,
		GroupAgentNodeHosts:      sortedHostSet(agentNodeHostSet),
		GroupStorageHosts:        sortedHostSet(storageReferencedHosts(state)),
	}
	for group, set := range agentNodeGroups {
		out[group] = sortedHostSet(set)
	}
	for group, set := range storageHostGroups {
		out[group] = sortedHostSet(set)
	}
	return out
}

func AgentNodeGroupName(clusterName string) string {
	return GroupAgentNodeHosts + "_" + inventoryGroupToken(clusterName)
}

func AgentNodeHostName(clusterName, machineName string) string {
	return clusterName + "__" + machineName
}

func hostInventoryEntry(h v1alpha1.Host, env *v1alpha1.Environment, secretsDir string, localPolicy locality.Policy) map[string]any {
	sshAddress := v1alpha1.HostSSHAddress(h)
	entry := map[string]any{
		"ansible_host":         sshAddress,
		"bootwright_host_name": h.Metadata.Name,
	}
	if locality.IsControllerLocalHost(h, localPolicy) {
		entry["ansible_connection"] = "local"
		return entry
	}
	if h.Spec.SSH.User != "" {
		entry["ansible_user"] = h.Spec.SSH.User
	}
	if path := secret.ResolveSSHPrivateKeyPath(h.Spec.SSH.KeyRef.Name, env, secretsDir); path != "" {
		entry["ansible_ssh_private_key_file"] = path
	}
	if path := hostKnownHostsPath(h, env, secretsDir); path != "" {
		entry["ansible_ssh_common_args"] = sshCommonArgs(path)
	}
	return entry
}

func hostKnownHostsPath(h v1alpha1.Host, env *v1alpha1.Environment, secretsDir string) string {
	if h.Spec.SSH == nil {
		return ""
	}
	if h.Spec.SSH.KnownHostsRef.Name != "" {
		return secret.ResolvePath(h.Spec.SSH.KnownHostsRef.Name, env, secretsDir)
	}
	return sshtrust.KnownHostsPathForSecrets(secretsDir)
}

func sshCommonArgs(knownHostsPath string) string {
	return shellQuoteArgs([]string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + knownHostsPath})
}

func localhostInventoryEntry() map[string]any {
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

func clusterMachineNames(cluster v1alpha1.ContainerCluster) []string {
	seen := map[string]bool{}
	var names []string
	for _, node := range cluster.Spec.Nodes {
		if node.InfraNodeRef.Name == "" || seen[node.InfraNodeRef.Name] {
			continue
		}
		seen[node.InfraNodeRef.Name] = true
		names = append(names, node.InfraNodeRef.Name)
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

func ocpReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	if primaryEnvironment(state) != nil {
		out["localhost"] = true
	}
	return out
}

// infraReferencedHosts returns the hosts that back a profile-based
// machine substrate. Bare-metal `machines[]` entries are reached over
// BMC from the controller. vSphere guests live on remote infrastructure and
// contribute nothing here. KubeVirt VM operations run from the controller
// against a kubeconfig, so they contribute localhost.
func infraReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ci := range state.ClusterInfras {
		for _, m := range ci.Spec.Components.Nodes {
			if host := machineHostRef(state, m); host != "" {
				out[host] = true
			}
		}
	}
	return out
}

func providerReferencedHosts(state v1alpha1.State) map[string]bool {
	return mergeHostSets(providerServiceReferencedHosts(state), providerHostSetupReferencedHosts(state))
}

func providerServiceReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, service := range stategraph.ResolveHostServices(state).Services {
		if service.IsProviderService() && service.HostRef != "" {
			out[service.HostRef] = true
		}
	}
	return out
}

func infraComponentReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, service := range stategraph.ResolveHostServices(state).Services {
		if service.IsInfraComponentService() && service.HostRef != "" {
			out[service.HostRef] = true
		}
	}
	return out
}

func providerHostSetupReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ci := range state.ClusterInfras {
		for _, machine := range ci.Spec.Components.Nodes {
			hostRef := machineHostRef(state, machine)
			if hostRef == "" {
				continue
			}
			if len(ProviderDriver(state, machine).Roles.HostSetupRoles) > 0 {
				out[hostRef] = true
			}
		}
	}
	return out
}

func bootReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ci := range state.ClusterInfras {
		for _, m := range ci.Spec.Components.Nodes {
			if host := machineHostRef(state, m); host != "" {
				out[host] = true
			}
		}
		ocp, ok := clusterForCI(state, ci)
		if !ok || !artifacts.ClusterNeedsPublication(state, ci, ocp) {
			continue
		}
		server, ok := artifacts.Select(state, ci)
		if !ok || server.Config == nil {
			continue
		}
		if host := server.Config.HostRef.Name; host != "" {
			out[host] = true
		}
	}
	return out
}

func mergeHostSets(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
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
