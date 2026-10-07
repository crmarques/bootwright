package infrastructureservices

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
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

func TestManagedServicesAndPlacement(t *testing.T) {
	machine := obj(api.Machine, "host", m("capabilities", api.StringList("container-runtime"), "network", m("addresses", list(m("name", "fqdn", "address", "host.example.test"), m("name", "ip", "address", "192.0.2.10/24")))))
	c := api.NewCatalog([]api.Object{machine})
	endpoints := list(m("name", "ip", "addressRef", "ip"))
	services := map[api.Kind]api.Value{
		api.ArtifactServer: m("listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080"))), "endpoints", list(m("name", "packages", "listenerRef", "http", "addressRef", "fqdn"))),
		api.LoadBalancer:   m("implementation", "haproxy", "bindAddresses", list(m("address", "192.0.2.5"))),
		api.Proxy:          m("implementation", "squid", "endpoints", endpoints),
		api.DNSServer:      m("implementation", "dnsmasq", "endpoints", endpoints),
		api.NTPServer:      m("implementation", "chrony", "endpoints", endpoints),
		api.Registry:       m("implementation", "mirror-registry", "endpoints", endpoints),
	}
	for kind, spec := range services {
		t.Run(string(kind), func(t *testing.T) {
			service := obj(kind, "service", spec.With("management", api.StringValue("managed")).With("machineRef", api.StringValue("host")))
			normalized, _ := Normalize(service, c)
			if issues := Validate(normalized, c); len(issues) > 0 {
				t.Fatal(issues)
			}
			if normalized.Spec().Equal(service.Spec()) && kind != api.LoadBalancer {
				t.Fatal("managed service defaults missing")
			}
			if kind != api.NTPServer {
				badHost := machine.WithSpec(machine.Spec().Without("capabilities"))
				if issues := Validate(normalized, api.NewCatalog([]api.Object{badHost})); len(issues) == 0 {
					t.Fatal("runtime capability omitted")
				}
			}
		})
	}
}

func TestExternalServicesHaveNoManagedDefaults(t *testing.T) {
	services := map[api.Kind]api.Value{
		api.Proxy:          m("connection", m("httpProxy", "http://proxy.example.test:3128")),
		api.DNSServer:      m("address", "192.0.2.53"),
		api.NTPServer:      m("address", "ntp.example.test"),
		api.ArtifactServer: m("endpoints", list(m("name", "media", "url", "https://artifacts.example.test"))),
		api.Registry:       m("url", "registry.example.test"),
		api.LoadBalancer:   m("bindAddresses", list(m("address", "192.0.2.5"))),
	}
	for kind, spec := range services {
		t.Run(string(kind), func(t *testing.T) {
			service := obj(kind, "service", spec.With("management", api.StringValue("external")))
			normalized, _ := Normalize(service, api.Catalog{})
			if !normalized.Spec().Equal(service.Spec()) {
				t.Fatal("external service acquired managed defaults")
			}
			if issues := Validate(normalized, api.Catalog{}); len(issues) != 0 {
				t.Fatal(issues)
			}
			for _, field := range []string{"machineRef", "implementation", "image"} {
				bad := service.WithSpec(service.Spec().With(field, api.StringValue("forbidden")))
				if issues := ValidateAuthored(bad, api.Catalog{}); len(issues) == 0 {
					t.Fatalf("external %s accepted %s", kind, field)
				}
			}
		})
	}
}

func TestServiceTransportAndLocalReferences(t *testing.T) {
	host := obj(api.Machine, "host", m("capabilities", api.StringList("container-runtime"), "network", m("addresses", list(m("name", "fqdn", "address", "host.example.test")))))
	cases := []struct {
		name string
		kind api.Kind
		spec api.Value
	}{
		{"TLS missing", api.ArtifactServer, m("listeners", list(m("name", "https", "protocol", "https", "port", api.IntegerValue("8443"))))},
		{"HTTP TLS", api.ArtifactServer, m("tls", m("secretRef", "tls"), "listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080"))))},
		{"duplicate port", api.ArtifactServer, m("listeners", list(m("name", "a", "protocol", "http", "port", api.IntegerValue("8080")), m("name", "b", "protocol", "http", "port", api.IntegerValue("8080"))))},
		{"missing endpoint contact", api.Proxy, m("implementation", "squid", "endpoints", list(m("name", "external", "addressRef", "unknown")))},
		{"unnamed bind addresses", api.LoadBalancer, m("implementation", "haproxy", "bindAddresses", list(m("address", "192.0.2.1"), m("address", "192.0.2.2")))},
		{"DNS port", api.DNSServer, m("implementation", "dnsmasq", "port", api.IntegerValue("5353"))},
		{"empty image", api.Proxy, m("implementation", "squid", "image", m())},
		{"managed external URL", api.Registry, m("implementation", "mirror-registry", "url", "registry.example.test")},
		{"managed external endpoint", api.ArtifactServer, m("endpoints", list(m("name", "external", "url", "https://artifacts.example.test")))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := tc.spec.With("machineRef", api.StringValue("host")).With("management", api.StringValue("managed"))
			if issues := Validate(obj(tc.kind, "service", spec), api.NewCatalog([]api.Object{host})); len(issues) == 0 {
				t.Fatal("invalid service admitted")
			}
		})
	}
}

