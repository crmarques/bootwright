package installation

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// An installation fetches its package tree from an artifact endpoint at an
// IPv6 address through a bracketed URL, which is how Anaconda and every other
// URL reader tell the port from the address, and freezes that URL in its
// request and its Kickstart alike.
func TestAnIPv6ArtifactEndpointFreezesABracketedInstallURL(t *testing.T) {
	addresses := controller().Spec().Get("network", "addresses").Items()
	host := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("network", api.MapValue(field("addresses", api.ListValue(append(addresses,
		api.MapValue(text("name", "v6"), text("address", "fd00::1/64")),
	)...)))))
	server := artifactServer(text("bindAddress", "::"), field("endpoints", api.ListValue(
		api.MapValue(text("name", "ip-https"), text("listenerRef", "https"), text("addressRef", "ip")),
		api.MapValue(text("name", "ip-http"), text("listenerRef", "http"), text("addressRef", "v6")),
	)))
	request, _ := onlyRequest(t, labCatalog(host, server))
	if request.Tree == nil || request.Tree.URL != "http://[fd00::1]:8080/os/rhel-9-8/tree" {
		t.Fatalf("tree = %+v, want the URL http://[fd00::1]:8080/os/rhel-9-8/tree", request.Tree)
	}
	requireLine(t, request.Kickstart, "url --url=http://[fd00::1]:8080/os/rhel-9-8/tree")
	if request.Image.URL != "https://192.0.2.1:8443/os/rhel-01/install.iso" {
		t.Fatalf("an IPv4 endpoint's image URL = %q", request.Image.URL)
	}
}
