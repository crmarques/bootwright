package mirrorfill

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

var (
	registryDigest = "sha256:" + strings.Repeat("3", 64)
	sushyDigest    = "sha256:" + strings.Repeat("4", 64)
)

func registryCatalog(objects ...api.Object) api.Catalog {
	catalog := mirrorCatalog("mirror")
	environment, _ := catalog.Find(api.Environment, "lab")
	kept := slices.DeleteFunc(catalog.Objects(), func(object api.Object) bool { return object.Kind() == api.Environment })
	selection := api.MapValue(field("registry", api.MapValue(text("registryRef", "mirror-registry"), text("endpointRef", "ip"))))
	kept = append(kept,
		environment.WithSpec(environment.Spec().With("mirror", selection)),
		api.NewObject(api.Registry, "mirror-registry", api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", "mirror"),
			field("port", api.IntegerValue("5000")), field("tls", api.MapValue(text("secretRef", "mirror-tls"))), text("credentialsRef", "mirror-registry-credentials"),
			field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))))),
		api.NewObject(api.Secret, "mirror-registry-credentials", api.Value{}, api.MapValue(text("type", "usernamePassword"))),
		api.NewObject(api.Secret, "mirror-ca", api.Value{}, api.MapValue(text("type", "caBundle"))),
		api.NewObject(api.Secret, "mirror-tls", api.Value{}, api.MapValue(text("type", "tlsCertificate"),
			field("source", api.MapValue(field("generated", api.MapValue(text("issuerRef", "mirror-ca"))))))))
	return api.NewCatalog(append(kept, objects...))
}

func pinnedImages(api.Catalog) ([]Image, error) {
	return []Image{
		{Digest: sushyDigest, Repository: "quay.io/metal3-io/sushy-tools", Source: "quay.io/metal3-io/sushy-tools@" + sushyDigest},
		{Digest: registryDigest, Repository: "docker.io/library/registry", Source: "docker.io/library/registry@" + registryDigest},
		{Digest: sushyDigest, Repository: "quay.io/metal3-io/sushy-tools", Source: "quay.io/metal3-io/sushy-tools@" + sushyDigest},
	}, nil
}

func plannedImages(t *testing.T, catalog api.Catalog, images Images) lifecycle.CapabilityPlan {
	t.Helper()
	plan, err := NewImages(nil, images).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "lab"},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return plan
}

