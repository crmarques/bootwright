package render

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/secret"
)

// Inventory builds the Ansible inventory tree per ADR-0002 § role
// taxonomy. Hosts that back a profile-based machine substrate (libvirt
// today — the only substrate with an on-host hypervisor) land in
// `bootwright_infra_hosts`. Bare-metal machines and vsphere/kubevirt
// guests are remote by design and have no on-host provider. Hosts
// that back a provider-scoped service (LB, DNS, proxy, registry,
// artifacts) land in `bootwright_provider_hosts`. A host that does
// both lives in both groups — Ansible group membership can overlap.
// The OCP-install layer runs against `bootwright_ocp_hosts`
// (currently just localhost — `openshift-install agent` is
// controller-driven).
//
// Two groups instead of one is deliberate: the cluster_infra layer
// playbook targets `bootwright_infra_hosts` directly and no longer
// needs to filter hosts by hostRef in its task body. The providers
// layer targets `bootwright_provider_hosts` for service convergence.
func Inventory(state v1alpha1.State, secretsDir string) map[string]any {
	infraHostSet := infraReferencedHosts(state)
	serviceHostSet := serviceReferencedHosts(state)
	bootHostSet := bootReferencedHosts(state)
	allHostSet := mergeHostSets(mergeHostSets(infraHostSet, serviceHostSet), bootHostSet)

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
		hosts[name] = entry
	}
	return map[string]any{
		"all": map[string]any{
			"hosts": hosts,
			"children": map[string]any{
				GroupProviderHosts: map[string]any{"hosts": hostsAsEmptyMap(serviceHostSet)},
				GroupInfraHosts:    map[string]any{"hosts": hostsAsEmptyMap(infraHostSet)},
				GroupBootHosts:     map[string]any{"hosts": hostsAsEmptyMap(bootHostSet)},
				GroupOCPHosts: map[string]any{
					"hosts": map[string]any{
						"localhost": map[string]any{"ansible_connection": "local"},
					},
				},
			},
		},
	}
}

// Inventory group names emitted by Inventory(). Exported so callers
// reasoning about Ansible `--limit` (e.g. workflow.Run skipping an
// invocation that would target only empty groups) don't have to
// hardcode the strings.
const (
	GroupProviderHosts = "bootwright_provider_hosts"
	GroupInfraHosts    = "bootwright_infra_hosts"
	GroupBootHosts     = "bootwright_boot_hosts"
	GroupOCPHosts      = "bootwright_ocp_hosts"
)

// HostGroupCounts returns the number of hosts in each inventory child
// group for the given state. Used to detect an ansible-playbook
// invocation that would target only empty groups (which fails with
// "no hosts to target") and skip it instead. `bootwright_ocp_hosts`
// always contains localhost, so its count is always 1.
func HostGroupCounts(state v1alpha1.State) map[string]int {
	return map[string]int{
		GroupInfraHosts:    len(infraReferencedHosts(state)),
		GroupProviderHosts: len(serviceReferencedHosts(state)),
		GroupBootHosts:     len(bootReferencedHosts(state)),
		GroupOCPHosts:      1,
	}
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
		components := ci.Spec.Components
		for _, c := range components.LoadBalancers {
			if lb, ok := resolveLoadBalancer(state, c.From); ok && lb.HAProxy != nil {
				out[lb.HAProxy.HostRef.Name] = true
			}
		}
		if c := components.Proxy; c != nil {
			if pr, ok := resolveProxy(state, c.From); ok && pr.Squid != nil {
				out[pr.Squid.HostRef.Name] = true
			}
		}
		if c := components.NameResolution; c != nil {
			if d, ok := resolveDNS(state, c.From); ok && d.Dnsmasq != nil {
				out[d.Dnsmasq.HostRef.Name] = true
			}
		}
		if c := components.Registry; c != nil {
			if r, ok := resolveRegistry(state, c.From); ok && r.MirrorRegistry != nil {
				out[r.MirrorRegistry.HostRef.Name] = true
			}
		}
	}
	if publisher, ok := artifactpub.Select(state); ok && publisher.Capability.HTTP != nil && anyClusterNeedsArtifactPublication(state) {
		out[publisher.Capability.HTTP.HostRef.Name] = true
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
		publisher, ok := artifactpub.Select(state)
		if !ok || publisher.Capability.HTTP == nil {
			continue
		}
		if host := publisher.Capability.HTTP.HostRef.Name; host != "" {
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
