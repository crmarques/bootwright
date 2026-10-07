package infrastructureservices

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const consumerDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func consumerHost(provided bool, addresses ...api.Value) api.Object {
	return obj(api.Machine, "host", m("capabilities", api.StringList("container-runtime"), "os", m("provided", provided), "network", m("addresses", list(addresses...))))
}

func consumerAddresses() []api.Value {
	return []api.Value{
		m("name", "fqdn", "address", "host.example.test"),
		m("name", "ip", "address", "192.0.2.10/24"),
		m("name", "other", "address", "192.0.2.11/24"),
		m("name", "v6", "address", "2001:0DB8:0:0::0010/64"),
	}
}

func managedService(kind api.Kind, spec api.Value) api.Object {
	return obj(kind, "service", spec.With("management", api.StringValue("managed")).With("machineRef", api.StringValue("host")))
}

func consumerSpec(kind api.Kind, endpoints ...api.Value) api.Value {
	switch kind {
	case api.ArtifactServer:
		for index, endpoint := range endpoints {
			endpoints[index] = endpoint.With("listenerRef", api.StringValue("http"))
		}
		return m("listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080"))), "endpoints", list(endpoints...))
	case api.LoadBalancer:
		return m("implementation", "haproxy", "bindAddresses", list(m("address", "192.0.2.5")))
	}
	implementation := map[api.Kind]string{api.Proxy: "squid", api.DNSServer: "dnsmasq", api.NTPServer: "chrony", api.Registry: "mirror-registry"}[kind]
	return m("implementation", implementation, "endpoints", list(endpoints...))
}

func admit(o api.Object, objects ...api.Object) []api.Issue {
	c := api.NewCatalog(objects)
	normalized, _ := Normalize(o, c)
	return Validate(normalized, c)
}

func refusalsAt(issues []api.Issue, field string) []api.Issue {
	found := []api.Issue{}
	for _, issue := range issues {
		if issue.Field == field {
			found = append(found, issue)
		}
	}
	return found
}

func mentionsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func TestADNSServerEndpointNamesAnIPAddressOnItsMachine(t *testing.T) {
	host := consumerHost(true, consumerAddresses()...)
	named := managedService(api.DNSServer, consumerSpec(api.DNSServer, m("name", "resolver", "addressRef", "fqdn")))
	found := refusalsAt(admit(named, host), "$.spec.endpoints[0].addressRef")
	if len(found) != 1 || found[0].Code != "api.invariant" || !mentionsAll(found[0].Message, "fqdn", "Machine/host", "host.example.test") ||
		!mentionsAll(found[0].Remediation, "addressRef", "spec.network.addresses", "Machine/host") {
		t.Fatalf("a DNSServer endpoint on a DNS name = %#v, want one refusal naming the address and its Machine", found)
	}
	addressed := managedService(api.DNSServer, consumerSpec(api.DNSServer, m("name", "resolver", "addressRef", "ip")))
	if issues := admit(addressed, host); len(issues) != 0 {
		t.Fatalf("a DNSServer endpoint on an ip/prefix address was refused: %v", issues)
	}
}

func TestANonWildcardBindEqualsEveryIPEndpoint(t *testing.T) {
	host := consumerHost(true, consumerAddresses()...)
	for _, kind := range []api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.ArtifactServer} {
		t.Run(string(kind), func(t *testing.T) {
			service := func(bind, addressRef string) api.Object {
				return managedService(kind, consumerSpec(kind, m("name", "contact", "addressRef", addressRef)).With("bindAddress", api.StringValue(bind)))
			}
			found := refusalsAt(admit(service("192.0.2.10", "other"), host), "$.spec.endpoints[0].addressRef")
			if len(found) != 1 || !mentionsAll(found[0].Message, "contact", "192.0.2.11", "spec.bindAddress 192.0.2.10") ||
				!mentionsAll(found[0].Remediation, "spec.bindAddress", "192.0.2.11", "addressRef", "192.0.2.10") {
				t.Fatalf("an endpoint off the bind address = %#v, want one refusal naming both addresses", found)
			}
			admitted := map[string]api.Object{
				"equal address":             service("192.0.2.10", "ip"),
				"non-canonical IPv6 equal":  service("2001:db8::10", "v6"),
				"IPv6 bind written loosely": service("2001:0DB8::0010", "v6"),
			}
			if kind != api.DNSServer {
				admitted["DNS-name endpoint"] = service("192.0.2.10", "fqdn")
			}
			for name, o := range admitted {
				if issues := admit(o, host); len(issues) != 0 {
					t.Errorf("%s was refused: %v", name, issues)
				}
			}
		})
	}
}

