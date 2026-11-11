package managedservice

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

func TestLoopbackOnlyPlacementNeedsNoFirewallGrantBlock(t *testing.T) {
	host := api.NewObject(api.Machine, "host", api.Value{}, api.MapValue().With("os", api.MapValue().With("provided", api.BoolValue(false)).With("installProfileRef", api.StringValue("rhel"))))
	profile := api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "customizations", "security", "firewall", "enabled"))
	catalog := api.NewCatalog([]api.Object{host, profile})
	placement := machine.Placement{Connection: machine.ConnectionInstalled, Machine: "host"}
	if !machine.ManagesFirewall(catalog, "host") {
		t.Fatal("fixture does not manage its firewall")
	}
	for _, requirement := range PlacementRequirements(catalog, placement, []string{"127.0.0.1", "::1"}) {
		if requirement.Family == machine.FirewallFamily {
			t.Fatal("loopback-only service requires a grant that will not be planned")
		}
	}
	found := false
	for _, requirement := range PlacementRequirements(catalog, placement, []string{"192.0.2.10"}) {
		found = found || requirement.Family == machine.FirewallFamily
	}
	if !found {
		t.Fatal("nonloopback listener lost its firewall requirement")
	}
}
