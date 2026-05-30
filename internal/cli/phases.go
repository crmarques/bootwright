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
		ApplyPlaybook: "playbooks/layers/providers/apply.yml",
		NeedsRoot:     true,
		Description:   "converge provider services: proxy, registry, BMC, boot artifacts, and load balancers",
	},
	"cluster": {
		Name:          "cluster",
		ApplyPlaybook: "playbooks/layers/cluster_infra/apply.yml",
		NeedsRoot:     true,
		Description:   "converge per-cluster substrate, networks, name resolution, and VIPs",
	},
	"storage": {
		Name:        "storage",
		NeedsRoot:   false,
		Description: "provision external storage clusters with provider-native tooling",
	},
	"clusters": {
		Name:          "clusters",
		ApplyPlaybook: "playbooks/layers/openshift/install-agent.yml",
		NeedsRoot:     true,
		Description:   "run openshift-install agent and boot nodes through the provider boot path",
	},
	"extensions": {
		Name:        "extensions",
		NeedsRoot:   true,
		Description: "apply declarative post-install cluster extensions with oc",
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
