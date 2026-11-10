package managedservice

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
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
		"quay.io/metal3-io/sushy-tools" + digest:   "mirror.example.test:8443/images/quay.io/metal3-io/sushy-tools" + digest,
		"library/nginx" + digest:                   "mirror.example.test:8443/images/docker.io/library/nginx" + digest,
		"localhost:5000/squid" + digest:            "localhost:5000/squid" + digest,
		"registry.access.redhat.com/ubi9" + digest: "registry.access.redhat.com/ubi9" + digest,
	} {
		got, source := MirroredImage(catalog, origin)
		if got == origin && source == nil && got == want {
			continue
		}
		if got != want || source == nil || source.Host != "mirror.example.test:8443" || source.CredentialsType != "usernamePassword" {
			t.Fatalf("%s mirrored to %s with %+v, want %s", origin, got, source, want)
		}
	}
	if got, source := MirroredImage(catalogOf(), "quay.io/a/b"+digest); got != "quay.io/a/b"+digest || source != nil {
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