func TestNTPServerListensOnPort123(t *testing.T) {
	host := consumerHost(true, consumerAddresses()...)
	spec := consumerSpec(api.NTPServer, m("name", "clock", "addressRef", "ip"))
	moved := managedService(api.NTPServer, spec.With("port", api.IntegerValue("1123")))
	found := refusalsAt(admit(moved, host), "$.spec.port")
	if len(found) != 1 || !strings.Contains(found[0].Message, "port 123") || found[0].Remediation != "set spec.port to 123 or omit it" {
		t.Fatalf("NTPServer on port 1123 = %#v, want one refusal at $.spec.port", found)
	}
	partial := refusalsAt(ValidatePartial(obj(api.NTPServer, "", m("port", api.IntegerValue("1123"))), api.Catalog{}), "$.spec.port")
	if len(partial) != 1 {
		t.Fatalf("an NTPServer kind default on port 1123 = %#v, want one refusal at $.spec.port", partial)
	}
	for name, o := range map[string]api.Object{"default": managedService(api.NTPServer, spec), "explicit 123": managedService(api.NTPServer, spec.With("port", api.IntegerValue("123")))} {
		if issues := admit(o, host); len(issues) != 0 {
			t.Errorf("%s port was refused: %v", name, issues)
		}
	}
	if issues := ValidatePartial(obj(api.NTPServer, "", m("port", api.IntegerValue("123"))), api.Catalog{}); len(issues) != 0 {
		t.Fatalf("an NTPServer kind default on port 123 was refused: %v", issues)
	}
}

func TestAWildcardBindNeedsAnEndpointPerServiceAndListener(t *testing.T) {
	host := consumerHost(true, consumerAddresses()...)
	for _, kind := range []api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.Registry} {
		t.Run(string(kind), func(t *testing.T) {
			for name, spec := range map[string]api.Value{"absent": consumerSpec(kind).Without("endpoints"), "empty": consumerSpec(kind)} {
				found := refusalsAt(admit(managedService(kind, spec), host), "$.spec.endpoints")
				if len(found) != 1 || !strings.Contains(found[0].Message, "wildcard 0.0.0.0") || !mentionsAll(found[0].Remediation, "spec.endpoints", "Machine/host", "spec.bindAddress") {
					t.Errorf("%s endpoints under the default wildcard bind = %#v, want one refusal at $.spec.endpoints", name, found)
				}
			}
			bound := managedService(kind, consumerSpec(kind).Without("endpoints").With("bindAddress", api.StringValue("192.0.2.10")))
			if issues := admit(bound, host); len(issues) != 0 {
				t.Fatalf("a service bound to one address needs no endpoint, but was refused: %v", issues)
			}
		})
	}
	listeners := list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080")), m("name", "spare", "protocol", "http", "port", api.IntegerValue("8081")))
	artifacts := managedService(api.ArtifactServer, m("bindAddress", "::", "listeners", listeners, "endpoints", list(m("name", "packages", "listenerRef", "http", "addressRef", "ip"))))
	found := refusalsAt(admit(artifacts, host), "$.spec.listeners[1]")
	if len(found) != 1 || !mentionsAll(found[0].Message, "spare", "wildcard bind ::") || !mentionsAll(found[0].Remediation, "listenerRef: spare", "spec.bindAddress", "Machine/host") {
		t.Fatalf("a listener no endpoint names under :: = %#v, want one refusal at $.spec.listeners[1]", found)
	}
	if len(refusalsAt(admit(artifacts, host), "$.spec.listeners[0]")) != 0 {
		t.Fatal("a listener an endpoint names was refused")
	}
	defaulted := managedService(api.ArtifactServer, m("tls", m("secretRef", "serving")))
	found = refusalsAt(admit(defaulted, host), "$.spec.listeners[0]")
	if len(found) != 1 || !strings.Contains(found[0].Message, "listener https") {
		t.Fatalf("the default listener without an endpoint = %#v, want one refusal at $.spec.listeners[0]", found)
	}
	if issues := admit(artifacts.WithSpec(artifacts.Spec().With("bindAddress", api.StringValue("192.0.2.10"))), host); len(issues) != 0 {
		t.Fatalf("a listener without an endpoint under a single-address bind was refused: %v", issues)
	}
}

