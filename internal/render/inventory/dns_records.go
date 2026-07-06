package inventory

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

type dnsmasqRecord struct {
	name    string
	address string
}

func nameResolutionRecordsVars(state v1alpha1.State, entryName string, additionalIngressHosts []string) ([]any, []any) {
	hostRecords := []dnsmasqRecord{}
	domainRecords := []dnsmasqRecord{}
	baseDomain := clusterBaseDomain(state)
	// Container-cluster endpoints (api/api-int/apps) plus any additional ingress
	// hosts pinned to the cluster ingress VIP.
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInstallForOCP(state, ocp)
		if err != nil || !clusterUsesNameResolution(state, ci, entryName) {
			continue
		}
		if baseDomain == "" {
			continue
		}
		clusterName := ocp.Metadata.Name
		if address := stateview.ContainerEndpointAddress(state, ci, ocp, v1alpha1.EndpointAPI); address != "" {
			hostRecords = append(hostRecords, dnsmasqRecord{
				name:    "api." + clusterName + "." + baseDomain,
				address: address,
			})
		}
		if address := stateview.ContainerEndpointAddress(state, ci, ocp, v1alpha1.EndpointAPIInt); address != "" {
			hostRecords = append(hostRecords, dnsmasqRecord{
				name:    "api-int." + clusterName + "." + baseDomain,
				address: address,
			})
		}
		if address := stateview.ContainerEndpointAddress(state, ci, ocp, v1alpha1.EndpointIngress); address != "" {
			domainRecords = append(domainRecords, dnsmasqRecord{
				name:    "apps." + clusterName + "." + baseDomain,
				address: address,
			})
			for _, host := range additionalIngressHosts {
				if host != "" {
					hostRecords = append(hostRecords, dnsmasqRecord{name: host, address: address})
				}
			}
		}
	}
	// Node A records: every machine this resolver serves, published by its
	// registered FQDN and by its bare name, so the hostname cephadm and the
	// installer use — e.g. the alertmanager host the Ceph dashboard dials —
	// resolves cluster-wide, for storage-only environments as much as OpenShift.
	hostRecords = append(hostRecords, nodeHostRecords(state, entryName)...)
	// Object-gateway S3 endpoints: the gateway owns both its public dnsName and
	// its ingress VIP, so publish that mapping for resolvers its cluster uses.
	hostRecords = append(hostRecords, gatewayHostRecords(state, entryName)...)
	// Ceph management VIP: the storage cluster owns its management dnsName and
	// the mgmt-gateway ingress VIP, so publish that mapping the same way.
	hostRecords = append(hostRecords, managementHostRecords(state, entryName)...)
	return dnsmasqRecordVars(hostRecords), dnsmasqRecordVars(domainRecords)
}

// nodeHostRecords builds the FQDN and bare-name A records for every machine
// whose network config references the named resolver.
func nodeHostRecords(state v1alpha1.State, entryName string) []dnsmasqRecord {
	var records []dnsmasqRecord
	for _, machine := range state.Machines {
		if !machineUsesNameResolution(state, machine, entryName) {
			continue
		}
		address := v1alpha1.MachineSSHAddress(machine)
		if address == "" {
			continue
		}
		if hostname, ok := stateview.NodeHostname(state, machine.Metadata.Name); ok && hostname != "" && hostname != machine.Metadata.Name {
			records = append(records, dnsmasqRecord{name: hostname, address: address})
		}
		records = append(records, dnsmasqRecord{name: machine.Metadata.Name, address: address})
	}
	return records
}

// gatewayHostRecords publishes each object gateway's public dnsName at its first
// ingress VIP, for resolvers used by the gateway's storage cluster.
func gatewayHostRecords(state v1alpha1.State, entryName string) []dnsmasqRecord {
	var records []dnsmasqRecord
	for _, gw := range state.StorageObjectGateways {
		dnsName := gw.Spec.Public.DNSName
		if dnsName == "" || len(gw.Spec.Ceph.Ingresses) == 0 {
			continue
		}
		vip := gw.Spec.Ceph.Ingresses[0].Address
		if vip == "" || !storageClusterUsesNameResolution(state, gw.Spec.StorageClusterRef.Name, entryName) {
			continue
		}
		records = append(records, dnsmasqRecord{name: dnsName, address: vip})
	}
	return records
}

// managementHostRecords publishes each storage cluster's management dnsName at
// its mgmt-gateway ingress VIP, for resolvers the cluster's nodes use.
func managementHostRecords(state v1alpha1.State, entryName string) []dnsmasqRecord {
	var records []dnsmasqRecord
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil || sc.Spec.Ceph.Management == nil {
			continue
		}
		mgmt := sc.Spec.Ceph.Management
		if mgmt.DNSName == "" || mgmt.Ingress.Address == "" {
			continue
		}
		if !storageClusterUsesNameResolution(state, sc.Metadata.Name, entryName) {
			continue
		}
		records = append(records, dnsmasqRecord{name: mgmt.DNSName, address: mgmt.Ingress.Address})
	}
	return records
}

