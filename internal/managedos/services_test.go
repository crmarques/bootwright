package managedos

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestProfileServiceChoicesUseExplicitTypedReferences(t *testing.T) {
	external := obj(api.Proxy, "egress", m("management", "external", "connection", m("httpsProxy", "http://proxy.example.test:3128")))
	managed := obj(api.Proxy, "managed", m("management", "managed", "endpoints", list(m("name", "install"))))
	clock := obj(api.NTPServer, "clock", m("management", "managed", "endpoints", list(m("name", "installation"))))
	catalog := api.NewCatalog([]api.Object{external, managed, clock})
	profile := anaconda()
	profile = profile.WithSpec(profile.Spec().With("proxy", m("proxyRef", "egress", "noProxy", api.StringList(".example.test"))).With("ntp", list(m("serverRef", "clock"))))
	effective, _ := Normalize(profile, catalog)
	if issues := Validate(effective, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	if !effective.Spec().Get("proxy").Equal(profile.Spec().Get("proxy")) || effective.Spec().Get("ntp").Items()[0].Get("endpointRef").Text() != "installation" {
		t.Fatal("profile service choices not retained")
	}
	if effective.Spec().Get("proxy").Has("connection") {
		t.Fatal("service connection copied into consumer")
	}
	managedChoice := effective.WithSpec(effective.Spec().With("proxy", m("proxyRef", "managed", "endpointRef", "install")))
	if issues := Validate(managedChoice, catalog); len(issues) == 0 {
		t.Fatal("managed machine-install proxy admitted")
	}
	empty, _ := Normalize(anaconda(), catalog)
	if !empty.Spec().Get("proxy").Equal(m("direct", m())) || empty.Spec().Has("ntp") {
		t.Fatal("service singleton inferred or native time default lost")
	}
}

func TestProfileArtifactsRequireExplicitServerAndIndependentHost(t *testing.T) {
	server := obj(api.ArtifactServer, "artifacts", m("management", "managed", "machineRef", "services", "listeners", list(m("name", "plain", "protocol", "http"), m("name", "secure", "protocol", "https")), "endpoints", list(m("name", "packages", "listenerRef", "plain"), m("name", "media", "listenerRef", "secure"))))
	machine := obj(api.Machine, "node", m("os", m("provided", false, "installProfileRef", "install")))
	catalog := api.NewCatalog([]api.Object{server, machine})
	profile := anaconda()
	profile = profile.WithSpec(profile.Spec().WithPath(m("serverRef", "artifacts", "endpointRef", "media"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint").WithPath(m("fromMedia", "local-media:packages.iso", "artifactServerEndpoint", m("serverRef", "artifacts", "endpointRef", "packages")), "installer", "anaconda", "packageSource", "hostedTree"))
	if issues := Validate(profile, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	missing := profile.WithSpec(profile.Spec().WithPath(m("endpointRef", "media"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint"))
	missing, _ = Normalize(missing, catalog)
	if missing.Spec().Has("installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint", "serverRef") {
		t.Fatal("singleton artifact server inferred")
	}
	if issues := Validate(missing, catalog); len(issues) == 0 {
		t.Fatal("missing artifact server admitted")
	}
	encryptedPackages := profile.WithSpec(profile.Spec().WithPath(api.StringValue("media"), "installer", "anaconda", "packageSource", "hostedTree", "artifactServerEndpoint", "endpointRef"))
	if issues := Validate(encryptedPackages, catalog); len(issues) == 0 {
		t.Fatal("HTTPS-only package hosting admitted")
	}
	selfHosted := server.WithSpec(server.Spec().With("machineRef", api.StringValue("node")))
	issues := Validate(profile, api.NewCatalog([]api.Object{selfHosted, machine}))
	if len(issues) != 2 {
		t.Fatal("profile self-hosted artifact dependencies not rejected at both consumers", issues)
	}
}
