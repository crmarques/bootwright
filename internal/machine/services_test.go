package machine

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInstallServiceProfileInheritanceAndOverrides(t *testing.T) {
	proxy := object(api.Proxy, "egress", m("management", "external", "connection", m("httpProxy", "http://proxy.example.test:3128")))
	ntp := object(api.NTPServer, "clock", m("management", "managed", "endpoints", list(m("name", "installation", "addressRef", "primary"))))
	profile := object(api.MachineInstallProfile, "install", m("proxy", m("proxyRef", "egress", "noProxy", api.StringList(".example.test")), "ntp", list(m("serverRef", "clock"))))
	c := api.NewCatalog([]api.Object{proxy, ntp, profile})
	original := object(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "install")))
	inherited, issues := Normalize(original, c)
	if len(issues) != 0 {
		t.Fatal(issues)
	}
	if !inherited.Spec().Get("proxy").Equal(profile.Spec().Get("proxy")) || inherited.Spec().Get("os", "install", "ntp").Items()[0].Get("endpointRef").Text() != "installation" {
		t.Fatal("profile policy or endpoint choice not inherited")
	}
	if issues := validateServices(inherited, c); len(issues) != 0 {
		t.Fatal(issues)
	}
	if original.Spec().Has("proxy") || original.Spec().Has("os", "install") || profile.Spec().Get("ntp").Items()[0].Has("endpointRef") {
		t.Fatal("normalization mutated declarations")
	}
	origins := NormalizationOrigins(original, inherited, c)
	if len(origins) != 2 || origins[0].Field != "$.spec.proxy" || origins[0].SourceName != "install" || origins[0].SourceField != "$.spec.proxy" || origins[1].Field != "$.spec.os.install.ntp" || origins[1].SourceField != "$.spec.ntp" {
		t.Fatal(origins)
	}
	explicit := original.WithSpec(original.Spec().With("proxy", m("direct", m())).WithPath(list(), "os", "install", "ntp"))
	normalized, _ := Normalize(explicit, c)
	if !normalized.Spec().Get("proxy").Equal(m("direct", m())) || normalized.Spec().Get("os", "install", "ntp").Len() != 0 {
		t.Fatal("explicit policy did not replace whole profile choice")
	}
	if origins := NormalizationOrigins(explicit, normalized, c); len(origins) != 0 {
		t.Fatal("explicit overrides attributed to profile", origins)
	}
	noProfile, _ := Normalize(original, api.Catalog{})
	if !noProfile.Spec().Get("proxy").Equal(m("direct", m())) || noProfile.Spec().Has("os", "install") {
		t.Fatal("omitted choices did not retain native defaults")
	}
	if origins := NormalizationOrigins(original, noProfile, api.Catalog{}); len(origins) != 0 {
		t.Fatal("intrinsic choice attributed to profile")
	}
}

func TestMachineProxyFollowsOSLifecycle(t *testing.T) {
	managed := object(api.Proxy, "managed", m("management", "managed", "endpoints", list(m("name", "installation", "addressRef", "primary"))))
	external := object(api.Proxy, "external", m("management", "external", "connection", m("httpsProxy", "http://proxy.example.test:3128")))
	c := api.NewCatalog([]api.Object{managed, external})
	installed := object(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "install"), "proxy", m("proxyRef", "managed", "endpointRef", "installation")))
	if issues := validateServices(installed, c); len(issues) == 0 || issues[0].Field != "$.spec.proxy.proxyRef" {
		t.Fatal("managed install proxy admitted")
	}
	for _, proxyRef := range []string{"managed", "external"} {
		provided := object(api.Machine, "ready", m("os", m("provided", true), "proxy", m("proxyRef", proxyRef)))
		if issues := ValidateAuthored(provided, c); len(issues) != 0 {
			t.Fatal("OS-ready Machine proxy rejected before normalization", issues)
		}
		effective, _ := Normalize(provided, c)
		if issues := validateServices(effective, c); len(issues) != 0 {
			t.Fatal("OS-ready Machine proxy rejected", issues)
		}
		if got := effective.Spec().Get("proxy"); got.Get("proxyRef").Text() != proxyRef || got.Has("connection") || got.Has("endpointRef") != (proxyRef == "managed") {
			t.Fatal("Machine proxy lost its selection or copied service connection facts")
		}
		if provided.Spec().Get("proxy").Has("endpointRef") || effective.Spec().Has("os", "install") {
			t.Fatal("OS-ready proxy normalization changed authored policy or installation intent")
		}
	}
	provided, _ := Normalize(object(api.Machine, "ready", m("os", m("provided", true))), c)
	if !provided.Spec().Get("proxy").Equal(m("direct", m())) {
		t.Fatal("OS-ready Machine did not default to direct access")
	}
	downstream := object(api.Machine, "node", m("os", m("provided", false)))
	effective, _ := Normalize(downstream, c)
	if effective.Spec().Has("proxy") || effective.Spec().Has("os", "install") {
		t.Fatal("downstream-installer Machine received independent host policy")
	}
	for _, choice := range []api.Value{m("direct", m()), m("proxyRef", "external"), m("proxyRef", "managed")} {
		declared := downstream.WithSpec(downstream.Spec().With("proxy", choice))
		if issues := ValidateAuthored(declared, c); len(issues) != 1 || issues[0].Field != "$.spec.proxy" {
			t.Fatal("downstream-installer Machine accepted a proxy choice", issues)
		}
	}
	for _, provided := range []bool{true, false} {
		o := object(api.Machine, "node", m("os", m("provided", provided, "install", m("ntp", list()))))
		if issues := ValidateAuthored(o, c); len(issues) != 1 || issues[0].Field != "$.spec.os.install.ntp" {
			t.Fatal("inapplicable installation NTP policy accepted", issues)
		}
	}
}

