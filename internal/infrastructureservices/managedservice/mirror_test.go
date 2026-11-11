package managedservice

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func mirrorEnvironment() api.Object {
	return api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		field("mirror", api.MapValue(field("registry", api.MapValue(text("registryRef", "mirror"))))),
	))
}

func mirrorRegistry(extra ...api.FieldValue) api.Object {
	return api.NewObject(api.Registry, "mirror", api.Value{}, api.MapValue(append([]api.FieldValue{
		text("management", "external"), text("url", "mirror.example.test:8443/images"),
	}, extra...)...))
}

func secretOf(name, kind string) api.Object {
	return api.NewObject(api.Secret, name, api.Value{}, api.MapValue(text("type", kind), text("source", "contextStore")))
}

func TestAnImageMirrorServesEveryPublicOriginImageUnderItsOriginAtTheSameDigest(t *testing.T) {
	digest := "@sha256:" + strings.Repeat("a", 64)
	catalog := catalogOf(mirrorEnvironment(), mirrorRegistry(text("credentialsRef", "mirror-pull"), text("trustBundleRef", "mirror-ca")),
		secretOf("mirror-pull", "usernamePassword"), secretOf("mirror-ca", "caBundle"))
	for origin, want := range map[string]string{
		"quay.io/metal3-io/sushy-tools" + digest:      "mirror.example.test:8443/images/quay.io/metal3-io/sushy-tools" + digest,
		"library/nginx" + digest:                      "mirror.example.test:8443/images/docker.io/library/nginx" + digest,
		"registry-1.docker.io/library/nginx" + digest: "mirror.example.test:8443/images/docker.io/library/nginx" + digest,
		"localhost:5000/squid" + digest:               "localhost:5000/squid" + digest,
		"registry.access.redhat.com/ubi9" + digest:    "registry.access.redhat.com/ubi9" + digest,
	} {
		got, source := MirroredImage(catalog, origin, ImageConsumer{Identity: "Proxy/egress", Machine: "controller"})
		if got == origin && source == nil && got == want {
			continue
		}
		if got != want || source == nil || source.Host != "mirror.example.test:8443" || source.CredentialsType != "usernamePassword" {
			t.Fatalf("%s mirrored to %s with %+v, want %s", origin, got, source, want)
		}
	}
	if got, source := MirroredImage(catalogOf(), "quay.io/a/b"+digest, ImageConsumer{Identity: "Proxy/egress", Machine: "controller"}); got != "quay.io/a/b"+digest || source != nil {
		t.Fatalf("an undeclared mirror moved the image to %s (%+v)", got, source)
	}
}

func TestAnImageMirrorLendsItsCredentialAndAnchorAndNoValue(t *testing.T) {
	for name, tc := range map[string]struct {
		source *ImageSource
		want   []lifecycle.MaterialFile
	}{
		"none":  {nil, nil},
		"trust": {&ImageSource{Host: "m", TrustBundleRef: "ca"}, []lifecycle.MaterialFile{{Name: "image-trust", Part: secrets.CertificatePart, Secret: "ca", Variable: "imageTrust"}}},
		"user": {&ImageSource{Host: "m", CredentialsRef: "pull", CredentialsType: "usernamePassword"}, []lifecycle.MaterialFile{
			{Name: "image-username", Part: secrets.UsernamePart, Secret: "pull", Variable: "imageUsername"},
			{Name: "image-password", Part: secrets.PasswordPart, Secret: "pull", Variable: "imagePassword"}}},
		"document": {&ImageSource{Host: "m", CredentialsRef: "pull", CredentialsType: "dockerConfigJson", TrustBundleRef: "ca"}, []lifecycle.MaterialFile{
			{Name: "image-auth", Part: secrets.ValuePart, Secret: "pull", Variable: "imageAuth"},
			{Name: "image-trust", Part: secrets.CertificatePart, Secret: "ca", Variable: "imageTrust"}}},
	} {
		if got := tc.source.Materials(); !slices.Equal(got, tc.want) {
			t.Fatalf("%s: materials = %+v, want %+v", name, got, tc.want)
		}
	}
}

