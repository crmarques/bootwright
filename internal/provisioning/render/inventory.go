package render

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/secret"
)

// Inventory builds the Ansible inventory tree per ADR-0002 § role
// taxonomy. Hosts that back a profile-based machine substrate (libvirt
// today — the only substrate with an on-host hypervisor) land in
// `bootwright_infra_hosts`. Bare-metal machines and vsphere/kubevirt
// guests are remote by design and have no on-host provider. Hosts
// that back managed services (LB, DNS, proxy, registry, artifacts) land
// in `bootwright_provider_hosts`. A host that does both lives in both
// groups. The OCP-install and agent-node layers run on localhost.
//
// Two groups instead of one is deliberate: the cluster_infra layer
// playbook targets `bootwright_infra_hosts` directly and no longer
// needs to filter hosts by hostRef in its task body. The providers
// layer targets `bootwright_provider_hosts` for service convergence.
func Inventory(state v1alpha1.State, secretsDir string) map[string]any {
	infraHostSet := infraReferencedHosts(state)
	serviceHostSet := serviceReferencedHosts(state)
	bootHostSet := bootReferencedHosts(state)
	ocpHostSet := ocpReferencedHosts(state)
	allHostSet := mergeHostSets(mergeHostSets(mergeHostSets(infraHostSet, serviceHostSet), bootHostSet), ocpHostSet)
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
		hosts[name] = hostInventoryEntry(h, env, secretsDir)
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
		GroupProviderHosts:   map[string]any{"hosts": hostsAsEmptyMap(serviceHostSet)},
		GroupInfraHosts:      map[string]any{"hosts": hostsAsEmptyMap(infraHostSet)},
		GroupBootHosts:       map[string]any{"hosts": hostsAsEmptyMap(bootHostSet)},
		GroupControllerHosts: map[string]any{"hosts": hostsAsEmptyMap(ocpHostSet)},
		GroupOCPHosts:        map[string]any{"hosts": hostsAsEmptyMap(ocpHostSet)},
		GroupAgentNodeHosts:  map[string]any{"hosts": hostsAsEmptyMap(agentNodeHostSet)},
	}
	for group, set := range agentNodeGroups {
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
	GroupProviderHosts   = "bootwright_provider_hosts"
	GroupInfraHosts      = "bootwright_infra_hosts"
	GroupBootHosts       = "bootwright_boot_hosts"
	GroupControllerHosts = "bootwright_controller_hosts"
	GroupOCPHosts        = "bootwright_ocp_hosts"
	GroupAgentNodeHosts  = "bootwright_agent_node_hosts"
)

// HostGroupCounts returns the number of hosts in each inventory child
// group for the given state. Used to detect an ansible-playbook
// invocation that would target only empty groups (which fails with
// "no hosts to target") and skip it instead. Controller and OCP-install
// groups contain localhost when clusters are loaded.
func HostGroupCounts(state v1alpha1.State) map[string]int {
	agentNodeHostSet, agentNodeGroups := agentNodeHostSets(state)
	out := map[string]int{
		GroupInfraHosts:      len(infraReferencedHosts(state)),
		GroupProviderHosts:   len(serviceReferencedHosts(state)),
		GroupBootHosts:       len(bootReferencedHosts(state)),
		GroupControllerHosts: len(ocpReferencedHosts(state)),
		GroupOCPHosts:        len(ocpReferencedHosts(state)),
		GroupAgentNodeHosts:  len(agentNodeHostSet),
	}
	for group, set := range agentNodeGroups {
		out[group] = len(set)
	}
	return out
}

func HostGroupMembers(state v1alpha1.State) map[string][]string {
	ocpHosts := sortedHostSet(ocpReferencedHosts(state))
	agentNodeHostSet, agentNodeGroups := agentNodeHostSets(state)
	out := map[string][]string{
		GroupInfraHosts:      sortedHostSet(infraReferencedHosts(state)),
		GroupProviderHosts:   sortedHostSet(serviceReferencedHosts(state)),
		GroupBootHosts:       sortedHostSet(bootReferencedHosts(state)),
		GroupControllerHosts: ocpHosts,
		GroupOCPHosts:        ocpHosts,
		GroupAgentNodeHosts:  sortedHostSet(agentNodeHostSet),
	}
	for group, set := range agentNodeGroups {
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

func hostInventoryEntry(h v1alpha1.Host, env *v1alpha1.Environment, secretsDir string) map[string]any {
	entry := map[string]any{
		"ansible_host":         v1alpha1.HostSSHAddress(h),
		"bootwright_host_name": h.Metadata.Name,
	}
	if h.Spec.SSH.User != "" {
		entry["ansible_user"] = h.Spec.SSH.User
	}
	if path := secret.ResolvePath(h.Spec.SSH.KeyRef.Name, env, secretsDir); path != "" {
		entry["ansible_ssh_private_key_file"] = path
	}
	return entry
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

func ocpReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	if primaryEnvironment(state) != nil {
		out["localhost"] = true
	}
	return out
}

// infraReferencedHosts returns the hosts that back a profile-based
// machine substrate. Bare-metal `machines[]` entries are reached over
// BMC from the controller, and vsphere / kubevirt guests live on
// remote infrastructure — those substrates have no on-host provider
// by design and contribute nothing here.
func infraReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ci := range state.ClusterInfras {
		for _, m := range ci.Spec.Components.Machines {
			if host := machineHostRef(state, m); host != "" {
				out[host] = true
			}
		}
	}
	return out
}

// serviceReferencedHosts returns the hosts that back rendered provider-scoped
// service work: managed services, artifact publication, and BMC services.
// One host can back several services; the set is unique.
func serviceReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ci := range state.ClusterInfras {
		for _, component := range loadBalancerComponentsForCluster(state, ci) {
			if component.Spec.LoadBalancer != nil {
				out[component.Spec.LoadBalancer.HostRef.Name] = true
			}
		}
		for _, selected := range nameResolutionComponentsForCluster(state, ci) {
			out[selected.component.Spec.NameResolution.HostRef.Name] = true
		}
	}
	for _, selected := range proxyComponentsForCluster(state) {
		out[selected.component.Spec.Proxy.HostRef.Name] = true
	}
	for _, ocp := range state.ContainerClusters {
		if selected, ok := registryComponentForCluster(state, ocp); ok {
			out[selected.component.Spec.Registry.HostRef.Name] = true
		}
	}
	if server, ok := artifactpub.Select(state); ok && server.Config != nil && anyClusterNeedsArtifactPublication(state) {
		out[server.Config.HostRef.Name] = true
	}
	for _, raw := range bmcProviderServiceVars(state) {
		service := raw.(map[string]any)
		if hostRef, _ := service["hostRef"].(string); hostRef != "" {
			out[hostRef] = true
		}
	}
	return out
}

func bootReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, ci := range state.ClusterInfras {
		for _, m := range ci.Spec.Components.Machines {
			if host := machineHostRef(state, m); host != "" {
				out[host] = true
			}
		}
		ocp, ok := clusterForCI(state, ci)
		if !ok || !artifactpub.ClusterNeedsPublication(state, ci, ocp) {
			continue
		}
		server, ok := artifactpub.Select(state)
		if !ok || server.Config == nil {
			continue
		}
		if host := server.Config.HostRef.Name; host != "" {
			out[host] = true
		}
	}
	return out
}

func anyClusterNeedsArtifactPublication(state v1alpha1.State) bool {
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInfraForOCP(state, ocp)
		if err != nil {
			continue
		}
		if artifactpub.ClusterNeedsPublication(state, ci, ocp) {
			return true
		}
	}
	return false
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
