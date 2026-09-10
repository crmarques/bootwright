package containercluster

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestClusterServiceChoicesAndRegistryPolicy(t *testing.T) {
	o, c := fixture("kubevirt", 1, false)
	proxy := obj(api.Proxy, "egress", m("management", "managed", "endpoints", list(m("name", "install"))))
	clock := obj(api.NTPServer, "clock", m("management", "managed", "endpoints", list(m("name", "time"))))
	mirror := obj(api.Registry, "images", m("management", "managed", "endpoints", list(m("name", "mirror")), "credentialsRef", "mirror-auth", "trustBundleRef", "mirror-ca"))
	c = api.NewCatalog(append(c.Objects(), proxy, clock, mirror))
	o = o.WithSpec(o.Spec().WithPath(m("proxyRef", "egress", "noProxy", api.StringList(".example.test")), "install", "proxy").WithPath(list(m("serverRef", "clock")), "install", "ntp").WithPath(m("mirror", m("registryRef", "images"), "imageDigestSources", list(m("source", "quay.io/example/release", "mirrors", api.StringList("mirror.example.test/release")))), "install", "registries"))
	effective, _ := Normalize(o, c)
	if issues := Validate(effective, c); len(issues) != 0 {
		t.Fatal(issues)
	}
	if effective.Spec().Get("install", "proxy", "endpointRef").Text() != "install" || effective.Spec().Get("install", "ntp").Items()[0].Get("endpointRef").Text() != "time" || effective.Spec().Get("install", "registries", "mirror", "endpointRef").Text() != "mirror" {
		t.Fatal("selected service endpoint not materialized")
	}
	if effective.Spec().Has("install", "registries", "mirror", "credentialsRef") || effective.Spec().Has("install", "registries", "mirror", "trustBundleRef") {
		t.Fatal("registry secret references copied to consumer")
	}
	for _, object := range c.OfKind(api.Machine) {
		if object.Spec().Has("proxy") || object.Spec().Has("os", "install", "ntp") {
			t.Fatal("cluster choices modified bound Machines")
		}
	}
	source := m("source", "quay.io/example/release", "mirrors", api.StringList("mirror.example.test/release"))
	duplicate := effective.WithSpec(effective.Spec().WithPath(list(source, source), "install", "registries", "imageDigestSources"))
	if issues := Validate(duplicate, c); !hasField(issues, "$.spec.install.registries.imageDigestSources[1].source") {
		t.Fatal("duplicate registry mapping admitted", issues)
	}
	if issues := ValidatePartial(duplicate, c); !hasField(issues, "$.spec.install.registries.imageDigestSources[1].source") {
		t.Fatal("duplicate mapping admitted in defaults", issues)
	}
	omitted := o.WithSpec(o.Spec().With("install", o.Spec().Get("install").Without("proxy").Without("ntp").Without("registries")))
	omitted, _ = Normalize(omitted, c)
	if !omitted.Spec().Get("install", "proxy").Equal(m("direct", m())) || omitted.Spec().Has("install", "ntp") || omitted.Spec().Has("install", "registries") {
		t.Fatal("singleton services inferred")
	}
}
