package agentinstall

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestInstallNetworkManifestGolden(t *testing.T) {
	cluster := api.NewObject(api.ContainerCluster, "cluster", api.MapValue(), api.MapValue(api.FieldValue{Name: "networking", Value: api.MapValue(api.FieldValue{Name: "ovnKubernetesConfig", Value: api.MapValue(api.FieldValue{Name: "mtu", Value: api.IntegerValue("1400")}, api.FieldValue{Name: "gatewayConfig", Value: api.MapValue(api.FieldValue{Name: "routingViaHost", Value: api.BoolValue(true)}, api.FieldValue{Name: "ipForwarding", Value: api.StringValue("Global")})})})}))
	manifests := installManifests(cluster)
	if err := validateInstallManifests(manifests); err != nil {
		t.Fatal(err)
	}
	data, err := reconciliation.Freeze(map[string]any{"manifests": manifests}, "install manifests")
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "install-network-manifest", data)
	original := MediaRequest{Version: mediaRequestVersion, Manifests: manifests}
	before, _ := original.Canonical()
	original.Manifests = nil
	after, _ := original.Canonical()
	if string(before) == string(after) {
		t.Fatal("manifest absent from frozen identity")
	}
	manifests[0].File = "../escape.yaml"
	if validateInstallManifests(manifests) == nil {
		t.Fatal("escaping manifest accepted")
	}
}
