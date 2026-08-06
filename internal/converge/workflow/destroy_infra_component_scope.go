package workflow

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

const InfraComponentDestroyScopeRecordsExtraVar = "bootwright_infra_component_destroy_scope_records"

func InfraComponentDestroyScopeRecords(state v1alpha1.State, clusterRoots []string) []string {
	scope := make(map[string]bool, len(clusterRoots))
	for _, name := range clusterRoots {
		if name = strings.TrimSpace(name); name != "" {
			scope[name] = true
		}
	}
	var out []string
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if !service.IsInfraComponentService() || strings.TrimSpace(service.MachineRef) == "" {
			continue
		}
		clusters := service.ConsumerClusters()
		if len(clusters) == 0 || !clustersWithinScope(clusters, scope) {
			continue
		}
		out = appendUniqueString(out, service.Identity.ProviderName+"-"+service.Identity.Name)
	}
	sort.Strings(out)
	return out
}

func clustersWithinScope(clusters []string, scope map[string]bool) bool {
	for _, cluster := range clusters {
		if !scope[strings.TrimSpace(cluster)] {
			return false
		}
	}
	return true
}
