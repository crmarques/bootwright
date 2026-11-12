package machine

import (
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/diskselection"
	"github.com/crmarques/bootwright/internal/substrate"
)

func validateDiscoveryRoot(o api.Object, variant string) []api.Issue {
	const path = "$.spec.substrate.baremetal.rootDevice.discover"
	root := o.Spec().Get("substrate", "baremetal", "rootDevice")
	if variant != substrate.ArmBaremetal || !installed(o) || len(substrate.MachineRootDeviceHints(o).Names()) != 1 {
		return []api.Issue{invariant(path, "automatic root discovery selects alone on a Bootwright-installed physical RHEL Machine", "declare rootDevice.discover alone and select an Anaconda installation profile")}
	}
	bmc := substrate.MachineBMC(o)
	if !bmc.Get("tls", "verify").Bool() || !strings.HasPrefix(bmc.Get("address").Text(), "https://") {
		return []api.Issue{invariant(path, "automatic disk discovery requires verified BMC TLS", "select full or certificate verification on the HTTPS BMC connection")}
	}
	if _, err := diskselection.PolicyFromValue(root.Get("discover")); err != nil {
		return []api.Issue{invariant(path, err.Error(), "use qualified disk predicates and stable exclusions with exact capacities")}
	}
	return nil
}
