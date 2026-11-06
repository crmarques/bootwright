package agentinstall

import (
	"reflect"
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

func physicalWithRoot(hints ...api.FieldValue) api.Catalog {
	objects := physicalCatalog().Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "metal-01" {
			objects[index] = withHints(object, hints...)
		}
	}
	return api.NewCatalog(objects)
}

func TestPhysicalAgentRootRefusesBeforeRenderingInstallerInputs(t *testing.T) {
	for _, device := range []string{"/dev/sda", "/dev/disk/by-path/pci-0000:00:04.0"} {
		catalog := physicalWithRoot(text("deviceName", device))
		refusals := Refusals(catalog)
		if len(refusals) != 1 || !strings.Contains(refusals[0].Reason, "stable WWN") || !strings.Contains(refusals[0].Remediation, "Machine/metal-01") {
			t.Fatalf("unsafe root refusal = %+v", refusals)
		}
		if _, _, _, err := Requests(catalog, "controller", testContext); err == nil {
			t.Fatal("unsafe physical root produced installer requests")
		}
	}
}

func TestPhysicalAgentSerialRootReachesTheFrozenNativeInputGolden(t *testing.T) {
	catalog := physicalWithRoot(text("serialNumber", "0000123456"), field("sizeBytes", api.IntegerValue("8192")))
	media, install, _ := onlyRequests(t, catalog)
	if got := install.Nodes[0].Hardware.RootDisk; got == nil || got.SerialNumber != "0000123456" || got.SizeBytes != 8192 {
		t.Fatalf("root disk proof = %+v", got)
	}
	hints, ok := hostHints(t, media)["master-0"].(map[string]any)
	if !ok || !reflect.DeepEqual(hints, map[string]any{"serialNumber": "0000123456"}) {
		t.Fatalf("serial-selected root hints = %#v", hints)
	}
	frozen, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMediaRequest(frozen)
	if err != nil || !reflect.DeepEqual(hostHints(t, decoded)["master-0"], hints) {
		t.Fatalf("frozen serial hints changed: %v", err)
	}
	frozen, err = install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-install-physical-serial", frozen)
	decodedInstall, err := DecodeInstallRequest(frozen)
	if err != nil || !reflect.DeepEqual(decodedInstall.Nodes[0].Hardware.RootDisk, install.Nodes[0].Hardware.RootDisk) {
		t.Fatalf("frozen root disk proof changed: %v", err)
	}
}

func TestAFrozenSerialSelectedNodeCannotDropOrCorruptItsDiskProof(t *testing.T) {
	for name, corrupt := range map[string]func(*Hardware){
		"missing selector": func(h *Hardware) { h.RootSelector = "" },
		"unknown selector": func(h *Hardware) { h.RootSelector = "unknown" },
		"missing proof":    func(h *Hardware) { h.RootDisk = nil },
		"invalid serial":   func(h *Hardware) { h.RootDisk.SerialNumber = "bad serial" },
		"invalid size":     func(h *Hardware) { h.RootDisk.SizeBytes = 8193 },
		"proof on WWN":     func(h *Hardware) { h.RootSelector = "wwn" },
	} {
		t.Run(name, func(t *testing.T) {
			catalog := physicalWithRoot(text("serialNumber", "0000123456"), field("sizeBytes", api.IntegerValue("8192")))
			_, install, _ := onlyRequests(t, catalog)
			corrupt(install.Nodes[0].Hardware)
			data, err := install.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeInstallRequest(data); err == nil {
				t.Fatal("an incomplete physical disk proof was accepted")
			}
		})
	}
}