// A service's own spec.image.local stays its image; every other service runs
// the mirror's copy, and its execution binds the mirror's Secrets.
func TestAManagedServicePullsFromTheMirrorWithItsSecretsBound(t *testing.T) {
	catalog := catalogOf(controller(), service(api.Proxy, "lab-proxy"), mirrorEnvironment(),
		mirrorRegistry(text("credentialsRef", "mirror-pull"), text("trustBundleRef", "mirror-ca")),
		secretOf("mirror-pull", "dockerConfigJson"), secretOf("mirror-ca", "caBundle"))
	runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: "changed"}}
	definition := testDefinition()
	definition.Image = "quay.io/bootwright/squid@sha256:" + strings.Repeat("a", 64)
	capability := NewCapability(definition, runner)
	plan, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.Secrets, "mirror-ca") || !slices.Contains(plan.Secrets, "mirror-pull") {
		t.Fatalf("the plan binds %v, not the mirror's Secrets", plan.Secrets)
	}
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeRequest(frozen.Blocks[0].Request, testDefinition().Version)
	if err != nil {
		t.Fatal(err)
	}
	if want := "mirror.example.test:8443/images/" + definition.Image; request.Image != want || request.ImageSource == nil {
		t.Fatalf("image = %s (%+v), want %s", request.Image, request.ImageSource, want)
	}
	references, err := capability.SecretReferences(frozen.Blocks[0])
	if err != nil || !slices.Equal(references, []string{"mirror-ca", "mirror-pull"}) {
		t.Fatalf("secret references = %v (%v)", references, err)
	}
	if _, err := capability.Apply(context.Background(), lifecycle.Execution{Operation: "op-1", Attempt: 1, Block: frozen.Blocks[0]}); err == nil && len(runner.requests) == 0 {
		t.Fatal("the apply ran no adapter")
	}
	if len(runner.requests) == 0 || !slices.Equal(runner.requests[0].Materials, request.ImageSource.Materials()) {
		t.Fatalf("the adapter was lent %+v", runner.requests)
	}
	_, _ = capability.Destroy(context.Background(), lifecycle.Execution{Operation: "op-2", Attempt: 1, Block: frozen.Blocks[0]})
	if len(runner.requests) != 2 || len(runner.requests[1].Materials) != 0 {
		t.Fatalf("a removal was lent the mirror's material: %+v", runner.requests)
	}
	local := "registry.example.test/local@sha256:" + strings.Repeat("b", 64)
	pinned := catalogOf(controller(), service(api.Proxy, "lab-proxy", field("image", api.MapValue(text("local", local)))), mirrorEnvironment(), mirrorRegistry())
	requests, err := capability.Requests(pinned, "controller", testContext)
	if err != nil || requests[0].Image != local || requests[0].ImageSource != nil {
		t.Fatalf("a local image moved: %+v (%v)", requests, err)
	}
}

func managedMirror(extra ...api.FieldValue) []api.Object {
	return []api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			field("mirror", api.MapValue(field("registry", api.MapValue(text("registryRef", "mirror"), text("endpointRef", "ip"))))))),
		api.NewObject(api.Machine, "mirror-host", api.Value{}, api.MapValue(field("network", api.MapValue(field("addresses",
			api.ListValue(api.MapValue(text("name", "ip"), text("address", "203.0.113.20")))))))),
		api.NewObject(api.Registry, "mirror", api.Value{}, api.MapValue(append([]api.FieldValue{
			text("management", "managed"), text("machineRef", "mirror-host"), field("port", api.IntegerValue("5000")),
			field("tls", api.MapValue(text("secretRef", "mirror-tls"))), text("credentialsRef", "mirror-push"),
			field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
		}, extra...)...)),
		api.NewObject(api.Secret, "mirror-push", api.Value{}, api.MapValue(text("type", "usernamePassword"))),
		api.NewObject(api.Secret, "mirror-ca", api.Value{}, api.MapValue(text("type", "caBundle"))),
		api.NewObject(api.Secret, "mirror-tls", api.Value{}, api.MapValue(text("type", "tlsCertificate"),
			field("source", api.MapValue(field("generated", api.MapValue(text("issuerRef", "mirror-ca"))))))),
	}
}

// A managed Registry is the image mirror at its selected endpoint's address
// and port, trusted through its certificate's issuer and read with its own
// credential; every image pulled on its own Machine stays at its origin, so
// the mirror never waits for itself, and every other pull requires it.
func TestAManagedImageMirrorIsReadAtItsEndpointAndNeverByItsOwnHost(t *testing.T) {
	origin := "quay.io/metal3-io/sushy-tools@sha256:" + strings.Repeat("a", 64)
	catalog := catalogOf(managedMirror()...)
	got, source := MirroredImage(catalog, origin, ImageConsumer{Identity: "Proxy/egress", Machine: "controller"})
	if got != "203.0.113.20:5000/"+origin || source == nil || *source != (ImageSource{Host: "203.0.113.20:5000", CredentialsRef: "mirror-push", CredentialsType: "usernamePassword", TrustBundleRef: "mirror-ca"}) {
		t.Fatalf("mirrored to %s with %+v", got, source)
	}
	if required := MirrorRequirements(catalog, source); !slices.Equal(required, []reconciliation.ObjectRef{{Kind: "Registry", Object: "mirror"}}) {
		t.Fatalf("a consumer requires %+v", required)
	}
	if got, source := MirroredImage(catalog, origin, ImageConsumer{Identity: "Proxy/egress", Machine: "mirror-host"}); got != origin || source != nil || MirrorRequirements(catalog, source) != nil {
		t.Fatalf("the mirror host pulls %s (%+v)", got, source)
	}
	authored := catalogOf(managedMirror(text("trustBundleRef", "corporate-ca"))...)
	if _, source := MirroredImage(authored, origin, ImageConsumer{Identity: "Proxy/egress", Machine: "controller"}); source == nil || source.TrustBundleRef != "corporate-ca" {
		t.Fatalf("an authored anchor was replaced: %+v", source)
	}
	if required := MirrorRequirements(catalogOf(mirrorEnvironment(), mirrorRegistry()), &ImageSource{Host: "m"}); required != nil {
		t.Fatalf("an external mirror requires %+v", required)
	}
}

