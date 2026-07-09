package converge

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Phase struct {
	Name         string
	NeedsRoot    bool
	Description  string
	AnsibleLimit string
}

var phases = map[string]Phase{
	PhaseFabric: {
		Name:         PhaseFabric,
		NeedsRoot:    true,
		Description:  "converge provider hosts (BMC services) and machine-bound shared services: proxy, registry, NTP, boot artifacts, DNS, and load balancers",
		AnsibleLimit: infraAnsibleLimit,
	},
	PhaseMachines: {
		Name:         PhaseMachines,
		NeedsRoot:    true,
		Description:  "make machines exist with an OS: per-cluster substrate, instantiation, managed-OS install, networks, name resolution, and VIPs",
		AnsibleLimit: infraAnsibleLimit,
	},
	PhaseDeps: {
		Name:         PhaseDeps,
		NeedsRoot:    true,
		Description:  "install per-cluster prerequisites before bringup: cephadm and dependencies on storage nodes; build the openshift-install agent ISO",
		AnsibleLimit: clustersAnsibleLimit,
	},
	PhaseBase: {
		Name:         PhaseBase,
		NeedsRoot:    true,
		Description:  "bring cluster control planes up: bootstrap Ceph and apply OSDs; boot nodes and wait for openshift-install",
		AnsibleLimit: clustersAnsibleLimit,
	},
	PhaseAddons: {
		Name:        PhaseAddons,
		NeedsRoot:   true,
		Description: "post-install integration: apply declarative cluster add-ons with oc and attach storage to OpenShift",
	},
}

func PhasesForState(selected []Phase, _ v1alpha1.State) []Phase {
	return selected
}

func RootPhaseCount(selected []Phase) int {
	count := 0
	for _, p := range selected {
		if p.NeedsRoot {
			count++
		}
	}
	return count
}

func UseControllingTTYForWorkflow(selected []Phase, askBecomePass bool) bool {
	return !askBecomePass && RootPhaseCount(selected) > 0
}

func SelectedTargetsClusters(selected []Phase) bool {
	for _, p := range selected {
		if p.Name == PhaseDeps || p.Name == PhaseBase {
			return true
		}
	}
	return false
}

func selectedPhaseNames(selected []Phase) []string {
	names := make([]string, 0, len(selected))
	for _, phase := range selected {
		names = append(names, phase.Name)
	}
	return names
}