func TestAManagedServiceRunsOnlyWhereItsMachineProvidesTheOS(t *testing.T) {
	for _, kind := range []api.Kind{api.Proxy, api.DNSServer, api.NTPServer, api.Registry, api.ArtifactServer, api.LoadBalancer} {
		t.Run(string(kind), func(t *testing.T) {
			service := managedService(kind, consumerSpec(kind, m("name", "contact", "addressRef", "ip")))
			found := refusalsAt(admit(service, consumerHost(false, consumerAddresses()...)), "$.spec.machineRef")
			if len(found) != 1 || !mentionsAll(found[0].Message, "Machine/host", "os.provided: false") || found[0].Remediation != "set spec.machineRef to a Machine with os.provided: true" {
				t.Fatalf("placement on a Machine without its OS = %#v, want one refusal at $.spec.machineRef", found)
			}
			if issues := admit(service, consumerHost(true, consumerAddresses()...)); len(issues) != 0 {
				t.Fatalf("placement on a Machine that provides its OS was refused: %v", issues)
			}
			undeclared := obj(api.Machine, "host", m("capabilities", api.StringList("container-runtime"), "network", m("addresses", list(consumerAddresses()...))))
			if found := refusalsAt(admit(service, undeclared), "$.spec.machineRef"); len(found) != 0 {
				t.Fatalf("a Machine the schema refuses for its missing os.provided got a second diagnostic: %v", found)
			}
		})
	}
}

func TestServiceImagePinsRequireADigest(t *testing.T) {
	host := consumerHost(true, consumerAddresses()...)
	spec := consumerSpec(api.Proxy, m("name", "egress", "addressRef", "ip"))
	for _, field := range []string{"local", "public"} {
		pinned := managedService(api.Proxy, spec.With("image", m(field, "registry.example.test/squid:6.10")))
		for name, issues := range map[string][]api.Issue{"Validate": admit(pinned, host), "ValidateAuthored": ValidateAuthored(pinned, api.Catalog{})} {
			found := refusalsAt(issues, "$.spec.image."+field)
			if len(found) != 1 || !strings.Contains(found[0].Message, "content digest") || found[0].Remediation != "write spec.image."+field+" as <repository>@sha256:<64 hex digits>" {
				t.Errorf("%s of a %s tag = %#v, want one refusal at $.spec.image.%s", name, field, found, field)
			}
		}
		partial := refusalsAt(ValidatePartial(obj(api.Proxy, "", m("image", m(field, "registry.example.test/squid:6.10"))), api.Catalog{}), "$.spec.image."+field)
		if len(partial) != 1 {
			t.Errorf("a %s tag in kind defaults = %#v, want one refusal at $.spec.image.%s", field, partial, field)
		}
		malformed := managedService(api.Proxy, spec.With("image", m(field, "registry.example.test/Squid:6.10")))
		if found := refusalsAt(admit(malformed, host), "$.spec.image."+field); len(found) != 0 {
			t.Errorf("a %s value outside the image grammar got the digest diagnostic as well as the grammar's: %v", field, found)
		}
	}
	upper := "registry.example.test/squid@SHA256:" + strings.ToUpper(strings.TrimPrefix(consumerDigest, "sha256:"))
	normalized, _ := Normalize(managedService(api.Proxy, spec.With("image", m("public", upper, "local", "registry.example.test/squid@"+consumerDigest))), api.Catalog{})
	if got := normalized.Spec().Get("image", "public").Text(); got != "registry.example.test/squid@"+consumerDigest {
		t.Fatalf("an uppercase digest normalized to %q, want it lowercased", got)
	}
	if issues := admit(normalized, host); len(issues) != 0 {
		t.Fatalf("digest pins were refused: %v", issues)
	}
}

