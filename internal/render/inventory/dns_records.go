package inventory

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/installer"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

type dnsmasqRecord struct {
	name    string
	address string
}

func nameResolutionRecordsVars(state v1alpha1.State, entryName string, additionalIngressHosts []string) ([]any, []any) {
	hostRecords := []dnsmasqRecord{}
	domainRecords := []dnsmasqRecord{}
	for _, ocp := range state.ContainerClusters {
		ci, err := installer.ClusterInstallForOCP(state, ocp)
		if err != nil || !clusterUsesNameResolution(state, ci, entryName) {
			continue
		}
		baseDomain := clusterBaseDomain(state)
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
	return dnsmasqRecordVars(hostRecords), dnsmasqRecordVars(domainRecords)
}

func clusterUsesNameResolution(state v1alpha1.State, ci v1alpha1.ClusterInstall, refName string) bool {
	if refName == "" {
		return false
	}
	for _, network := range stateview.ClusterNetworkConfigs(state, ci) {
		for _, ref := range network.Spec.NameResolutionRefs {
			if ref.Name == refName {
				return true
			}
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
