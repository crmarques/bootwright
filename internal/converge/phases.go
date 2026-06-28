package converge

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Phase struct {
	Name          string
	ApplyPlaybook string
	NeedsRoot     bool
	Description   string
	// AnsibleLimit is the inventory --limit a single-phase (sub-phase) run
	// targets; a sub-phase scope inherits it. Empty means no limit (add-ons runs
	// no ansible). The family scopes carry their own AnsibleLimit directly.
	AnsibleLimit string
}

// phases are the five sub-phases of the two families. infra = fabric + machines;
// clusters = deps + base + add-ons. Each maps to several task playbooks, so the
// per-phase ApplyPlaybook field is left empty (advisory) and the task graph is
// authoritative. NeedsRoot is coarse: base and add-ons mix root work (container
// install, add-on ownership) with non-root work (ceph bootstrap), so it stays true.
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

// PhasesForState used to derive NeedsRoot from substrate kind. In the
// new shape every machine substrate (libvirt/baremetal/vsphere/kubevirt)
// converges through provider-host root-escalation in some form, so the
// default `NeedsRoot: true` is left in place. Kept as a structural hook
// for future per-phase nuance.
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

// SelectedTargetsClusters reports whether the selected phases include cluster
// bringup work (`deps` builds the agent ISO, `base` boots and waits for install).
// Used to gate ResolveInstaller: the install_agent role consumes secret-inlined
// installer inputs under the per-cluster runtime work dir, so apply paths that
// drive that role must inline secrets before handing off to Ansible.
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