func clusterUsesNameResolution(state v1alpha1.State, ci v1alpha1.ClusterInstall, refName string) bool {
	if refName == "" {
		return false
	}
	for _, network := range stateview.ClusterNetworkConfigs(state, ci) {
		if nameResolutionRefsContain(network.Spec.NameResolutionRefs, refName) {
			return true
		}
	}
	return false
}

// ClusterControllerNameResolvers projects the managed dnsmasq resolvers a
// cluster's node networks reference, as {bindAddress, domain} pairs. The
// controller wires its own resolver to these before the agent-install gate so
// `openshift-install` (which polls the API from the controller) resolves the
// cluster endpoints. Only managed entries with a usable bind address are
// returned; external/operator-owned name resolution stays the operator's job.
func ClusterControllerNameResolvers(state v1alpha1.State, ci v1alpha1.ClusterInstall) []any {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	baseDomain := env.Spec.BaseDomain
	if baseDomain == "" {
		return nil
	}
	refs := map[string]bool{}
	for _, network := range stateview.ClusterNetworkConfigs(state, ci) {
		for _, ref := range network.Spec.NameResolutionRefs {
			if ref.Name != "" {
				refs[ref.Name] = true
			}
		}
	}
	if len(refs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []any
	for _, entry := range env.Spec.InfraComponents.NameResolution {
		if entry.Management != v1alpha1.EnvironmentComponentManaged || !refs[entry.Name] {
			continue
		}
		component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
		if !ok || component.Spec.NameResolution == nil {
			continue
		}
		bind := component.Spec.NameResolution.BindAddress
		// Wildcard binds ("", 0.0.0.0, ::) are not routable resolver
		// addresses; emitting DNS=:: into the controller's
		// systemd-resolved drop-in leaves it unable to resolve api/api-int.
		if bind == "" || bind == "0.0.0.0" || bind == "::" || seen[bind] {
			continue
		}
		seen[bind] = true
		out = append(out, map[string]any{
			"bindAddress": bind,
			"domain":      baseDomain,
		})
	}
	return out
}

// machineUsesNameResolution reports whether a machine's network config — named
// or inline — references the named resolver.
func machineUsesNameResolution(state v1alpha1.State, machine v1alpha1.Machine, entryName string) bool {
	if entryName == "" {
		return false
	}
	config := machine.Spec.Network.Config
	if config.Spec != nil && nameResolutionRefsContain(config.Spec.NameResolutionRefs, entryName) {
		return true
	}
	if config.NetworkConfigRef.Name == "" {
		return false
	}
	network, ok := stateview.NetworkConfig(state, config.NetworkConfigRef.Name)
	return ok && nameResolutionRefsContain(network.Spec.NameResolutionRefs, entryName)
}

func storageClusterUsesNameResolution(state v1alpha1.State, clusterName, entryName string) bool {
	for _, sc := range state.StorageClusters {
		if sc.Metadata.Name != clusterName || sc.Spec.Ceph == nil {
			continue
		}
		for _, node := range sc.Spec.Ceph.Topology.Hosts {
			if machine, ok := stateview.Machine(state, node.MachineRef.Name); ok && machineUsesNameResolution(state, machine, entryName) {
				return true
			}
		}
	}
	return false
}

func nameResolutionRefsContain(refs []v1alpha1.LocalObjectReference, entryName string) bool {
	for _, ref := range refs {
		if ref.Name == entryName {
			return true
		}
	}
	return false
}

func clusterBaseDomain(state v1alpha1.State) string {
	if env := stateview.Environment(state); env != nil {
		return env.Spec.BaseDomain
	}
	return ""
}

func dnsmasqRecordVars(records []dnsmasqRecord) []any {
	if len(records) == 0 {
		return nil
	}
	seen := map[string]bool{}
	unique := make([]dnsmasqRecord, 0, len(records))
	for _, record := range records {
		if record.name == "" || record.address == "" {
			continue
		}
		key := record.name + "|" + record.address
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, record)
	}
	sort.SliceStable(unique, func(i, j int) bool {
		if unique[i].name != unique[j].name {
			return unique[i].name < unique[j].name
		}
		return unique[i].address < unique[j].address
	})
	out := make([]any, 0, len(unique))
	for _, record := range unique {
		out = append(out, map[string]any{
			"name":    record.name,
			"address": record.address,
		})
	}
	return out
}
