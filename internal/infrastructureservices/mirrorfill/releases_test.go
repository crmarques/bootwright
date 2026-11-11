package mirrorfill

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	secretmaterial "github.com/crmarques/bootwright/internal/secrets/material"
)

var releaseDigest = "sha256:" + strings.Repeat("5", 64)

const (
	releaseVersion = "4.21.15"
	clientURL      = "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.15/openshift-client-linux-4.21.15.tar.gz"
)

func clusterReleases(digest string) Releases {
	return func(api.Catalog) ([]Release, error) {
		return []Release{
			{Client: ReleaseClientFor(releaseVersion, clientURL), Cluster: "sno", Digest: digest, PullSecretRef: "openshift-pull-secret", Version: releaseVersion},
			{Client: ReleaseClientFor(releaseVersion, clientURL), Cluster: "compact", PullSecretRef: "openshift-pull-secret", Version: releaseVersion},
		}, nil
	}
}

func plannedReleases(t *testing.T, catalog api.Catalog, releases Releases) lifecycle.CapabilityPlan {
	t.Helper()
	plan, err := NewReleases(nil, releases, secretmaterial.RegistryAuth).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "lab"},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return plan
}

func releaseRequest(t *testing.T, digest string) (ReleaseRequest, reconciliation.Block) {
	t.Helper()
	plan := plannedReleases(t, registryCatalog(), clusterReleases(digest))
	definition := plan.Definitions[1]
	request, err := DecodeReleaseRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	requestDigest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	return request, reconciliation.Block{BlockDefinition: definition, RequestDigest: requestDigest}
}

