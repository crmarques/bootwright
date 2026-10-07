package infrastructureservices

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestTypedSelectionsAndEndpointNormalization(t *testing.T) {
	for _, kind := range []api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.Registry} {
		t.Run(string(kind), func(t *testing.T) {
			ref := "serverRef"
			if kind == api.Proxy {
				ref = "proxyRef"
			} else if kind == api.Registry {
				ref = "registryRef"
			}
			one := obj(kind, "one", m("management", "managed", "endpoints", list(m("name", "provisioning", "addressRef", "address"))))
			many := obj(kind, "many", m("management", "managed", "endpoints", list(m("name", "a"), m("name", "b"))))
			external := obj(kind, "external", m("management", "external"))
			c := api.NewCatalog([]api.Object{one, many, external})
			normalize := func(v api.Value) api.Value {
				switch kind {
				case api.Proxy:
					return NormalizeProxy(v, c)
				case api.Registry:
					return NormalizeRegistrySelection(v, c)
				default:
					return NormalizeServerSelections(list(v), c, kind).Items()[0]
				}
			}
			validate := func(v api.Value) []api.Issue {
				switch kind {
				case api.Proxy:
					return ValidateProxy(v, c, "$.selection", false)
				case api.Registry:
					return ValidateRegistrySelection(v, c, "$.selection")
				default:
					return ValidateServerSelections(list(v), c, kind, "$.selection")
				}
			}
			selection := m(ref, "one")
			resolved := normalize(selection)
			if resolved.Get("endpointRef").Text() != "provisioning" || selection.Has("endpointRef") {
				t.Fatal("endpoint normalization missing or mutated selection")
			}
			if resolved.Len() != 2 || !normalize(resolved).Equal(resolved) {
				t.Fatal("normalization copied connection facts or is not idempotent")
			}
			for _, valid := range []api.Value{selection, resolved, m(ref, "many", "endpointRef", "b"), m(ref, "external")} {
				if issues := validate(valid); len(issues) != 0 {
					t.Fatal(issues)
				}
			}
			for _, invalid := range []api.Value{m(), m(ref, "unknown"), m(ref, "many"), m(ref, "one", "endpointRef", "unknown"), m(ref, "external", "endpointRef", "x")} {
				if issues := validate(invalid); len(issues) == 0 {
					t.Fatalf("invalid selection admitted: %v", invalid)
				}
			}
		})
	}
}

func TestProxyDirectAndMachineBoundary(t *testing.T) {
	c := api.NewCatalog([]api.Object{obj(api.Proxy, "managed", m("management", "managed", "endpoints", list(m("name", "only")))), obj(api.Proxy, "external", m("management", "external"))})
	if direct := NormalizeProxy(api.Value{}, c); !direct.Equal(m("direct", m())) {
		t.Fatal("absent proxy did not default to direct")
	}
	if !NormalizeProxy(m(), c).Equal(m()) {
		t.Fatal("empty invalid choice normalized to direct")
	}
	for _, invalid := range []api.Value{m("direct", m(), "proxyRef", "external"), m("direct", m(), "noProxy", list()), m("direct", m(), "endpointRef", "only"), m("proxyRef", "managed")} {
		if issues := ValidateProxy(invalid, c, "$.proxy", true); len(issues) == 0 {
			t.Fatalf("invalid install proxy accepted: %v", invalid)
		}
	}
	choice := m("proxyRef", "external", "noProxy", api.StringList(".example.test", "192.0.2.0/24"))
	if issues := ValidateProxy(choice, c, "$.proxy", true); len(issues) != 0 {
		t.Fatal(issues)
	}
	if !NormalizeProxy(choice, c).Equal(choice) {
		t.Fatal("external proxy choice changed")
	}
}

func TestServerSelectionListsPreserveOrderAndEmptyIntent(t *testing.T) {
	c := api.NewCatalog([]api.Object{obj(api.NTPServer, "clock", m("management", "managed", "endpoints", list(m("name", "only")))), obj(api.NTPServer, "external", m("management", "external"))})
	if NormalizeServerSelections(api.Value{}, c, api.NTPServer).Present() {
		t.Fatal("absent NTP policy materialized")
	}
	if !NormalizeServerSelections(list(), c, api.NTPServer).Equal(list()) {
		t.Fatal("explicit empty NTP policy lost")
	}
	selection := list(m("serverRef", "clock"), m("serverRef", "external"))
	resolved := NormalizeServerSelections(selection, c, api.NTPServer)
	if resolved.Items()[0].Get("serverRef").Text() != "clock" || resolved.Items()[1].Get("serverRef").Text() != "external" {
		t.Fatal("selection order changed")
	}
	duplicate := list(m("serverRef", "clock"), m("serverRef", "clock", "endpointRef", "only"))
	if issues := ValidateServerSelections(duplicate, c, api.NTPServer, "$.ntp"); len(issues) != 1 {
		t.Fatalf("equivalent endpoint duplicate not rejected: %v", issues)
	}
}

func TestArtifactEndpointSelectionAndHTTPContent(t *testing.T) {
	service := obj(api.ArtifactServer, "artifact", m("management", "managed", "listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080")), m("name", "https", "protocol", "https", "port", api.IntegerValue("8443"))), "endpoints", list(m("name", "packages", "listenerRef", "http"), m("name", "media", "listenerRef", "https"))))
	external := obj(api.ArtifactServer, "external", m("management", "external", "endpoints", list(m("name", "media", "url", "https://artifacts.example.test"))))
	c := api.NewCatalog([]api.Object{service, external})
	selection := m("serverRef", "artifact", "endpointRef", "packages")
	if got, ok := ArtifactEndpoint(selection, c); !ok || got.Name() != "artifact" {
		t.Fatal("typed artifact reference not selected")
	}
	if issues := ValidateArtifactEndpoint(selection, c, "$.selection", true); len(issues) > 0 {
		t.Fatal(issues)
	}
	for _, invalid := range []api.Value{m("endpointRef", "packages"), m("serverRef", "artifact"), m("serverRef", "external", "endpointRef", "media"), m("serverRef", "artifact", "endpointRef", "media"), m("serverRef", "artifact", "endpointRef", "unknown")} {
		if issues := ValidateArtifactEndpoint(invalid, c, "$.selection", true); len(issues) == 0 {
			t.Fatalf("invalid artifact selection accepted: %v", invalid)
		}
	}
	if issues := ValidateArtifactEndpoint(selection, api.Catalog{}, "$.selection", false); len(issues) != 0 {
		t.Fatalf("an unresolved serverRef, which the schema reference check reports, was reported again: %v", issues)
	}
}