func imagesRequest(t *testing.T) (ImagesRequest, reconciliation.Block) {
	t.Helper()
	plan := plannedImages(t, registryCatalog(), pinnedImages)
	request, err := DecodeImagesRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := reconciliation.RequestDigest(plan.Definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	return request, reconciliation.Block{BlockDefinition: plan.Definitions[0], RequestDigest: digest}
}

// The image fill plans one block for the Environment's managed image mirror,
// on the Registry's Machine, waiting for the Registry's own block alone, with
// each image the mirror serves once, in source order; it reads the mirror at
// its endpoint with the Registry's credential and its certificate's issuer.
func TestTheImageFillCopiesEachMirroredImageIntoTheRegistryOnItsMachine(t *testing.T) {
	plan := plannedImages(t, registryCatalog(), pinnedImages)
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	definition := plan.Definitions[0]
	if definition.ID != "mirror-images-lab" || definition.Kind != "Registry" || definition.Object != "mirror-registry" || definition.Family != Family ||
		definition.Stage != reconciliation.StageInfrastructureServices ||
		!slices.Equal(definition.Requires, []reconciliation.ObjectRef{{Kind: "Registry", Object: "mirror-registry", Family: "managed-service"}}) {
		t.Fatalf("definition = %+v", definition)
	}
	if !slices.Equal(plan.Secrets, []string{"mirror-access-key", "mirror-ca", "mirror-known-hosts", "mirror-registry-credentials"}) {
		t.Errorf("secrets = %v", plan.Secrets)
	}
	request, err := DecodeImagesRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	if request.Placement.Machine != "mirror" || request.Mirror.Host != "203.0.113.20:5000" || request.Mirror.TrustBundleRef != "mirror-ca" ||
		request.Mirror.CredentialsRef != "mirror-registry-credentials" || request.Mirror.CredentialsType != "usernamePassword" {
		t.Fatalf("request = %+v", request)
	}
	var sources []string
	for _, image := range request.Images {
		sources = append(sources, image.Source)
	}
	if !slices.Equal(sources, []string{"docker.io/library/registry@" + registryDigest, "quay.io/metal3-io/sushy-tools@" + sushyDigest}) {
		t.Fatalf("images = %+v", request.Images)
	}
	if (ImageFill{}).Claimed(registryCatalog())[0] != "Registry/mirror-registry" {
		t.Fatal("the fill claims no Registry")
	}
}

// An external image mirror, no image mirror, or no image pulled through it
// plans nothing.
func TestAnExternalImageMirrorOrNoMirroredImagePlansNoImageFill(t *testing.T) {
	none := func(api.Catalog) ([]Image, error) { return nil, nil }
	if plan := plannedImages(t, registryCatalog(), none); len(plan.Definitions) != 0 {
		t.Fatalf("an empty list planned %+v", plan.Definitions)
	}
	if plan := plannedImages(t, mirrorCatalog("mirror"), pinnedImages); len(plan.Definitions) != 0 {
		t.Fatalf("no image mirror planned %+v", plan.Definitions)
	}
	catalog := registryCatalog()
	registry, _ := catalog.Find(api.Registry, "mirror-registry")
	external := api.NewCatalog(append(slices.DeleteFunc(catalog.Objects(), func(object api.Object) bool { return object.Kind() == api.Registry }),
		registry.WithSpec(api.MapValue(text("management", "external"), text("url", "registry.example.test/mirror")))))
	if plan := plannedImages(t, external, pinnedImages); len(plan.Definitions) != 0 {
		t.Fatalf("an external mirror planned %+v", plan.Definitions)
	}
}

// A frozen request naming an image by tag, a source other than its
// repository at its digest, an unsafe repository or a mirror with no
// credential or anchor never decodes, so no adapter runs it.
func TestAFrozenImagesRequestThatCopiesNoPinnedImageRefuses(t *testing.T) {
	request, _ := imagesRequest(t)
	for name, mutate := range map[string]func(*ImagesRequest){
		"tag":        func(r *ImagesRequest) { r.Images[0].Digest = "latest" },
		"source":     func(r *ImagesRequest) { r.Images[0].Source = "docker.io/library/other@" + registryDigest },
		"repository": func(r *ImagesRequest) { r.Images[0].Repository = "../escape" },
		"duplicate":  func(r *ImagesRequest) { r.Images[1] = r.Images[0] },
		"host":       func(r *ImagesRequest) { r.Mirror.Host = "203.0.113.20" },
		"credential": func(r *ImagesRequest) { r.Mirror.CredentialsRef = "" },
		"anchor":     func(r *ImagesRequest) { r.Mirror.TrustBundleRef = "" },
		"document":   func(r *ImagesRequest) { r.Mirror.CredentialsType = "dockerConfigJson" },
		"empty":      func(r *ImagesRequest) { r.Images = nil },
	} {
		changed := request
		changed.Images = slices.Clone(request.Images)
		mutate(&changed)
		data, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeImagesRequest(data); len(diagnostics.Of(err)) == 0 {
			t.Errorf("%s: the request decoded", name)
		}
	}
}

// The fill's evidence names each image the mirror holds by digest, in request
// order: all of them is the apply's completion, none its no effect, any other
// subset partial, and an image the request does not name in that place
// unknown; a removal is complete only when it reports releasing everything.
func TestTheImageFillReadsItsEffectFromTheImagesTheMirrorHolds(t *testing.T) {
	request, _ := imagesRequest(t)
	first := ImageEvidence{Repository: request.Images[0].Repository, Digest: request.Images[0].Digest, Verified: true}
	second := ImageEvidence{Repository: request.Images[1].Repository, Digest: request.Images[1].Digest, Verified: true}
	cases := []struct {
		name           string
		evidence       ImagesEvidence
		apply, removal reconciliation.EffectState
	}{
		{"complete", ImagesEvidence{Images: []ImageEvidence{first, second}, Postcondition: true}, reconciliation.EffectCompleted, reconciliation.EffectUnknown},
		{"released", ImagesEvidence{Absent: true, Postcondition: true}, reconciliation.EffectNoEffect, reconciliation.EffectCompleted},
		{"empty", ImagesEvidence{Postcondition: true}, reconciliation.EffectNoEffect, reconciliation.EffectUnknown},
		{"released beside images", ImagesEvidence{Absent: true, Images: []ImageEvidence{first}}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		{"one", ImagesEvidence{Images: []ImageEvidence{second}}, reconciliation.EffectPartial, reconciliation.EffectUnknown},
		{"other digest", ImagesEvidence{Images: []ImageEvidence{{Repository: first.Repository, Digest: sushyDigest, Verified: true}}}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		{"reordered", ImagesEvidence{Images: []ImageEvidence{second, first}, Postcondition: true}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
	}
	for _, test := range cases {
		if got := imagesEffect(test.evidence, request, false); got != test.apply {
			t.Errorf("%s: apply effect %s, want %s", test.name, got, test.apply)
		}
		if got := imagesEffect(test.evidence, request, true); got != test.removal {
			t.Errorf("%s: removal effect %s, want %s", test.name, got, test.removal)
		}
	}
}

// The apply lends the Registry's credential and anchor as material and names
// each refusal as the fill's own; a missing skopeo names its package, a
// mismatched digest fails as state and an unproved fill stays unknown. The
// removal lends nothing and its resolution reads no host.
func TestTheImageFillLendsItsCredentialToTheApplyAloneAndNamesItsRefusals(t *testing.T) {
	request, block := imagesRequest(t)
	runner := &scriptedRunner{}
	capability := NewImages(runner, pinnedImages)
	execution := lifecycle.Execution{Context: "lab", Block: block}
	refused := imagesRefusals("lab", request)[SkopeoReason]
	runner.err = refused
	if result, err := capability.Apply(context.Background(), execution); !errors.Is(err, refused) || result.Outcome == reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v, %v", result, err)
	}
	if reported := diagnostics.Of(refused); len(reported) != 1 || !strings.Contains(reported[0].Remediation, "install the skopeo package on Machine/mirror") ||
		reported[0].Object == nil || reported[0].Object.Kind != "Registry" || reported[0].Object.Name != "mirror-registry" {
		t.Fatalf("refusal = %+v", reported)
	}
	if code := diagnostics.Of(imagesRefusals("lab", request)["image-digest-mismatch"])[0].Code; code != "lifecycle.state" {
		t.Fatalf("a digest mismatch is %s", code)
	}
	lent := request.Mirror.Materials()
	if len(runner.request.Refusals) != 4 || len(runner.request.Materials) < len(lent) || !slices.Equal(runner.request.Materials[:len(lent)], lent) {
		t.Fatalf("the apply was lent %+v with refusals %v", runner.request.Materials, runner.request.Refusals)
	}
	parts := map[string]secrets.Part{}
	for _, material := range runner.request.Materials {
		parts[material.Variable] = material.Part
	}
	if parts["imageUsername"] != secrets.UsernamePart || parts["imagePassword"] != secrets.PasswordPart || parts["imageTrust"] != secrets.CertificatePart {
		t.Fatalf("materials = %+v", runner.request.Materials)
	}
	partial, _ := json.Marshal(ImagesEvidence{Images: []ImageEvidence{{Repository: request.Images[0].Repository, Digest: request.Images[0].Digest, Verified: true}},
		Request: block.RequestDigest, Version: ImagesEvidenceVersion})
	runner.err, runner.result = nil, lifecycle.RunResult{Outcome: "changed", Evidence: partial}
	if result, err := capability.Apply(context.Background(), execution); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("an unproved fill = %+v, %v", result, err)
	}
	released, _ := json.Marshal(ImagesEvidence{Absent: true, Images: []ImageEvidence{}, Postcondition: true, Request: block.RequestDigest, Version: ImagesEvidenceVersion})
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: released}
	if result, err := capability.Destroy(context.Background(), execution); err != nil || result.Outcome != reconciliation.OutcomeUnchanged ||
		slices.ContainsFunc(runner.request.Materials, func(material lifecycle.MaterialFile) bool { return slices.Contains(lent, material) }) {
		t.Fatalf("destroy = %+v, %v, lent %+v", result, err, runner.request.Materials)
	}
	runner.request = lifecycle.RunRequest{}
	observation, err := capability.ObserveRemoval(context.Background(), execution)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || runner.request.Implementation != "" {
		t.Fatalf("removal observation = %+v, %v, ran %+v", observation, err, runner.request)
	}
	if classes, _ := capability.SecretClasses(block); classes.Of("mirror-registry-credentials") != secrets.AccessCredential || classes.Of("mirror-ca") != secrets.TrustAnchor {
		t.Fatalf("classes = %+v", classes)
	}
}

// The image fill is no controller precursor: it waits for the controller
// stage like every block the stage does not read.
func TestTheImageFillDoesNotPrecedeTheControllerStage(t *testing.T) {
	if _, precedes := any(ImageFill{}).(lifecycle.ControllerPrecursor); precedes {
		t.Fatal("the image fill declares that it precedes the controller stage")
	}
}

func TestMirrorImagesRequestAndEvidenceGolden(t *testing.T) {
	plan := plannedImages(t, registryCatalog(), pinnedImages)
	matchesGolden(t, "mirror-images-request", plan.Definitions[0].Request)
	digest, err := reconciliation.RequestDigest(plan.Definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := json.Marshal(ImagesEvidence{Images: []ImageEvidence{{Repository: "docker.io/library/registry", Digest: registryDigest, Verified: true}},
		Postcondition: true, Request: digest, Version: ImagesEvidenceVersion})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeImagesEvidence(evidence, digest); err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "mirror-images-evidence", evidence)
}