func TestServiceAdmissionRemediesAreExact(t *testing.T) {
	host := consumerHost(true, consumerAddresses()...)
	unprovided := consumerHost(false, consumerAddresses()...)
	proxy := consumerSpec(api.Proxy, m("name", "egress", "addressRef", "ip"))
	cases := []struct {
		name, field, remedy string
		issues              []api.Issue
	}{
		{"implementation", "$.spec.implementation", "set spec.implementation: squid", Validate(managedService(api.Proxy, proxy.Without("implementation")), api.NewCatalog([]api.Object{host}))},
		{"forbidden field", "$.spec.url", "remove spec.url, which management: managed does not use", ValidateAuthored(managedService(api.Registry, consumerSpec(api.Registry).With("url", api.StringValue("registry.example.test"))), api.Catalog{})},
		{"external proxy URL", "$.spec.connection", "spec.connection.httpProxy", Validate(obj(api.Proxy, "service", m("management", "external", "connection", m())), api.Catalog{})},
		{"empty image", "$.spec.image", "spec.image.local", Validate(managedService(api.Proxy, proxy.With("image", m())), api.NewCatalog([]api.Object{host}))},
		{"DNS port", "$.spec.port", "set spec.port to 53 or omit it", ValidatePartial(obj(api.DNSServer, "", m("port", api.IntegerValue("5353"))), api.Catalog{})},
		{"NTP port", "$.spec.port", "spec.port", ValidatePartial(obj(api.NTPServer, "", m("port", api.IntegerValue("1123"))), api.Catalog{})},
		{"capability", "$.spec.machineRef", "container-runtime", admit(managedService(api.Proxy, proxy), obj(api.Machine, "host", m("os", m("provided", true), "network", m("addresses", list(consumerAddresses()...)))))},
		{"OS", "$.spec.machineRef", "spec.machineRef", admit(managedService(api.Proxy, proxy), unprovided)},
		{"unknown address", "$.spec.endpoints[0].addressRef", "spec.network.addresses", admit(managedService(api.Proxy, consumerSpec(api.Proxy, m("name", "egress", "addressRef", "missing"))), host)},
		{"DNS name", "$.spec.endpoints[0].addressRef", "addressRef", admit(managedService(api.DNSServer, consumerSpec(api.DNSServer, m("name", "resolver", "addressRef", "fqdn"))), host)},
		{"bind", "$.spec.endpoints[0].addressRef", "spec.bindAddress", admit(managedService(api.Proxy, proxy.With("bindAddress", api.StringValue("192.0.2.11"))), host)},
		{"wildcard", "$.spec.endpoints", "spec.endpoints", admit(managedService(api.Proxy, consumerSpec(api.Proxy)), host)},
		{"listener", "$.spec.listeners[0]", "listenerRef: https", admit(managedService(api.ArtifactServer, m("tls", m("secretRef", "serving"))), host)},
		{"image tag", "$.spec.image.public", "spec.image.public", ValidatePartial(obj(api.Proxy, "", m("image", m("public", "registry.example.test/squid:6.10"))), api.Catalog{})},
		{"listener reference", "$.spec.endpoints[0].listenerRef", "spec.listeners", admit(managedService(api.ArtifactServer, m("bindAddress", "192.0.2.10", "listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080"))), "endpoints", list(m("name", "packages", "listenerRef", "missing", "addressRef", "ip")))), host)},
		{"duplicate port", "$.spec.listeners[1].port", "spec.listeners[1]", ValidateAuthored(managedService(api.ArtifactServer, m("listeners", list(m("name", "a", "protocol", "http", "port", api.IntegerValue("8080")), m("name", "b", "protocol", "http", "port", api.IntegerValue("8080"))))), api.Catalog{})},
		{"missing TLS", "$.spec.tls", "spec.tls.secretRef", Validate(managedService(api.ArtifactServer, m("bindAddress", "192.0.2.10", "listeners", list(m("name", "https", "protocol", "https", "port", api.IntegerValue("8443"))))), api.NewCatalog([]api.Object{host}))},
		{"HTTP TLS", "$.spec.tls", "remove spec.tls", ValidateAuthored(managedService(api.ArtifactServer, m("tls", m("secretRef", "serving"), "listeners", list(m("name", "http", "protocol", "http", "port", api.IntegerValue("8080"))))), api.Catalog{})},
		{"endpoint field", "$.spec.endpoints[0].url", "remove spec.endpoints[0].url", ValidateAuthored(managedService(api.ArtifactServer, m("endpoints", list(m("name", "media", "url", "https://artifacts.example.test")))), api.Catalog{})},
		{"bind-address names", "$.spec.bindAddresses[1].name", "spec.bindAddresses[1]", ValidateAuthored(obj(api.LoadBalancer, "service", m("management", "external", "bindAddresses", list(m("name", "a", "address", "192.0.2.1"), m("name", "a", "address", "192.0.2.2")))), api.Catalog{})},
	}
	for _, tc := range cases {
		found := refusalsAt(tc.issues, tc.field)
		if len(found) == 0 || !strings.Contains(found[0].Remediation, tc.remedy) {
			t.Errorf("%s: refusals at %s = %#v, want a remediation naming %q", tc.name, tc.field, found, tc.remedy)
		}
		for _, issue := range tc.issues {
			if issue.Remediation == "" || strings.Contains(issue.Remediation, "consistent with its typed references") {
				t.Errorf("%s: %s carries no exact remediation: %q", tc.name, issue.Field, issue.Remediation)
			}
		}
	}
}

func TestAMistypedServerRefIsOnlyAReferenceProblem(t *testing.T) {
	external := obj(api.ArtifactServer, "mirror", m("management", "external", "endpoints", list(m("name", "media", "url", "https://artifacts.example.test"))))
	c := api.NewCatalog([]api.Object{external})
	if issues := ValidateArtifactEndpoint(m("serverRef", "mirorr", "endpointRef", "media"), c, "$.spec.artifactServerEndpoint", false); len(issues) != 0 {
		t.Fatalf("an unresolved serverRef, which the schema reference check reports once, was reported again: %v", issues)
	}
	issues := ValidateArtifactEndpoint(m("serverRef", "mirror", "endpointRef", "media"), c, "$.spec.artifactServerEndpoint", false)
	if len(issues) != 1 || issues[0].Code != "api.reference" || issues[0].Field != "$.spec.artifactServerEndpoint.serverRef" ||
		!mentionsAll(issues[0].Message, "ArtifactServer/mirror", "external") || !mentionsAll(issues[0].Remediation, "spec.artifactServerEndpoint.serverRef", "ArtifactServer/mirror", "management: managed") {
		t.Fatalf("an external serverRef = %#v, want one reference refusal naming the server and management: managed", issues)
	}
}
