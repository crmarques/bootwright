package infrastructureservices

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestDNSForwardZonesAdmission(t *testing.T) {
	field, ok := api.Schema(api.DNSServer).Field("forwardZones")
	if !ok || field.Shape == nil {
		t.Fatal("DNSServer cannot declare conditional forwarding")
	}
	zone := m("domain", "sno.example.test", "forwarders", api.StringList("192.0.2.10"))
	valid := m("management", "managed", "machineRef", "host", "implementation", "dnsmasq", "forwardZones", list(zone))
	if issues := ValidateAuthored(obj(api.DNSServer, "dns", valid), api.Catalog{}); len(issues) != 0 {
		t.Fatal(issues)
	}
	for name, spec := range map[string]api.Value{
		"external":          m("management", "external", "address", "192.0.2.1", "forwardZones", list(zone)),
		"duplicate domains": valid.With("forwardZones", list(zone, zone)),
		"empty forwarders":  valid.With("forwardZones", list(zone.With("forwarders", api.ListValue()))),
		"own listener":      valid.With("bindAddress", api.StringValue("192.0.2.10")),
	} {
		t.Run(name, func(t *testing.T) {
			if issues := ValidateAuthored(obj(api.DNSServer, "dns", spec), api.Catalog{}); len(issues) == 0 {
				t.Fatal("invalid forwarding accepted")
			}
		})
	}
}

func TestDNSForwardZonesNormalizeAddresses(t *testing.T) {
	zone := m("domain", "sno.example.test", "forwarders", api.StringList("2001:0db8:0:0::53"))
	original := obj(api.DNSServer, "dns", m("management", "managed", "forwardZones", list(zone)))
	normalized, _ := Normalize(original, api.Catalog{})
	if got := normalized.Spec().Get("forwardZones").Items()[0].Get("forwarders").Items()[0].Text(); got != "2001:db8::53" {
		t.Fatalf("forwarder = %q", got)
	}
}

func TestDNSLocalZonesRefuseForwardingBackOut(t *testing.T) {
	env := obj(api.Environment, "lab", m("domains", m("containerClusters", "example.test")))
	cluster := obj(api.OpenShiftCluster, "sno", m())
	catalog := api.NewCatalog([]api.Object{cluster, env})
	for _, domain := range []string{"sno.example.test", "child.sno.example.test"} {
		value := m("forwardZones", list(m("domain", domain, "forwarders", api.StringList("192.0.2.1"))))
		if issues := validateLocalDNSZones(value, catalog); len(issues) != 1 {
			t.Fatalf("local forwarding %s accepted: %v", domain, issues)
		}
	}
	value := m("forwardZones", list(m("domain", "example.test", "forwarders", api.StringList("192.0.2.1"))))
	if issues := validateLocalDNSZones(value, catalog); len(issues) != 0 {
		t.Fatalf("broader forwarding refused: %v", issues)
	}
	if zones := LocalDNSZones(api.NewCatalog([]api.Object{env})); len(zones) != 0 {
		t.Fatalf("bootstrap DNS owns future cluster zones: %v", zones)
	}
}
