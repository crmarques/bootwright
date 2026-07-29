package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func endpointVIPClusterInstall(platform string, machines int, endpoint v1alpha1.Endpoint) v1alpha1.ClusterInstall {
	ci := v1alpha1.ClusterInstall{
		Metadata: v1alpha1.Metadata{Name: "hub"},
		Platform: v1alpha1.InstallPlatform{Type: platform},
		Endpoints: map[string]v1alpha1.Endpoint{
			v1alpha1.EndpointAPI: endpoint,
		},
	}
	for i := 0; i < machines; i++ {
		ci.Machines = append(ci.Machines, v1alpha1.InstallMachine{Name: "node"})
	}
	return ci
}

func validateEndpointVIP(ci v1alpha1.ClusterInstall) []string {
	return validateClusterEndpoints("ContainerCluster/hub spec.install", ci,
		map[string]v1alpha1.InfraComponent{}, map[string]v1alpha1.NetworkConfig{}, true)
}

func TestClusterEndpointDNSNameAloneRejectedForVIPSlots(t *testing.T) {
	dnsOnly := v1alpha1.Endpoint{DNSName: "api.hub.example.test", Source: v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal}}
	for _, platform := range []string{v1alpha1.PlatformTypeBareMetal, v1alpha1.PlatformTypeVSphere} {
		errs := validateEndpointVIP(endpointVIPClusterInstall(platform, 3, dnsOnly))
		if !containsSubstring(errs, "endpoints.api must set address or source.type=infraComponent") {
			t.Fatalf("platform %s: dnsName alone must not satisfy a VIP-bearing endpoint, got: %v", platform, errs)
		}
	}
}

func TestClusterEndpointVIPSlotAcceptsAddressOrInfraComponent(t *testing.T) {
	withAddress := v1alpha1.Endpoint{Address: "192.168.132.10", Source: v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal}}
	if errs := validateEndpointVIP(endpointVIPClusterInstall(v1alpha1.PlatformTypeBareMetal, 3, withAddress)); len(errs) != 0 {
		t.Fatalf("an address satisfies a VIP-bearing endpoint, got: %v", errs)
	}
	fromComponent := v1alpha1.Endpoint{Source: v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceInfraComponent}}
	errs := validateEndpointVIP(endpointVIPClusterInstall(v1alpha1.PlatformTypeBareMetal, 3, fromComponent))
	if containsSubstring(errs, "must set address") {
		t.Fatalf("source.type=infraComponent satisfies a VIP-bearing endpoint, got: %v", errs)
	}
}

func TestClusterEndpointDNSNameAloneStaysLegalWhereNoVIPRenders(t *testing.T) {
	dnsOnly := v1alpha1.Endpoint{DNSName: "api.hub.example.test", Source: v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal}}
	if errs := validateEndpointVIP(endpointVIPClusterInstall(v1alpha1.PlatformTypeNone, 3, dnsOnly)); len(errs) != 0 {
		t.Fatalf("platform none renders no VIP, so dnsName alone stays legal, got: %v", errs)
	}
	if errs := validateEndpointVIP(endpointVIPClusterInstall(v1alpha1.PlatformTypeBareMetal, 1, dnsOnly)); len(errs) != 0 {
		t.Fatalf("a single-node cluster renders no VIP, so dnsName alone stays legal, got: %v", errs)
	}
}

func TestClusterEndpointStillRequiresOneOf(t *testing.T) {
	empty := v1alpha1.Endpoint{Source: v1alpha1.EndpointSource{Type: v1alpha1.EndpointSourceExternal}}
	errs := validateEndpointVIP(endpointVIPClusterInstall(v1alpha1.PlatformTypeNone, 1, empty))
	if !containsSubstring(errs, "must set address, dnsName, or source.type=infraComponent") {
		t.Fatalf("a bare endpoint must still fail the one-of, got: %v", errs)
	}
}
