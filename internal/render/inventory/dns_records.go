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

func nameResolutionRecordsVars(state v1alpha1.State, entryName string, additionalIngressHosts []string) ([]any, []any, []any) {
	hostRecords := []dnsmasqRecord{}
	domainRecords := []dnsmasqRecord{}
	baseDomain := clusterBaseDomain(state)
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
	machineRecords, cnameRecords := nodeHostRecords(state, entryName)
	hostRecords = append(hostRecords, machineRecords...)
	hostRecords = append(hostRecords, gatewayHostRecords(state, entryName)...)
	hostRecords = append(hostRecords, managementHostRecords(state, entryName)...)
	return dnsmasqRecordVars(hostRecords), dnsmasqRecordVars(domainRecords), dnsmasqRecordVars(cnameRecords)
}

func nodeHostRecords(state v1alpha1.State, entryName string) ([]dnsmasqRecord, []dnsmasqRecord) {
	var records []dnsmasqRecord
	var cnames []dnsmasqRecord
	for _, machine := range state.Machines {
		if !machineUsesNameResolution(state, machine, entryName) {
			continue
		}
		address := v1alpha1.MachineSSHAddress(machine)
		if address == "" {
			continue
		}
		fqdn := v1alpha1.MachineFQDNAddress(machine)
		if fqdn == "" {
			if hostname, ok := stateview.NodeHostname(state, machine.Metadata.Name); ok && hostname != "" && hostname != machine.Metadata.Name {
				records = append(records, dnsmasqRecord{name: hostname, address: address})
			}
			records = append(records, dnsmasqRecord{name: machine.Metadata.Name, address: address})
			continue
		}
		records = append(records, dnsmasqRecord{name: fqdn, address: address})
		if hostname, ok := stateview.NodeHostname(state, machine.Metadata.Name); ok && hostname != "" && hostname != fqdn {
			cnames = append(cnames, dnsmasqRecord{name: hostname, address: fqdn})
		}
	}
	return records, cnames
}

func gatewayHostRecords(state v1alpha1.State, entryName string) []dnsmasqRecord {
	var records []dnsmasqRecord
	for _, gw := range state.StorageObjectGateways {
		dnsName := stateview.StorageGatewayFQDN(state, gw)
		if dnsName == "" || len(gw.Spec.Ceph.Ingresses) == 0 {
			continue
		}
		if !storageClusterUsesNameResolution(state, gw.Spec.StorageClusterRef.Name, entryName) {
			continue
		}
		for _, ingress := range gw.Spec.Ceph.Ingresses {
			if ingress.Address == "" {
				continue
			}
			records = append(records, dnsmasqRecord{name: dnsName, address: ingress.Address})
		}
	}
	return records
}

func managementHostRecords(state v1alpha1.State, entryName string) []dnsmasqRecord {
	var records []dnsmasqRecord
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil || sc.Spec.Ceph.Management == nil {
			continue
		}
		mgmt := sc.Spec.Ceph.Management
		dnsName := stateview.StorageManagementFQDN(state, sc)
		if dnsName == "" || mgmt.Ingress.Address == "" {
			continue
		}
		if !storageClusterUsesNameResolution(state, sc.Metadata.Name, entryName) {
			continue
		}
		records = append(records, dnsmasqRecord{name: dnsName, address: mgmt.Ingress.Address})
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

func ClusterControllerNameResolvers(state v1alpha1.State, ci v1alpha1.ClusterInstall) []any {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	baseDomain := env.Spec.Domains.ContainerClustersDomain()
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
		if !refs[entry.Name] {
			continue
		}
		bind := entry.Address
		if entry.Management == v1alpha1.EnvironmentComponentManaged {
			component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
			if !ok || component.Spec.NameResolution == nil {
				continue
			}
			bind = component.Spec.NameResolution.BindAddress
		}
		if entry.Management != v1alpha1.EnvironmentComponentExternal &&
			entry.Management != v1alpha1.EnvironmentComponentManaged {
			continue
		}
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
		for _, node := range sc.Spec.Ceph.Topology.Nodes {
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
		return env.Spec.Domains.ContainerClustersDomain()
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