// The release fill plans one block per cluster under the managed image
// mirror, on the Registry's Machine, waiting for the Registry's own block and
// holding the Registry's push exclusively; it copies the cluster's pinned
// digest or else its version's tag, and reads oc from the release's own
// client directory, checked against the publisher's list.
func TestTheReleaseFillCopiesEachClustersReleaseIntoTheRegistryOnItsMachine(t *testing.T) {
	plan := plannedReleases(t, registryCatalog(), clusterReleases(releaseDigest))
	if len(plan.Definitions) != 2 {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	definition := plan.Definitions[1]
	if definition.ID != "mirror-release-sno" || definition.Kind != "OpenShiftCluster" || definition.Object != "sno" || definition.Family != Family ||
		definition.Stage != reconciliation.StageInfrastructureServices || !slices.Equal(definition.Exclusive, []string{"registry-push:mirror-registry"}) ||
		!slices.Equal(definition.Requires, []reconciliation.ObjectRef{{Kind: "Registry", Object: "mirror-registry", Family: "managed-service"}}) {
		t.Fatalf("definition = %+v", definition)
	}
	if !slices.Equal(plan.Secrets, []string{"mirror-access-key", "mirror-ca", "mirror-known-hosts", "mirror-registry-credentials", "openshift-pull-secret"}) {
		t.Errorf("secrets = %v", plan.Secrets)
	}
	pinned, _ := releaseRequest(t, releaseDigest)
	tagged, _ := releaseRequest(t, "")
	if pinned.Source() != "quay.io/openshift-release-dev/ocp-release@"+releaseDigest || tagged.Source() != "quay.io/openshift-release-dev/ocp-release:4.21.15-x86_64" {
		t.Fatalf("sources = %s and %s", pinned.Source(), tagged.Source())
	}
	if pinned.Placement.Machine != "mirror" || pinned.Mirror.Host != "203.0.113.20:5000" || pinned.Mirror.TrustBundleRef != "mirror-ca" ||
		pinned.Release.Components != "quay.io/openshift-release-dev/ocp-v4.0-art-dev" || pinned.Client.URL != clientURL ||
		pinned.Client.Checksums != "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.15/sha256sum.txt" {
		t.Fatalf("request = %+v", pinned)
	}
	if !slices.Equal(definition.Impacts, []string{"publish-content 203.0.113.20:5000/quay.io/openshift-release-dev/ocp-v4.0-art-dev",
		"publish-content 203.0.113.20:5000/quay.io/openshift-release-dev/ocp-release:4.21.15-x86_64"}) {
		t.Fatalf("impacts = %v", definition.Impacts)
	}
	if _, precedes := any(ReleaseFill{}).(lifecycle.ControllerPrecursor); precedes {
		t.Fatal("the release fill declares that it precedes the controller stage")
	}
	if releaseDeadline() > lifecycle.MaxDeadline || releaseDeadline() < releaseBudget {
		t.Fatalf("deadline %s", releaseDeadline())
	}
}

// An external image mirror, no image mirror, or no cluster under it plans no
// release fill.
func TestAnExternalImageMirrorOrNoClusterPlansNoReleaseFill(t *testing.T) {
	none := func(api.Catalog) ([]Release, error) { return nil, nil }
	if plan := plannedReleases(t, registryCatalog(), none); len(plan.Definitions) != 0 {
		t.Fatalf("no cluster planned %+v", plan.Definitions)
	}
	if plan := plannedReleases(t, mirrorCatalog("mirror"), clusterReleases("")); len(plan.Definitions) != 0 {
		t.Fatalf("no image mirror planned %+v", plan.Definitions)
	}
	catalog := registryCatalog()
	registry, _ := catalog.Find(api.Registry, "mirror-registry")
	external := api.NewCatalog(append(slices.DeleteFunc(catalog.Objects(), func(object api.Object) bool { return object.Kind() == api.Registry }),
		registry.WithSpec(api.MapValue(text("management", "external"), text("url", "registry.example.test/mirror")))))
	if plan := plannedReleases(t, external, clusterReleases("")); len(plan.Definitions) != 0 {
		t.Fatalf("an external mirror planned %+v", plan.Definitions)
	}
}

// A frozen request that would copy another repository, push another tag,
// read oc from anywhere but an HTTPS file of that release, check it against
// another list, or read the mirror without its credential and anchor never
// decodes, so no adapter runs it.
func TestAFrozenReleaseRequestThatCopiesAnotherReleaseRefuses(t *testing.T) {
	request, _ := releaseRequest(t, releaseDigest)
	for name, mutate := range map[string]func(*ReleaseRequest){
		"repository": func(r *ReleaseRequest) { r.Release.Repository = "quay.io/example/release" },
		"components": func(r *ReleaseRequest) { r.Release.Components = "quay.io/example/content" },
		"tag":        func(r *ReleaseRequest) { r.Release.Tag = "latest" },
		"digest":     func(r *ReleaseRequest) { r.Release.Digest = "sha256:short" },
		"version":    func(r *ReleaseRequest) { r.Release.Version, r.Release.Tag = "4.21", "4.21-x86_64" },
		"client": func(r *ReleaseRequest) {
			r.Client.URL = "http://mirror.openshift.com/openshift-client-linux-4.21.15.tar.gz"
		},
		"file": func(r *ReleaseRequest) { r.Client.File = "oc.tar.gz" },
		"query": func(r *ReleaseRequest) {
			r.Client.URL = "https://example.test/openshift-client-linux-4.21.15.tar.gz?/openshift-client-linux-4.21.15.tar.gz"
		},
		"checksums":  func(r *ReleaseRequest) { r.Client.Checksums = "https://example.test/sha256sum.txt" },
		"host":       func(r *ReleaseRequest) { r.Mirror.Host = "203.0.113.20" },
		"credential": func(r *ReleaseRequest) { r.Mirror.CredentialsRef = "" },
		"anchor":     func(r *ReleaseRequest) { r.Mirror.TrustBundleRef = "" },
		"pull":       func(r *ReleaseRequest) { r.PullSecretRef = "" },
	} {
		changed := request
		mutate(&changed)
		data, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeReleaseRequest(data); len(diagnostics.Of(err)) == 0 {
			t.Errorf("%s: the request decoded", name)
		}
	}
}

// The fill's evidence names the release digest the mirror holds and each
// component it verified there: the release with every component and none
// missing completes the apply, nothing held is no effect, anything between is
// partial, and a release at another digest than the pinned one is unknown.
func TestTheReleaseFillReadsItsEffectFromWhatTheMirrorHolds(t *testing.T) {
	request, _ := releaseRequest(t, releaseDigest)
	component := ComponentEvidence{Name: "cli", Digest: registryDigest}
	cases := []struct {
		name           string
		evidence       ReleaseEvidence
		apply, removal reconciliation.EffectState
	}{
		{"complete", ReleaseEvidence{Digest: releaseDigest, Components: []ComponentEvidence{component}, Postcondition: true}, reconciliation.EffectCompleted, reconciliation.EffectUnknown},
		{"missing", ReleaseEvidence{Digest: releaseDigest, Components: []ComponentEvidence{component}, Missing: 1}, reconciliation.EffectPartial, reconciliation.EffectUnknown},
		{"empty", ReleaseEvidence{Postcondition: true}, reconciliation.EffectNoEffect, reconciliation.EffectUnknown},
		{"released", ReleaseEvidence{Absent: true, Postcondition: true}, reconciliation.EffectNoEffect, reconciliation.EffectCompleted},
		{"released beside a release", ReleaseEvidence{Absent: true, Digest: releaseDigest}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		{"other digest", ReleaseEvidence{Digest: registryDigest, Components: []ComponentEvidence{component}, Postcondition: true}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		{"no components", ReleaseEvidence{Digest: releaseDigest, Postcondition: true}, reconciliation.EffectPartial, reconciliation.EffectUnknown},
	}
	for _, test := range cases {
		if got := releaseEffect(test.evidence, request, false); got != test.apply {
			t.Errorf("%s: apply effect %s, want %s", test.name, got, test.apply)
		}
		if got := releaseEffect(test.evidence, request, true); got != test.removal {
			t.Errorf("%s: removal effect %s, want %s", test.name, got, test.removal)
		}
	}
	tagged, _ := releaseRequest(t, "")
	if got := releaseEffect(cases[5].evidence, tagged, false); got != reconciliation.EffectCompleted {
		t.Fatalf("a release copied by tag reads %s", got)
	}
}

func releaseMaterial() map[string]secrets.Material {
	pullSecret := `{"auths":{"cloud.openshift.com":{"auth":"Y2xvdWQ6dG9rZW4=","email":"owner@example.test"},` +
		`"quay.io":{"auth":"cXVheTp0b2tlbg==","email":"owner@example.test"},"registry.redhat.io":{"auth":"cmVkaGF0OnRva2Vu"}}}`
	return map[string]secrets.Material{
		"openshift-pull-secret":       secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(pullSecret)}),
		"mirror-registry-credentials": secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("mirror"), secrets.PasswordPart: []byte("s3cret")}),
		"mirror-ca":                   secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("anchor")}),
	}
}

