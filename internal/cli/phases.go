package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Phase struct {
	Name          string
	ApplyPlaybook string
	NeedsRoot     bool
	Description   string
}

var phases = map[string]Phase{
	"provider": {
		Name:          "provider",
		ApplyPlaybook: "bootwright.core.task_provider_services_apply",
		NeedsRoot:     true,
		Description:   "converge provider setup and BMC services",
	},
	"infra-components": {
		Name:          "infra-components",
		ApplyPlaybook: "bootwright.core.task_infra_component_services_apply",
		NeedsRoot:     true,
		Description:   "converge host-bound infra components: proxy, registry, NTP, boot artifacts, DNS, and load balancers",
	},
	"cluster-infra": {
		Name:          "cluster-infra",
		ApplyPlaybook: "bootwright.core.task_machine_infra_apply",
		NeedsRoot:     true,
		Description:   "converge per-cluster substrate, networks, name resolution, and VIPs",
	},
	"storage-infra": {
		Name:        "storage-infra",
		NeedsRoot:   true,
		Description: "prepare managed storage nodes for later Ceph provisioning",
	},
	"storage-cluster": {
		Name:        "storage-cluster",
		NeedsRoot:   false,
		Description: "provision external storage clusters with provider-native tooling",
	},
	"container-cluster": {
		Name:          "container-cluster",
		ApplyPlaybook: "bootwright.core.task_container_cluster_agent_install",
		NeedsRoot:     true,
		Description:   "run openshift-install agent and boot nodes through the provider boot path",
	},
	"addons": {
		Name:        "addons",
		NeedsRoot:   true,
		Description: "apply declarative post-install cluster addons with oc",
	},
}

// phasesForState used to derive NeedsRoot from substrate kind. In the
// new shape every machine substrate (libvirt/baremetal/vsphere/kubevirt)
// converges through provider-host root-escalation in some form, so the
// default `NeedsRoot: true` is left in place. Kept as a structural hook
// for future per-phase nuance.
func phasesForState(selected []Phase, _ v1alpha1.State) []Phase {
	return selected
}