func TestAuthoredInstallServicesDeferCatalogChecks(t *testing.T) {
	managed := object(api.Proxy, "egress", m("management", "managed", "endpoints", list(m("name", "valid"))))
	clock := object(api.NTPServer, "clock", m("management", "managed", "endpoints", list(m("name", "valid"))))
	catalog := api.NewCatalog([]api.Object{managed, clock})
	for _, selection := range []api.Value{
		m("proxy", m("proxyRef", "missing"), "ntp", list(m("serverRef", "missing"))),
		m("proxy", m("proxyRef", "egress", "endpointRef", "missing"), "ntp", list(m("serverRef", "clock", "endpointRef", "missing"))),
	} {
		o := object(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "install", "install", m("ntp", selection.Get("ntp"))), "proxy", selection.Get("proxy")))
		if issues := ValidateAuthored(o, catalog); len(issues) != 0 {
			t.Fatal("authored phase resolved a service or endpoint", issues)
		}
		if issues := validateServices(o, catalog); len(issues) == 0 {
			t.Fatal("effective phase omitted service validation")
		}
	}
}

func TestMachineProxyDefaultsValidateOnlyKnownContradictions(t *testing.T) {
	for _, choice := range []api.Value{
		m("direct", m(), "proxyRef", "missing"),
		m("direct", m(), "noProxy", api.StringList("example.test")),
		m("direct", m(), "endpointRef", "missing"),
	} {
		o := object(api.Machine, "node", m("os", m("provided", true), "proxy", choice))
		for _, validate := range []func(api.Object, api.Catalog) []api.Issue{ValidatePartial, ValidateAuthored} {
			if issues := validate(o, api.Catalog{}); len(issues) != 1 || issues[0].Field != "$.spec.proxy" {
				t.Fatal("contradictory proxy choice was accepted or mislocated", issues)
			}
		}
	}
	for _, os := range []api.Value{m(), m("provided", true), m("provided", false)} {
		partial := object(api.Machine, "", m("os", os, "proxy", m("proxyRef", "missing", "endpointRef", "missing")))
		if issues := ValidatePartial(partial, api.Catalog{}); len(issues) != 0 {
			t.Fatal("partial defaults inferred absent lifecycle fields or resolved references", issues)
		}
	}
}

func TestDNSSelectionsPreserveOrderAndCountDistinctManagedServers(t *testing.T) {
	primary := object(api.DNSServer, "primary", m("management", "managed", "endpoints", list(m("name", "a"), m("name", "b"))))
	secondary := object(api.DNSServer, "secondary", m("management", "managed", "endpoints", list(m("name", "sole"))))
	external := object(api.DNSServer, "external", m("management", "external", "address", "192.0.2.53"))
	c := api.NewCatalog([]api.Object{primary, secondary, external})
	valid := m("dns", list(m("serverRef", "external"), m("serverRef", "primary", "endpointRef", "b"), m("serverRef", "primary", "endpointRef", "a")))
	effective := normalizeConfiguration(valid, c)
	if !effective.Equal(valid) {
		t.Fatal("DNS order or typed refs changed")
	}
	if issues := validateConfiguration(effective, c, "$.spec"); len(issues) != 0 {
		t.Fatal(issues)
	}
	for name, selections := range map[string]api.Value{
		"two managed servers": list(m("serverRef", "primary", "endpointRef", "a"), m("serverRef", "secondary")),
		"ambiguous endpoint":  list(m("serverRef", "primary")),
		"external endpoint":   list(m("serverRef", "external", "endpointRef", "invalid")),
		"repeated choice":     list(m("serverRef", "external"), m("serverRef", "external")),
		"missing server":      list(m("serverRef", "missing")),
	} {
		t.Run(name, func(t *testing.T) {
			if issues := validateConfiguration(normalizeConfiguration(m("dns", selections), c), c, "$.spec"); len(issues) == 0 {
				t.Fatal("invalid DNS selection admitted")
			}
		})
	}
	inline := object(api.Machine, "node", m("network", m("inline", m("dns", list(m("serverRef", "secondary"))))))
	inline, _ = Normalize(inline, c)
	if inline.Spec().Get("network", "inline", "dns").Items()[0].Get("endpointRef").Text() != "sole" {
		t.Fatal("inline endpoint not materialized")
	}
	for _, key := range []string{"nameResolutionRefs", "dns"} {
		if issues := validateNative(m(key, list()), "$.spec.nmstate", false); len(issues) == 0 {
			t.Fatal("Bootwright service policy admitted as native NMState")
		}
	}
}
