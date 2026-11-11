package infrastructureservices

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func mirrorGraph(address string, port int64, artifacts api.Value, issuer string) api.Catalog {
	generated := m()
	if issuer != "" {
		generated = m("issuerRef", issuer)
	}
	return api.NewCatalog([]api.Object{
		obj(api.Environment, "lab", m("mirror", m("artifacts", artifacts), "controller", m("machineRef", "controller"))),
		obj(api.Machine, "mirror", m("network", m("addresses", list(m("name", "ip", "address", address))))),
		obj(api.ArtifactServer, "mirror", m("management", "managed", "machineRef", "mirror", "tls", m("secretRef", "mirror-tls"),
			"listeners", list(m("name", "https", "protocol", "https", "port", api.IntegerValue(itoa(port)))),
			"endpoints", list(m("name", "ip", "listenerRef", "https", "addressRef", "ip")))),
		obj(api.Secret, "mirror-ca", m("type", "caBundle")),
		obj(api.Secret, "mirror-tls", m("type", "tlsCertificate", "source", m("generated", generated))),
	})
}

func itoa(value int64) string {
	if value == 443 {
		return "443"
	}
	return "8443"
}

// A managed artifact mirror's URL is its selected endpoint beneath the
// Environment's mirror subtree, the default HTTPS port left implicit and an
// IPv6 address bracketed; its anchor is the authority that issues its serving
// certificate unless one is authored; and an operator's URL stays as written.
func TestTheArtifactMirrorIsDerivedFromItsManagedServer(t *testing.T) {
	managed := m("serverRef", "mirror", "endpointRef", "ip")
	for name, test := range map[string]struct {
		catalog    api.Catalog
		url, trust string
		managed    bool
	}{
		"managed":        {mirrorGraph("203.0.113.20", 443, managed, "mirror-ca"), "https://203.0.113.20/mirror/lab", "mirror-ca", true},
		"other port":     {mirrorGraph("203.0.113.20", 8443, managed, "mirror-ca"), "https://203.0.113.20:8443/mirror/lab", "mirror-ca", true},
		"ipv6":           {mirrorGraph("2001:db8::20", 443, managed, "mirror-ca"), "https://[2001:db8::20]/mirror/lab", "mirror-ca", true},
		"authored trust": {mirrorGraph("203.0.113.20", 443, m("serverRef", "mirror", "endpointRef", "ip", "trustBundleRef", "corporate-ca"), "mirror-ca"), "https://203.0.113.20/mirror/lab", "corporate-ca", true},
		"no issuer":      {mirrorGraph("203.0.113.20", 443, managed, ""), "https://203.0.113.20/mirror/lab", "", true},
		"no endpoint":    {mirrorGraph("203.0.113.20", 443, m("serverRef", "mirror", "endpointRef", "missing"), "mirror-ca"), "", "mirror-ca", true},
		"operator":       {mirrorGraph("203.0.113.20", 443, m("url", "https://mirror.example.test/tools"), "mirror-ca"), "https://mirror.example.test/tools", "", false},
	} {
		mirror, declared := ArtifactMirrorOf(test.catalog)
		if !declared || mirror.URL != test.url || mirror.TrustBundleRef != test.trust || mirror.Managed != test.managed {
			t.Errorf("%s: mirror = %+v (%v)", name, mirror, declared)
		}
		if host := MirrorHost(test.catalog); (host == "mirror") != test.managed {
			t.Errorf("%s: mirror host %q", name, host)
		}
	}
	if _, declared := ArtifactMirrorOf(mirrorGraph("203.0.113.20", 443, m(), "mirror-ca")); declared {
		t.Error("an empty declaration names a mirror")
	}
}
