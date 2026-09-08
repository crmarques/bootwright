package infrastructureservices

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"testing"
)

func m(kv ...any) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i < len(kv); i += 2 {
		var v api.Value
		switch x := kv[i+1].(type) {
		case api.Value:
			v = x
		case string:
			v = api.StringValue(x)
		case bool:
			v = api.BoolValue(x)
		}
		fields = append(fields, api.FieldValue{Name: kv[i].(string), Value: v})
	}
	return api.MapValue(fields...)
}
func obj(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, m(), spec)
}
func list(v ...api.Value) api.Value { return api.ListValue(v...) }
func TestAllComponentArmsAndPlacement(t *testing.T) {
	machine := obj(api.Machine, "host", m("capabilities", api.StringList("container-runtime"), "network", m("addresses", list(m("name", "fqdn", "address", "host.example.test")))))
	c := api.NewCatalog([]api.Object{machine})
	arms := map[string]api.Value{
		"artifactServer": m("machineRef", "host", "listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080"))), "endpoints", list(m("name", "packages", "listenerRef", "http", "addressRef", "fqdn"))),
		"loadBalancer":   m("implementation", "haproxy", "machineRef", "host", "bindAddresses", list(m("address", "192.0.2.5"))),
		"proxy":          m("implementation", "squid", "machineRef", "host"),
		"nameResolution": m("implementation", "dnsmasq", "machineRef", "host", "port", api.IntegerValue("53")),
		"ntp":            m("implementation", "chrony", "machineRef", "host"),
		"registry":       m("implementation", "mirror-registry", "machineRef", "host"),
	}
	for arm, value := range arms {
		t.Run(arm, func(t *testing.T) {
			component := obj(api.InfraComponent, arm, m(arm, value))
			if issues := Validate(component, c); len(issues) > 0 {
				t.Fatal(issues)
			}
			if arm != "ntp" {
				badHost := machine.WithSpec(machine.Spec().Without("capabilities"))
				if issues := Validate(component, api.NewCatalog([]api.Object{badHost})); len(issues) == 0 {
					t.Fatal("runtime capability omitted")
				}
			}
		})
	}
}
func TestComponentTransportAndLocalReferences(t *testing.T) {
	host := obj(api.Machine, "host", m("capabilities", api.StringList("container-runtime"), "network", m("addresses", list(m("name", "fqdn", "address", "host.example.test")))))
	cases := map[string]api.Value{
		"TLS missing":              m("artifactServer", m("machineRef", "host", "listeners", list(m("name", "https", "protocol", "https", "port", api.IntegerValue("8443"))))),
		"HTTP TLS":                 m("artifactServer", m("machineRef", "host", "tls", m("secretRef", "tls"), "listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080"))))),
		"duplicate port":           m("artifactServer", m("machineRef", "host", "listeners", list(m("name", "a", "protocol", "http", "port", api.IntegerValue("8080")), m("name", "b", "protocol", "http", "port", api.IntegerValue("8080"))))),
		"missing endpoint contact": m("proxy", m("machineRef", "host", "endpoints", list(m("name", "external", "addressRef", "unknown")))),
		"unnamed bind addresses":   m("loadBalancer", m("machineRef", "host", "bindAddresses", list(m("address", "192.0.2.1"), m("address", "192.0.2.2")))),
		"DNS port":                 m("nameResolution", m("machineRef", "host", "port", api.IntegerValue("5353"))),
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if issues := Validate(obj(api.InfraComponent, "service", spec), api.NewCatalog([]api.Object{host})); len(issues) == 0 {
				t.Fatal("invalid component admitted")
			}
		})
	}
}
func TestArtifactEndpointSelectionAndHTTPContent(t *testing.T) {
	component := obj(api.InfraComponent, "artifact", m("artifactServer", m("listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080")), m("name", "https", "protocol", "https", "port", api.IntegerValue("8443"))), "endpoints", list(m("name", "packages", "listenerRef", "http"), m("name", "media", "listenerRef", "https")))))
	env := obj(api.Environment, "env", m("infraComponents", m("artifactServers", list(m("name", "primary", "management", "managed", "componentRef", "artifact")))))
	c := api.NewCatalog([]api.Object{component, env})
	selection := m("endpointRef", "packages")
	row, _, ok := ArtifactEndpoint(selection, c)
	if !ok || row.Get("name").Text() != "primary" {
		t.Fatal("sole catalog entry not selected")
	}
	if issues := ValidateArtifactEndpoint(selection, c, "$.selection", true); len(issues) > 0 {
		t.Fatal(issues)
	}
	if issues := ValidateArtifactEndpoint(m("endpointRef", "media"), c, "$.selection", true); len(issues) == 0 {
		t.Fatal("HTTPS hosted content accepted")
	}
	if issues := ValidateArtifactEndpoint(m("endpointRef", "unknown"), c, "$.selection", false); len(issues) == 0 {
		t.Fatal("missing nested endpoint accepted")
	}
	if issues := ValidateArtifactEndpoint(selection, api.Catalog{}, "$.selection", false); len(issues) != 0 {
		t.Fatal("missing Environment cascaded")
	}
}
