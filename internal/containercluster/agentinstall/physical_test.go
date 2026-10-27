package agentinstall

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func physicalWithController(change func(api.Value) api.Value) api.Catalog {
	objects := physicalCatalog().Objects()
	for i, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "metal-01" {
			objects[i] = object.WithSpec(object.Spec().WithPath(change(object.Spec().Get("substrate", "baremetal", "bmc")), "substrate", "baremetal", "bmc"))
		}
	}
	return api.NewCatalog(objects)
}

func TestPhysicalPrivateDeliveryRequiresVerifiedTLSOnBothLegs(t *testing.T) {
	for name, test := range map[string]struct {
		change func(api.Value) api.Value
		want   string
	}{
		"verified import": {func(v api.Value) api.Value { return v }, ""},
		"HTTP": {func(v api.Value) api.Value {
			return v.With("address", api.StringValue("http://bmc.example.test/redfish/v1/Systems/1"))
		}, "HTTPS"},
		"unverified controller": {func(v api.Value) api.Value { return v.WithPath(api.BoolValue(false), "tls", "verify") }, "verifies"},
		"unverified fetch": {func(v api.Value) api.Value {
			return v.WithPath(api.StringValue("disableVerification"), "virtualMedia", "tls", "trust")
		}, "verification"},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := physicalWithController(test.change)
			cluster, _ := catalog.Find(api.OpenShiftCluster, "metal")
			_, reason, remedy := refusedCluster(catalog, cluster)
			if test.want == "" {
				if reason != "" {
					t.Fatalf("safe physical nodes refused: %s (%s)", reason, remedy)
				}
			} else if !strings.Contains(reason, test.want) || !strings.Contains(remedy, "Machine/metal-01") {
				t.Fatalf("refusal = %q (%q)", reason, remedy)
			}
		})
	}
}

func TestPhysicalInstallRequestGolden(t *testing.T) {
	_, request, _ := onlyRequests(t, physicalCatalog())
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-install-physical", canonical)
	if _, err := DecodeInstallRequest(canonical); err != nil {
		t.Fatal(err)
	}
}
