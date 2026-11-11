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

// A managed image mirror is read at its selected endpoint's address and the
// Registry's port, an IPv6 address bracketed, trusted through its serving
// certificate's issuer unless an anchor is authored; an external one is its
// url as written, and its Machine is the image mirror host only when managed.
func TestTheImageMirrorIsDerivedFromItsManagedRegistry(t *testing.T) {
	graph := func(address string, registry ...any) api.Catalog {
		return api.NewCatalog([]api.Object{
			obj(api.Environment, "lab", m("mirror", m("registry", m("registryRef", "mirror", "endpointRef", "ip")))),
			obj(api.Machine, "mirror", m("network", m("addresses", list(m("name", "ip", "address", address))))),
			obj(api.Registry, "mirror", m(append([]any{"machineRef", "mirror", "port", api.IntegerValue("5000"), "tls", m("secretRef", "mirror-tls"),
				"credentialsRef", "mirror-push", "endpoints", list(m("name", "ip", "addressRef", "ip"))}, registry...)...)),
			obj(api.Secret, "mirror-ca", m("type", "caBundle")),
			obj(api.Secret, "mirror-tls", m("type", "tlsCertificate", "source", m("generated", m("issuerRef", "mirror-ca")))),
		})
	}
	for name, test := range map[string]struct {
		catalog               api.Catalog
		location, trust, host string
		managed               bool
	}{
		"managed":        {graph("203.0.113.20", "management", "managed"), "203.0.113.20:5000", "mirror-ca", "mirror", true},
		"ipv6":           {graph("2001:db8::20", "management", "managed"), "[2001:db8::20]:5000", "mirror-ca", "mirror", true},
		"authored trust": {graph("203.0.113.20", "management", "managed", "trustBundleRef", "corporate-ca"), "203.0.113.20:5000", "corporate-ca", "mirror", true},
		"external":       {graph("203.0.113.20", "management", "external", "url", "mirror.example.test/images"), "mirror.example.test/images", "", "", false},
	} {
		mirror, declared := ImageMirrorOf(test.catalog)
		if !declared || mirror.Location != test.location || mirror.TrustBundleRef != test.trust || mirror.Managed != test.managed || mirror.CredentialsRef != "mirror-push" {
			t.Errorf("%s: mirror = %+v (%v)", name, mirror, declared)
		}
		if host := ImageMirrorHost(test.catalog); host != test.host {
			t.Errorf("%s: mirror host %q", name, host)
		}
	}
	if _, declared := ImageMirrorOf(mirrorGraph("203.0.113.20", 443, m("url", "https://mirror.example.test"), "")); declared {
		t.Error("an Environment without spec.mirror.registry names an image mirror")
	}
}