// A mirror host Bootwright installs waits for its own emulated BMC and every
// managed server its installation selects, and those wait for their own
// hosts' installations in turn, so exactly those consumers pull their
// origins, while another service or another Machine's emulated BMC on the
// same Machines reads the mirror.
func TestAnInstalledMirrorHostsInstallationPullsFromTheOrigins(t *testing.T) {
	origin := "quay.io/metal3-io/sushy-tools@sha256:" + strings.Repeat("a", 64)
	installed := func(name, profile string, extra ...api.FieldValue) api.Object {
		return api.NewObject(api.Machine, name, api.Value{}, api.MapValue(append([]api.FieldValue{
			field("os", api.MapValue(field("provided", api.BoolValue(false)), text("installProfileRef", profile))),
			field("network", api.MapValue(text("configRef", "guests"), field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", "203.0.113.20")))))),
		}, extra...)...))
	}
	server := func(kind api.Kind, name, machine string) api.Object {
		return api.NewObject(kind, name, api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", machine)))
	}
	selecting := func(server string) api.Value { return api.ListValue(api.MapValue(text("serverRef", server))) }
	endpoint := api.MapValue(field("artifactServerEndpoint", api.MapValue(text("serverRef", "media"))))
	objects := slices.DeleteFunc(managedMirror(), func(object api.Object) bool { return object.Kind() == api.Machine })
	objects = append(objects,
		installed("mirror-host", "mirror", field("substrate", api.MapValue(text("providerRef", "lab-libvirt")))),
		installed("time-host", "time", field("substrate", api.MapValue(text("providerRef", "lab-libvirt")))),
		installed("guest", "time", field("substrate", api.MapValue(text("providerRef", "lab-libvirt")))),
		api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(field("libvirt", api.MapValue(text("machineRef", "hypervisor"))))),
		api.NewObject(api.MachineInstallProfile, "mirror", api.Value{}, api.MapValue(field("installer", api.MapValue(field("anaconda", api.MapValue(field("redfishVirtualMedia", endpoint))))),
			field("ntp", selecting("time")))),
		api.NewObject(api.MachineInstallProfile, "time", api.Value{}, api.MapValue(field("ntp", selecting("deep-time")))),
		api.NewObject(api.NetworkConfig, "guests", api.Value{}, api.MapValue(field("dns", selecting("resolver")))),
		server(api.ArtifactServer, "media", "media-host"), server(api.NTPServer, "time", "time-host"), server(api.NTPServer, "deep-time", "deep-host"),
		server(api.DNSServer, "resolver", "resolver-host"), server(api.Proxy, "egress", "resolver-host"), server(api.NTPServer, "spare-time", "time-host"),
		api.NewObject(api.DNSServer, "external-resolver", api.Value{}, api.MapValue(text("management", "external"), text("machineRef", "elsewhere"))),
	)
	catalog := catalogOf(objects...)
	mirror, _ := infrastructureservices.ImageMirrorOf(catalog)
	want := []string{"ArtifactServer/media", "DNSServer/resolver", "Machine/mirror-host", "Machine/time-host", "NTPServer/deep-time", "NTPServer/time"}
	if got := slices.Sorted(maps.Keys(OriginConsumers(catalog, mirror))); !slices.Equal(got, want) {
		t.Fatalf("the origin consumers are %v, want %v", got, want)
	}
	machine := func(name string) api.Object { found, _ := catalog.Find(api.Machine, name); return found }
	service := func(kind api.Kind, name string) api.Object { found, _ := catalog.Find(kind, name); return found }
	for name, consumer := range map[string]ImageConsumer{
		"the mirror host's BMC": EmulatedBMCConsumer(machine("mirror-host"), "hypervisor"),
		"the time host's BMC":   EmulatedBMCConsumer(machine("time-host"), "hypervisor"),
		"the resolver":          ServiceConsumer(service(api.DNSServer, "resolver")),
		"the media server":      ServiceConsumer(service(api.ArtifactServer, "media")),
		"the deep time server":  ServiceConsumer(service(api.NTPServer, "deep-time")),
		"the mirror host's own": {Identity: "Proxy/beside-mirror", Machine: "mirror-host"},
	} {
		if got, source := MirroredImage(catalog, origin, consumer); got != origin || source != nil {
			t.Errorf("%s pulls %s (%+v)", name, got, source)
		}
	}
	for name, consumer := range map[string]ImageConsumer{
		"another guest's BMC":             EmulatedBMCConsumer(machine("guest"), "hypervisor"),
		"a proxy beside the resolver":     ServiceConsumer(service(api.Proxy, "egress")),
		"a time server nothing waits for": ServiceConsumer(service(api.NTPServer, "spare-time")),
		"a service on the controller":     {Identity: "Proxy/controller-proxy", Machine: "controller"},
	} {
		if got, source := MirroredImage(catalog, origin, consumer); got != "203.0.113.20:5000/"+origin || source == nil || MirrorRequirements(catalog, source) == nil {
			t.Errorf("%s pulls %s (%+v)", name, got, source)
		}
	}
}
