package main

import (
	"encoding/json"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const networksCluster = "clusters/networks/cluster.yaml"

func withMachineNetworks(refs ...string) exampleEdit {
	return exampleEdit{networksCluster, "  networking:\n    ovnKubernetesConfig:\n", "  networking:\n    machineNetworkRefs:\n      - " + strings.Join(refs, "\n      - ") + "\n\n    ovnKubernetesConfig:\n"}
}

// An authored machine network list keeps every node's install Network and may
// add a routed one, which the install configuration and the proxy bypass then
// carry in the authored order.
func TestAClusterNamesItsMachineNetworks(t *testing.T) {
	refusal := requireRefusal(t, editedExample(t, "lab-networks", withMachineNetworks("lab-routed")), "api.invariant", "OpenShiftCluster/networks", "$.spec.networking.machineNetworkRefs")
	if !strings.Contains(refusal.Message, "omits Network/lab-guests") || !strings.Contains(refusal.Remediation, "add lab-guests") {
		t.Fatalf("refusal = %+v", refusal)
	}
	requireRefusal(t, editedExample(t, "lab-networks", withMachineNetworks("lab-guests", "lab-absent")), "api.reference", "OpenShiftCluster/networks", "$.spec.networking.machineNetworkRefs[1]")
	state, _ := compileAcceptance(t, editedExample(t, "lab-networks", withMachineNetworks("lab-routed", "lab-guests")))
	catalog := state.Effective()
	controller := requireObject(t, catalog, api.Environment, "lab-networks").Spec().Get("controller", "machineRef").Text()
	media, _, _, err := agentinstall.Requests(catalog, controller, "lab")
	if err != nil || len(media) != 1 {
		t.Fatal(diagnostics.Of(err))
	}
	data, err := json.Marshal(media[0].InstallConfig["networking"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"machineNetwork":[{"cidr":"192.0.2.128/25"},{"cidr":"198.51.100.0/24"}]`) {
		t.Fatalf("networking = %s", data)
	}
}
