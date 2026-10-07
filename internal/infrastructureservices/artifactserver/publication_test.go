package artifactserver

import (
	"net/url"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// A consumer reaches a published file at <protocol>://<host>:<port>, and an
// IPv6 host is bracketed there as every URL writes one, so a fetcher reads its
// port as the port rather than as one more group of the address. IPv4 and DNS
// names are written as they always were.
func TestEndpointURLsBracketIPv6(t *testing.T) {
	addresses := controller().Spec().Get("network", "addresses").Items()
	host := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("network", api.MapValue(field("addresses", api.ListValue(append(addresses,
		api.MapValue(text("name", "v6"), text("address", "fd00::10/64")),
		api.MapValue(text("name", "alias"), text("address", "artifacts.example.test")),
	)...)))))
	server := artifactServer(text("bindAddress", "::"), field("endpoints", api.ListValue(
		api.MapValue(text("name", "ip-https"), text("listenerRef", "https"), text("addressRef", "ip")),
		api.MapValue(text("name", "v6-https"), text("listenerRef", "https"), text("addressRef", "v6")),
		api.MapValue(text("name", "alias-https"), text("listenerRef", "https"), text("addressRef", "alias")),
	)))
	catalog := catalogOf(host, server)
	for endpoint, want := range map[string]struct{ url, host string }{
		"ip-https":    {"https://192.0.2.1:8443", "192.0.2.1"},
		"v6-https":    {"https://[fd00::10]:8443", "fd00::10"},
		"alias-https": {"https://artifacts.example.test:8443", "artifacts.example.test"},
	} {
		t.Run(endpoint, func(t *testing.T) {
			base, err := EndpointURL(catalog, server, endpoint, "MachineInstallProfile/rhel")
			if err != nil || base != want.url {
				t.Fatalf("endpoint URL = %q (%v), want %q", base, err, want.url)
			}
			selection := api.MapValue(text("serverRef", server.Name()), text("endpointRef", endpoint))
			public, err := PublicPath(catalog, server, selection, testContext, "os", "rhel-01", "install.iso", "Machine/rhel-01")
			if err != nil {
				t.Fatal(err)
			}
			private, _, err := PrivatePath(catalog, server, selection, testContext, "os", "rhel-01", "Machine/rhel-01")
			if err != nil {
				t.Fatal(err)
			}
			for _, published := range []string{public.URL, private.URL} {
				parsed, err := url.Parse(published)
				if err != nil || parsed.Hostname() != want.host || parsed.Port() != "8443" {
					t.Fatalf("%q parses to host %q port %q (%v), want %q and 8443", published, parsed.Hostname(), parsed.Port(), err, want.host)
				}
			}
		})
	}
}
