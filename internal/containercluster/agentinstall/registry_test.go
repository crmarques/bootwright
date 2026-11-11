package agentinstall

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
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

func environmentMirrorCatalog(extra ...api.Object) api.Catalog {
	return underTheImageMirror(singleNodeCatalog(), extra...)
}

func underTheImageMirror(catalog api.Catalog, extra ...api.Object) api.Catalog {
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == api.Environment {
			objects[i] = object.WithSpec(object.Spec().With("mirror", api.MapValue(field("registry", api.MapValue(text("registryRef", "mirror"))))))
		}
	}
	return api.NewCatalog(append(append(objects,
		api.NewObject(api.Registry, "mirror", api.Value{}, api.MapValue(text("management", "external"), text("url", "mirror.example.test/images"), text("credentialsRef", "mirror-credentials"), text("trustBundleRef", "mirror-ca"))),
		api.NewObject(api.Secret, "mirror-credentials", api.Value{}, api.MapValue(text("type", "usernamePassword")))), extra...))
}

// The Environment's image mirror is the cluster's mirror with nothing authored
// on the cluster: its credential joins the pull secret, its trust joins the
// cluster's, and both release repositories map to it, never contacting quay.io.
func TestTheEnvironmentsImageMirrorServesTheReleaseRepositories(t *testing.T) {
	media, _, _ := onlyRequests(t, environmentMirrorCatalog())
	canonical, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "media-request-environment-mirror", canonical)
	if media.RegistryMirror == nil || media.RegistryMirror.Host != "mirror.example.test" || !slices.Contains(media.SecretReferences(), "mirror-credentials") {
		t.Fatalf("mirror = %+v, secrets %v", media.RegistryMirror, media.SecretReferences())
	}
	if !slices.Contains(media.TrustBundleRefs, "mirror-ca") {
		t.Fatalf("trust = %v", media.TrustBundleRefs)
	}
	sources := ImageDigestSources(environmentMirrorCatalog(), clusterOf(t, environmentMirrorCatalog()))
	want := []string{"quay.io/openshift-release-dev/ocp-release", "quay.io/openshift-release-dev/ocp-v4.0-art-dev"}
	if len(sources) != len(want) {
		t.Fatalf("sources = %v", sources)
	}
	for index, source := range sources {
		entry := source.(map[string]any)
		if entry["source"] != want[index] || entry["sourcePolicy"] != "NeverContactSource" || entry["mirrors"].([]any)[0] != "mirror.example.test/images/"+want[index] {
			t.Fatalf("source %d = %v", index, entry)
		}
	}
}

func TestAClusterMirrorBesideTheEnvironmentsRefusesAndAnAuthoredSourceStays(t *testing.T) {
	catalog := environmentMirrorCatalog(api.NewObject(api.Registry, "other", api.Value{}, api.MapValue(text("management", "external"), text("url", "other.example.test"))))
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == api.OpenShiftCluster {
			authored := api.ListValue(api.MapValue(text("source", "quay.io/openshift-release-dev/ocp-release"), field("mirrors", api.StringList("other.example.test/release"))))
			objects[i] = object.WithSpec(object.Spec().WithPath(api.MapValue(field("mirror", api.MapValue(text("registryRef", "other"))), field("imageDigestSources", authored)), "install", "registries"))
		}
	}
	other := api.NewCatalog(objects)
	if _, _, _, err := Requests(other, "controller", testContext); err == nil || len(diagnostics.Of(err)) == 0 || !strings.Contains(diagnostics.Of(err)[0].Message, "beside the Environment's image mirror") {
		t.Fatalf("a second mirror was accepted: %v", err)
	}
	sources := ImageDigestSources(other, clusterOf(t, other))
	if len(sources) != 2 || sources[0].(map[string]any)["mirrors"].([]any)[0] != "other.example.test/release" {
		t.Fatalf("an authored source was replaced: %v", sources)
	}
}

func clusterOf(t *testing.T, catalog api.Catalog) api.Object {
	t.Helper()
	clusters := catalog.OfKind(api.OpenShiftCluster)
	if len(clusters) != 1 {
		t.Fatalf("clusters = %v", clusters)
	}
	return clusters[0]
}

func TestABroaderAuthoredSourceKeepsTheReleaseRepositoriesItMaps(t *testing.T) {
	objects := environmentMirrorCatalog().Objects()
	for i, object := range objects {
		if object.Kind() == api.OpenShiftCluster {
			authored := api.ListValue(api.MapValue(text("source", "quay.io/openshift-release-dev"), field("mirrors", api.StringList("mirror.example.test/images/ocp")), text("sourcePolicy", "NeverContactSource")))
			objects[i] = object.WithSpec(object.Spec().WithPath(authored, "install", "registries", "imageDigestSources"))
		}
	}
	catalog := api.NewCatalog(objects)
	cluster := clusterOf(t, catalog)
	if sources := ImageDigestSources(catalog, cluster); len(sources) != 1 {
		t.Fatalf("a derived entry overrode a broader authored one: %v", sources)
	}
	if !NeverContacted(catalog, cluster, "quay.io/openshift-release-dev/ocp-v4.0-art-dev") || NeverContacted(catalog, cluster, "quay.io/openshift-release-devx/ocp") {
		t.Fatal("the never-contacted test does not respect path boundaries")
	}
}
