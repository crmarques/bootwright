package installation

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestARequestRefusesANetworkThatDoesNotCompose(t *testing.T) {
	metal := metalOn("enp1s0", hardwareNIC("enp1s0", "52:54:00:9a:1b:01"), hardwareNIC("nic-b", "52:54:00:9a:1b:0b"))
	metal = metal.WithSpec(metal.Spec().WithPath(api.ListValue(
		api.MapValue(text("interfaceName", "enp2s0"), text("nicRef", "nic-b")),
	), "network", "interfaceBinding"))
	catalog := metalCatalog(metal, metalNetwork(declaredInterface("enp1s0", "ethernet"), declaredInterface("enp2s0", "ethernet")))
	requests, _, err := Requests(catalog, "controller", testContext)
	reported := diagnostics.Of(err)
	if len(requests) != 0 || len(reported) != 1 || reported[0].Code != "api.value" ||
		reported[0].Message != "the network configuration of Machine/metal-01 does not compose" ||
		reported[0].Remediation != "correct spec.network on Machine/metal-01" {
		t.Fatalf("requests = %d, refusal = %#v", len(requests), reported)
	}
}