type lendingRunner struct {
	scriptedRunner
	document []byte
	lent     secrets.Material
}

func (r *lendingRunner) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.lent = request.Material[releaseAuthMaterial]
	r.document, _ = r.lent.Part(secrets.ValuePart)
	return r.scriptedRunner.Run(ctx, request)
}

// The apply lends one auth document derived on the controller, holding the
// pull secret's quay.io token and the Registry's credential for its own host
// and nothing else, never the pull secret itself; the document is cleared
// when the run returns, whatever it returned; observation lends the mirror's
// credential and anchor alone, removal nothing, and a pull secret with no
// quay.io token refuses before any run, naming no material.
func TestTheReleaseFillLendsATwoEntryAuthDocumentToTheApplyAlone(t *testing.T) {
	request, block := releaseRequest(t, releaseDigest)
	for _, failed := range []bool{false, true} {
		runner := &lendingRunner{}
		complete, _ := json.Marshal(ReleaseEvidence{Components: []ComponentEvidence{{Name: "cli", Digest: registryDigest}}, Digest: releaseDigest,
			Postcondition: true, Request: block.RequestDigest, Version: ReleaseEvidenceVersion})
		runner.result = lifecycle.RunResult{Outcome: "changed", Evidence: complete}
		refused := releaseRefusals("lab", request)["release-unreachable"]
		if failed {
			runner.result, runner.err = lifecycle.RunResult{}, refused
		}
		material := releaseMaterial()
		result, err := NewReleases(runner, clusterReleases(releaseDigest), secretmaterial.RegistryAuth).Apply(context.Background(), lifecycle.Execution{Context: "lab", Block: block, Material: material})
		if failed != (err != nil) || !failed && result.Outcome != reconciliation.OutcomeChanged || failed && !errors.Is(err, refused) {
			t.Fatalf("apply (failed %v) = %+v, %v", failed, result, diagnostics.Of(err))
		}
		var document struct {
			Auths map[string]map[string]string `json:"auths"`
		}
		if err := json.Unmarshal(runner.document, &document); err != nil {
			t.Fatal(err)
		}
		mirror := base64.StdEncoding.EncodeToString([]byte("mirror:s3cret"))
		want := map[string]map[string]string{"quay.io": {"auth": "cXVheTp0b2tlbg=="}, "203.0.113.20:5000": {"auth": mirror}}
		if !maps.EqualFunc(document.Auths, want, maps.Equal) {
			t.Fatalf("the lent document holds %v", document.Auths)
		}
		if lent, _ := runner.lent.Part(secrets.ValuePart); len(lent) == 0 || bytes.ContainsFunc(lent, func(r rune) bool { return r != 0 }) {
			t.Fatalf("the lent document survived the run (failed %v): %q", failed, lent)
		}
		if _, leaked := runner.request.Material["openshift-pull-secret"]; leaked || len(material) != 3 {
			t.Fatalf("the pull secret itself was lent or the caller's material changed: %v", slices.Sorted(maps.Keys(runner.request.Material)))
		}
		variables := map[string]secrets.Part{}
		for _, lent := range runner.request.Materials {
			variables[lent.Variable] = lent.Part
		}
		if len(variables) != 4+len(lifecycle.Materials(request.Placement)) || variables["releaseAuth"] != secrets.ValuePart || variables["imagePassword"] != secrets.PasswordPart || variables["imageTrust"] != secrets.CertificatePart {
			t.Fatalf("materials = %+v", runner.request.Materials)
		}
		if len(runner.request.Refusals) != 6 || runner.request.Deadline != releaseDeadline() {
			t.Fatalf("refusals %v, deadline %s", runner.request.Refusals, runner.request.Deadline)
		}
		evidence, _ := json.Marshal(runner.result.Evidence)
		if bytes.Contains(evidence, []byte("cXVheTp0b2tlbg==")) || bytes.Contains(evidence, []byte(mirror)) || bytes.Contains(block.Request, []byte("s3cret")) {
			t.Fatal("a record holds a credential")
		}
	}
	runner := &lendingRunner{}
	capability := NewReleases(runner, clusterReleases(releaseDigest), secretmaterial.RegistryAuth)
	_, _ = capability.Observe(context.Background(), lifecycle.Execution{Context: "lab", Block: block, Material: releaseMaterial()})
	if runner.document != nil || slices.ContainsFunc(runner.request.Materials, func(m lifecycle.MaterialFile) bool { return m.Secret == releaseAuthMaterial }) || len(runner.request.Materials) != len(request.Mirror.Materials())+len(lifecycle.Materials(request.Placement)) {
		t.Fatalf("the observation was lent %+v", runner.request.Materials)
	}
	released, _ := json.Marshal(ReleaseEvidence{Absent: true, Components: []ComponentEvidence{}, Postcondition: true, Request: block.RequestDigest, Version: ReleaseEvidenceVersion})
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: released}
	if result, err := capability.Destroy(context.Background(), lifecycle.Execution{Context: "lab", Block: block}); err != nil || result.Outcome != reconciliation.OutcomeUnchanged || len(runner.request.Materials) != len(lifecycle.Materials(request.Placement)) {
		t.Fatalf("destroy = %+v, %v, lent %+v", result, err, runner.request.Materials)
	}
	material := releaseMaterial()
	material["openshift-pull-secret"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"auths":{"registry.redhat.io":{"auth":"cmVkaGF0OnRva2Vu"}}}`)})
	runner.request = lifecycle.RunRequest{}
	_, err := capability.Apply(context.Background(), lifecycle.Execution{Context: "lab", Block: block, Material: material})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "secret.part" || runner.request.Implementation != "" || strings.Contains(reported[0].Message+reported[0].Remediation, "cmVkaGF0") {
		t.Fatalf("a pull secret without quay.io = %+v, ran %q", reported, runner.request.Implementation)
	}
	if classes, _ := capability.SecretClasses(block); classes.Of("openshift-pull-secret") != secrets.AccessCredential || classes.Of("mirror-ca") != secrets.TrustAnchor {
		t.Fatalf("classes = %+v", classes)
	}
}

// Every refusal the release fill names is its cluster's, classed as state
// when the mirror or the publisher answered something other than the pin and
// unknown when a read failed.
func TestTheReleaseFillNamesEachRefusalAsItsClusters(t *testing.T) {
	request, _ := releaseRequest(t, releaseDigest)
	want := map[string]string{"client-unreachable": "lifecycle.unknown", "client-digest-mismatch": "lifecycle.state", "release-unreachable": "lifecycle.unknown",
		"release-incomplete": "lifecycle.state", "release-digest-mismatch": "lifecycle.state", "mirror-unreadable": "lifecycle.unknown"}
	refusals := releaseRefusals("lab", request)
	if len(refusals) != len(want) {
		t.Fatalf("refusals = %v", slices.Sorted(maps.Keys(refusals)))
	}
	for reason, code := range want {
		reported := diagnostics.Of(refusals[reason])
		if len(reported) != 1 || reported[0].Code != code || reported[0].Object == nil || reported[0].Object.Kind != "OpenShiftCluster" || reported[0].Object.Name != "sno" ||
			!strings.Contains(reported[0].Remediation, "bootwright apply --context lab") {
			t.Errorf("%s = %+v", reason, reported)
		}
	}
}

func TestMirrorReleaseRequestAndEvidenceGolden(t *testing.T) {
	plan := plannedReleases(t, registryCatalog(), clusterReleases(releaseDigest))
	matchesGolden(t, "mirror-release-request", plan.Definitions[1].Request)
	digest, err := reconciliation.RequestDigest(plan.Definitions[1])
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := json.Marshal(ReleaseEvidence{Components: []ComponentEvidence{{Name: "cli", Digest: registryDigest}}, Digest: releaseDigest,
		Missing: 0, Postcondition: true, Request: digest, Version: ReleaseEvidenceVersion})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeReleaseEvidence(evidence, digest); err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "mirror-release-evidence", evidence)
}

// A cluster under the managed mirror with no pull secret, or whose release
// the controller stage selects no oc for, refuses naming what to set rather
// than leaving its image build waiting for a copy no block plans.
func TestAReleaseWithNoPullSecretOrClientRefusesNamingItsCluster(t *testing.T) {
	for code, change := range map[string]func(*Release){
		"api.required":    func(r *Release) { r.PullSecretRef = "" },
		"lifecycle.state": func(r *Release) { r.Client = ReleaseClientFor(releaseVersion, "") },
	} {
		releases := func(catalog api.Catalog) ([]Release, error) {
			listed, _ := clusterReleases("")(catalog)
			change(&listed[0])
			return listed, nil
		}
		_, err := NewReleases(nil, releases, secretmaterial.RegistryAuth).Requests(registryCatalog(), "controller", "lab")
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != code || !strings.Contains(reported[0].Message, "OpenShiftCluster/sno") || reported[0].Remediation == "" {
			t.Errorf("%s: %+v", code, reported)
		}
	}
}
