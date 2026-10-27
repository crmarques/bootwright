package agentinstall

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func mirrorCatalog(credentialType string, trust bool) api.Catalog {
	objects := singleNodeCatalog().Objects()
	for i, object := range objects {
		if object.Kind() == api.OpenShiftCluster {
			sources := api.ListValue(api.MapValue(text("source", "quay.io/example/release"), field("mirrors", api.StringList("mirror.example.test/release")), text("sourcePolicy", "NeverContactSource")))
			spec := object.Spec().WithPath(api.MapValue(field("mirror", api.MapValue(text("registryRef", "mirror"))), field("imageDigestSources", sources)), "install", "registries")
			if trust {
				spec = spec.WithPath(api.StringList("mirror-ca"), "install", "additionalTrustBundleRefs")
			}
			objects[i] = object.WithSpec(spec)
		}
	}
	return api.NewCatalog(append(objects,
		api.NewObject(api.Registry, "mirror", api.Value{}, api.MapValue(text("management", "external"), text("url", "mirror.example.test"), text("credentialsRef", "mirror-credentials"), text("trustBundleRef", "mirror-ca"))),
		api.NewObject(api.Secret, "mirror-credentials", api.Value{}, api.MapValue(text("type", credentialType)))))
}

func TestRegistryMirrorGolden(t *testing.T) {
	for _, kind := range []string{"usernamePassword", "dockerConfigJson"} {
		t.Run(kind, func(t *testing.T) {
			media, _, _ := onlyRequests(t, mirrorCatalog(kind, true))
			canonical, err := media.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, "media-request-mirror-"+kind, canonical)
			if _, err := DecodeMediaRequest(canonical); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(media.SecretReferences(), "mirror-credentials") {
				t.Fatal("mirror credential not frozen")
			}
		})
	}
}

func TestRegistryMirrorRequiresExplicitTrust(t *testing.T) {
	if got := Refusals(mirrorCatalog("usernamePassword", false)); len(got) != 1 {
		t.Fatalf("refusals = %v", got)
	}
	if _, _, _, err := Requests(mirrorCatalog("usernamePassword", false), "controller", testContext); err == nil {
		t.Fatal("unselected mirror trust accepted")
	}
}
