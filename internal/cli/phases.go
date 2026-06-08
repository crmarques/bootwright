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

// phases are the five sub-phases of the two families. infra = fabric + machines;
// clusters = deps + base + addons. Each maps to several task playbooks, so the
// per-phase ApplyPlaybook field is left empty (advisory) and the task graph is
// authoritative. NeedsRoot is coarse: base and addons mix root work (container
// install, addon ownership) with non-root work (ceph bootstrap), so it stays true.
var phases = map[string]Phase{
	"fabric": {
		Name:        "fabric",
		NeedsRoot:   true,
		Description: "converge provider hosts (BMC services) and machine-bound shared services: proxy, registry, NTP, boot artifacts, DNS, and load balancers",
	},
	"machines": {
		Name:        "machines",
		NeedsRoot:   true,
		Description: "make machines exist with an OS: per-cluster substrate, instantiation, managed-OS install, networks, name resolution, and VIPs",
	},
	"deps": {
		Name:        "deps",
		NeedsRoot:   true,
		Description: "install per-cluster prerequisites before bringup: cephadm and dependencies on storage nodes; build the openshift-install agent ISO",
	},
	"base": {
		Name:        "base",
		NeedsRoot:   true,
		Description: "bring cluster control planes up: bootstrap Ceph and apply OSDs; boot nodes and wait for openshift-install",
	},
	"addons": {
		Name:        "addons",
		NeedsRoot:   true,
		Description: "post-install integration: apply declarative cluster addons with oc and attach storage to OpenShift",
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
