package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func InfraComponentServiceSelection(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
	if !service.IsInfraComponentService() {
		return nil, false
	}
	selection, ok := machineServiceVarsFromGraph(state, service)
	if !ok {
		return nil, false
	}
	delete(selection, "clusterName")
	delete(selection, "consumingClusters")
	return selection, true
}