func TestRequiredModeFieldsAndPartialDefaults(t *testing.T) {
	for _, service := range []api.Object{
		obj(api.Proxy, "service", m("management", "managed")),
		obj(api.Proxy, "service", m("management", "external", "connection", m())),
		obj(api.DNSServer, "service", m("management", "external")),
		obj(api.NTPServer, "service", m("management", "external")),
		obj(api.Registry, "service", m("management", "external")),
		obj(api.ArtifactServer, "service", m("management", "external")),
	} {
		if issues := Validate(service, api.Catalog{}); len(issues) == 0 {
			t.Fatalf("incomplete service accepted: %v", service)
		}
		if issues := ValidatePartial(service, api.Catalog{}); len(issues) != 0 {
			t.Fatalf("partial default rejected: %v", issues)
		}
	}
	external := obj(api.ArtifactServer, "service", m("management", "external", "endpoints", list(m("name", "media", "listenerRef", "http", "addressRef", "fqdn"))))
	if issues := ValidateAuthored(external, api.Catalog{}); len(issues) == 0 {
		t.Fatal("mixed artifact modes accepted")
	}
}

func TestServiceIPNormalizationPreservesHostnamesOrderAndAuthoredIntent(t *testing.T) {
	cases := []struct {
		name string
		kind api.Kind
		spec api.Value
		want api.Value
	}{
		{"external DNS", api.DNSServer, m("management", "external", "address", "2001:0DB8:0000::0053"), m("management", "external", "address", "2001:db8::53")},
		{"external NTP IP", api.NTPServer, m("management", "external", "address", "2001:0DB8:0000::0123"), m("management", "external", "address", "2001:db8::123")},
		{"external NTP hostname", api.NTPServer, m("management", "external", "address", "clock.example.test"), m("management", "external", "address", "clock.example.test")},
		{"load balancer", api.LoadBalancer, m("management", "external", "bindAddresses", list(m("name", "api", "address", "2001:0DB8:0000::0001"), m("name", "ingress", "address", "192.0.2.4"))), m("management", "external", "bindAddresses", list(m("name", "api", "address", "2001:db8::1"), m("name", "ingress", "address", "192.0.2.4")))},
		{"managed DNS", api.DNSServer, m("management", "managed", "bindAddress", "2001:0DB8:0000::0053", "forwarders", api.StringList("2001:0DB8:0000::0001", "192.0.2.53")), m("management", "managed", "bindAddress", "2001:db8::53", "port", api.IntegerValue("53"), "forwarders", api.StringList("2001:db8::1", "192.0.2.53"))},
		{"managed NTP", api.NTPServer, m("management", "managed", "upstreamSources", api.StringList("clock.example.test", "2001:0DB8:0000::0123")), m("management", "managed", "bindAddress", "0.0.0.0", "port", api.IntegerValue("123"), "upstreamSources", api.StringList("clock.example.test", "2001:db8::123"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := obj(tc.kind, "service", tc.spec)
			got, issues := Normalize(o, api.Catalog{})
			if len(issues) != 0 || !got.Spec().Equal(tc.want) {
				t.Fatalf("canonical service mismatch: got %v, want %v, issues %v", got.Spec(), tc.want, issues)
			}
			if !o.Spec().Equal(tc.spec) {
				t.Fatal("normalization changed authored intent")
			}
			again, _ := Normalize(got, api.Catalog{})
			if !got.Spec().Equal(again.Spec()) {
				t.Fatal("canonicalization is not idempotent")
			}
		})
	}
}

func TestArtifactEndpointDefaultsRequireTheirMode(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		endpoint api.Value
	}{
		{"managed", m("name", "media", "listenerRef", "http", "addressRef", "fqdn")},
		{"external", m("name", "media", "url", "https://artifacts.example.test")},
	} {
		fragment := obj(api.ArtifactServer, "", m("endpoints", list(tc.endpoint)))
		issues := ValidatePartial(fragment, api.Catalog{})
		if len(issues) != 1 || issues[0].Field != "$.spec.management" || issues[0].Remediation == "" {
			t.Fatalf("ambiguous endpoint default needs actionable mode diagnostic: %v", issues)
		}
		fragment = fragment.WithSpec(fragment.Spec().With("management", api.StringValue(tc.mode)))
		if issues := ValidatePartial(fragment, api.Catalog{}); len(issues) != 0 {
			t.Fatalf("explicit %s endpoint defaults rejected: %v", tc.mode, issues)
		}
	}
	concrete := obj(api.ArtifactServer, "artifact", m("endpoints", list(m("name", "media", "url", "https://artifacts.example.test"))))
	if issues := ValidateAuthored(concrete, api.Catalog{}); len(issues) != 0 {
		t.Fatalf("concrete object must be able to inherit its management mode: %v", issues)
	}
}
