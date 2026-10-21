package ceph

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage"
)

func TestAuthoredVersionsReachFrozenNativeRequestsWithoutCompatibilitySelection(t *testing.T) {
	for _, selection := range []string{"unrelated-release", "not a version?", "does-not-exist", "incompatible+build", "--native-value"} {
		t.Run(selection, func(t *testing.T) {
			catalog := pinnedCatalog(t, func(spec api.Value) api.Value {
				for _, path := range [][]string{{"release"}, {"packageVersion"}, {"cephadm", "ansible", "packageVersion"}, {"nfsGanesha", "packageVersion"}, {"image", "version"}} {
					spec = spec.WithPath(api.StringValue(selection), append([]string{"ceph"}, path...)...)
				}
				return spec.WithPath(api.StringValue("https://packages.example.test/native"), "ceph", "packageRepository").WithPath(api.StringValue("https://packages.example.test/keys/ibm.asc"), "ceph", "packageRepositoryKey").WithPath(api.StringValue("cp.icr.io/other/selected-image"), "ceph", "image", "base")
			})
			state := compilation.NewState(catalog, catalog, nil)
			for _, kind := range []api.Kind{api.StorageCluster, api.StoragePool, api.StorageClient} {
				capability := New(kind, nil)
				if refusals := capability.Unsupported(state); len(refusals) != 0 {
					t.Fatal(refusals)
				}
				request, err := capability.request(catalog, catalog.OfKind(kind)[0], "synthetic")
				if err != nil {
					t.Fatal(err)
				}
				if request.Release != selection || request.Cephadm.PackageVersion != selection || request.AnsiblePackageVersion != selection || request.NFSGaneshaPackageVersion != selection || request.Image != "cp.icr.io/other/selected-image:"+selection || request.PackageRepository != "https://packages.example.test/native" {
					t.Fatalf("authored coordinate changed: %+v", request)
				}
				data, err := request.Canonical()
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := DecodeRequest(data)
				if err != nil || decoded.Release != selection || decoded.AnsiblePackageVersion != selection || decoded.NFSGaneshaPackageVersion != selection {
					t.Fatalf("frozen coordinates changed: %+v %v", decoded, err)
				}
			}
			plan, err := NewImage(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: state, Controller: "controller"})
			if err != nil || len(plan.Definitions) != 1 {
				t.Fatalf("image plan: %+v %v", plan, err)
			}
			image, err := DecodeImageRequest(plan.Definitions[0].Request)
			if err != nil || image.Tag != selection || image.Release != selection {
				t.Fatalf("image selection changed: %+v %v", image, err)
			}
		})
	}
}

func TestDirectImageDigestHasNoDiscoveryTag(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	catalog := pinnedCatalog(t, func(spec api.Value) api.Value {
		return spec.WithPath(api.StringValue(digest), "ceph", "image", "version")
	})
	plan, err := NewImage(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	request, err := DecodeImageRequest(plan.Definitions[0].Request)
	if err != nil || request.Tag != "" || request.Image != PinnedRepository+"@"+digest {
		t.Fatalf("direct digest changed: %+v %v", request, err)
	}
}

func TestEntitlementNamespaceScopesSelectionAndFreezesOnlyCredentialAuthority(t *testing.T) {
	catalog := pinnedCatalog(t, nil)
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == api.Entitlement {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.StringValue("cp.icr.io/cp/ibm-ceph"), "registry", "url"))
		}
	}
	catalog = api.NewCatalog(objects)
	cluster := catalog.OfKind(api.StorageCluster)[0]
	request, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil || request.Registry != "cp.icr.io" || !strings.HasPrefix(request.Image, "cp.icr.io/cp/ibm-ceph/") {
		t.Fatalf("scoped selection: %+v %v", request, err)
	}
	foreign := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("cp.icr.io/cp/unrelated/ceph"), "ceph", "image", "base"))
	if _, err := baseRequest(catalog, foreign, "synthetic"); err == nil {
		t.Fatal("entitlement namespace widened to another repository")
	}
}

func TestEntitlementDefaultRegistryURLPlans(t *testing.T) {
	catalog := pinnedCatalog(t, nil)
	objects := catalog.Objects()
	for i, object := range objects {
		if object.Kind() == api.Entitlement && object.Name() == "ibm" {
			objects[i], _ = storage.Normalize(object.WithSpec(requestMap("type", "ibm-storage-ceph", "registry", requestMap("credentialsRef", "registry"), "license", requestMap("accept", true))), catalog)
			if url := objects[i].Spec().Get("registry", "url").Text(); url != "cp.icr.io/cp" {
				t.Fatalf("default registry url %q", url)
			}
		}
	}
	catalog = api.NewCatalog(objects)
	state := compilation.NewState(catalog, catalog, nil)
	for _, kind := range []api.Kind{api.StorageCluster, api.StoragePool, api.StorageClient} {
		if refusals := New(kind, nil).Unsupported(state); len(refusals) != 0 {
			t.Fatalf("%s: default registry refused: %+v", kind, refusals)
		}
	}
	request, err := baseRequest(catalog, catalog.OfKind(api.StorageCluster)[0], "synthetic")
	if err != nil || request.Registry != PinnedRegistry || request.Image != PinnedRepository+":"+PinnedTag {
		t.Fatalf("default registry request: %+v %v", request, err)
	}
	plan, err := NewImage(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: state, Controller: "controller"})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	image, err := DecodeImageRequest(plan.Definitions[0].Request)
	if err != nil || image.Registry != "cp.icr.io" || image.Base != PinnedRepository {
		t.Fatalf("default registry image request: %+v %v", image, err)
	}
}
