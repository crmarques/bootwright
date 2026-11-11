package mirrorfill

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	helmDigest    = "1111111111111111111111111111111111111111111111111111111111111111"
	kubectlDigest = "2222222222222222222222222222222222222222222222222222222222222222"
	mirrorURL     = "https://203.0.113.20/mirror/lab"
)

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}
func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func mirrorCatalog(host string) api.Catalog {
	return api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			field("mirror", api.MapValue(field("artifacts", api.MapValue(text("serverRef", "mirror"), text("endpointRef", "ip"))))),
			field("controller", api.MapValue(text("machineRef", "controller"))))),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(field("access", api.MapValue(field("local", api.BoolValue(true)))))),
		api.NewObject(api.Machine, "mirror", api.Value{}, api.MapValue(
			field("proxy", api.MapValue(field("direct", api.MapValue()))),
			field("network", api.MapValue(field("addresses", api.ListValue(api.MapValue(text("name", "ip"), text("address", "203.0.113.20")))))),
			field("access", api.MapValue(field("ssh", api.MapValue(text("addressRef", "ip"), text("knownHostsRef", "mirror-known-hosts"),
				field("auth", api.MapValue(text("privateKeyRef", "mirror-access-key"))))))))),
		api.NewObject(api.ArtifactServer, "mirror", api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", host),
			field("tls", api.MapValue(text("secretRef", "mirror-tls"))),
			field("listeners", api.ListValue(api.MapValue(text("name", "https"), text("protocol", "https"), field("port", api.IntegerValue("443"))))),
			field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("listenerRef", "https"), text("addressRef", "ip")))))),
	})
}

func clientArtifacts(api.Catalog) ([]Artifact, error) {
	return []Artifact{
		{Bytes: 7, Location: mirrorURL + "/kubectl/v1.37.2/kubectl", SHA256: kubectlDigest, Source: "https://dl.k8s.io/release/v1.37.2/bin/linux/amd64/kubectl"},
		{Bytes: 5, Location: mirrorURL + "/helm/v4.3.1/helm-v4.3.1-linux-amd64.tar.gz", SHA256: helmDigest, Source: "https://get.helm.sh/helm-v4.3.1-linux-amd64.tar.gz"},
		{Bytes: 9, Location: "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.21.15/openshift-client-linux-4.21.15.tar.gz", SHA256: helmDigest, Source: "https://mirror.openshift.com/x"},
	}, nil
}

func planned(t *testing.T, catalog api.Catalog, artifacts Artifacts) lifecycle.CapabilityPlan {
	t.Helper()
	plan, err := NewFiles(nil, artifacts).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "lab"},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return plan
}

// The fill plans one block for the Environment's managed mirror, beneath the
// server's served root, waiting for the server's own block alone, with every
// client the mirror serves at the path the controller stage reads it from, in
// path order; a client of a vendor origin, read elsewhere, is not filled.
func TestTheFillPlacesEachMirroredClientBeneathTheServersMirrorRoot(t *testing.T) {
	plan := planned(t, mirrorCatalog("mirror"), clientArtifacts)
	if len(plan.Definitions) != 1 {
		t.Fatalf("definitions = %+v", plan.Definitions)
	}
	definition := plan.Definitions[0]
	if definition.ID != "mirror-files-lab" || definition.Kind != "ArtifactServer" || definition.Object != "mirror" || definition.Family != Family ||
		!slices.Equal(definition.Requires, []reconciliation.ObjectRef{{Kind: "ArtifactServer", Object: "mirror", Family: "managed-service"}}) {
		t.Fatalf("definition = %+v", definition)
	}
	if !slices.Equal(plan.Secrets, []string{"mirror-access-key", "mirror-known-hosts"}) {
		t.Errorf("secrets = %v", plan.Secrets)
	}
	request, err := DecodeFilesRequest(definition.Request)
	if err != nil {
		t.Fatal(err)
	}
	if request.Root != "/var/lib/bootwright-hosted/lab/services/artifact-server/mirror/public/mirror/lab" || request.Placement.Machine != "mirror" || request.Placement.Address != "203.0.113.20" {
		t.Fatalf("request = %+v", request)
	}
	var paths []string
	for _, file := range request.Files {
		paths = append(paths, file.Path)
	}
	if !slices.Equal(paths, []string{"helm/v4.3.1/helm-v4.3.1-linux-amd64.tar.gz", "kubectl/v1.37.2/kubectl"}) || request.Files[0].SHA256 != helmDigest || request.Files[0].Bytes != 5 {
		t.Fatalf("files = %+v", request.Files)
	}
}

// No managed mirror, or one that serves no client, plans nothing.
func TestAnOperatorFilledMirrorOrAnEmptyListPlansNoFill(t *testing.T) {
	catalog := mirrorCatalog("mirror")
	environment, _ := catalog.Find(api.Environment, "lab")
	external := api.NewCatalog(append(slices.DeleteFunc(catalog.Objects(), func(object api.Object) bool { return object.Kind() == api.Environment }),
		environment.WithSpec(environment.Spec().With("mirror", api.MapValue(field("artifacts", api.MapValue(text("url", mirrorURL))))))))
	if plan := planned(t, external, clientArtifacts); len(plan.Definitions) != 0 {
		t.Fatalf("an operator-filled mirror planned %+v", plan.Definitions)
	}
	none := func(api.Catalog) ([]Artifact, error) { return nil, nil }
	if plan := planned(t, catalog, none); len(plan.Definitions) != 0 {
		t.Fatalf("an empty acquisition list planned %+v", plan.Definitions)
	}
}

// A frozen request whose file would leave the mirror root, or names no
// digest, never decodes, so no adapter runs it.
func TestAFrozenRequestThatCouldLeaveItsRootRefuses(t *testing.T) {
	plan := planned(t, mirrorCatalog("mirror"), clientArtifacts)
	request, err := DecodeFilesRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*FilesRequest){
		"parent":    func(r *FilesRequest) { r.Files[0].Path = "../escape/file" },
		"absolute":  func(r *FilesRequest) { r.Files[0].Path = "/etc/passwd" },
		"single":    func(r *FilesRequest) { r.Files[0].Path = "file" },
		"digest":    func(r *FilesRequest) { r.Files[0].SHA256 = "abc" },
		"plaintext": func(r *FilesRequest) { r.Files[0].Source = "http://get.helm.sh/helm" },
		"duplicate": func(r *FilesRequest) { r.Files[1].Path = r.Files[0].Path },
		"outside":   func(r *FilesRequest) { r.Root = "/var/lib/other" },
	} {
		changed := request
		changed.Files = slices.Clone(request.Files)
		mutate(&changed)
		data, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeFilesRequest(data); len(diagnostics.Of(err)) == 0 {
			t.Errorf("%s: the request decoded", name)
		}
	}
}

func filesRequest(t *testing.T) (FilesRequest, reconciliation.Block) {
	t.Helper()
	plan := planned(t, mirrorCatalog("mirror"), clientArtifacts)
	request, err := DecodeFilesRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := reconciliation.RequestDigest(plan.Definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	return request, reconciliation.Block{BlockDefinition: plan.Definitions[0], RequestDigest: digest}
}

// The fill's evidence names each file the mirror holds, in request order:
// all of them verified is the apply's completion and the removal's no effect,
// none of them the apply's no effect and the removal's completion, any other
// subset partial, and a file the request does not name in that place, or a
// root reported absent beside held files, unknown.
func TestTheFillReadsItsEffectFromTheFilesTheMirrorHolds(t *testing.T) {
	request, _ := filesRequest(t)
	helm, kubectl := FileEvidence{Path: request.Files[0].Path, Verified: true}, FileEvidence{Path: request.Files[1].Path, Verified: true}
	cases := []struct {
		name           string
		evidence       FilesEvidence
		apply, removal reconciliation.EffectState
	}{
		{"complete", FilesEvidence{Files: []FileEvidence{helm, kubectl}, Postcondition: true}, reconciliation.EffectCompleted, reconciliation.EffectNoEffect},
		{"empty", FilesEvidence{Absent: true}, reconciliation.EffectNoEffect, reconciliation.EffectCompleted},
		{"one", FilesEvidence{Files: []FileEvidence{kubectl}}, reconciliation.EffectPartial, reconciliation.EffectPartial},
		{"unverified", FilesEvidence{Files: []FileEvidence{{Path: helm.Path}, kubectl}}, reconciliation.EffectPartial, reconciliation.EffectPartial},
		{"foreign", FilesEvidence{Files: []FileEvidence{{Path: "foreign/v1/file", Verified: true}}}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		{"reordered", FilesEvidence{Files: []FileEvidence{kubectl, helm}, Postcondition: true}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
		{"absent root", FilesEvidence{Absent: true, Files: []FileEvidence{helm}}, reconciliation.EffectUnknown, reconciliation.EffectUnknown},
	}
	for _, test := range cases {
		if got := filesEffect(test.evidence, request, false); got != test.apply {
			t.Errorf("%s: apply effect %s, want %s", test.name, got, test.apply)
		}
		if got := filesEffect(test.evidence, request, true); got != test.removal {
			t.Errorf("%s: removal effect %s, want %s", test.name, got, test.removal)
		}
	}
}

type scriptedRunner struct {
	request lifecycle.RunRequest
	result  lifecycle.RunResult
	err     error
}

func (r *scriptedRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.request = request
	return r.result, r.err
}

// A fill whose adapter named a digest mismatch fails with the diagnostic the
// capability gave that refusal, so a file whose SHA-256 differs is reported as
// such and never published; evidence that does not prove every file verified
// leaves the attempt unknown.
func TestAFillRefusesAFileItCouldNotVerify(t *testing.T) {
	request, block := filesRequest(t)
	runner := &scriptedRunner{}
	capability := NewFiles(runner, clientArtifacts)
	execution := lifecycle.Execution{Context: "lab", Block: block}
	refused := filesRefusals("lab", request)["file-digest-mismatch"]
	runner.err = refused
	result, err := capability.Apply(context.Background(), execution)
	if !errors.Is(err, refused) || result.Outcome == reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v, %v", result, err)
	}
	reported := diagnostics.Of(refused)
	if len(reported) != 1 || !strings.Contains(reported[0].Message, "SHA-256") || reported[0].Object == nil || reported[0].Object.Name != "mirror" {
		t.Fatalf("refusal = %+v", reported)
	}
	partial, _ := json.Marshal(FilesEvidence{Files: []FileEvidence{{Path: request.Files[0].Path}}, Request: block.RequestDigest, Version: FilesEvidenceVersion})
	runner.err, runner.result = nil, lifecycle.RunResult{Outcome: "changed", Evidence: partial}
	if result, err := capability.Apply(context.Background(), execution); err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("an unproved fill = %+v, %v", result, err)
	}
	complete, _ := json.Marshal(FilesEvidence{Files: []FileEvidence{{Path: request.Files[0].Path, Verified: true}, {Path: request.Files[1].Path, Verified: true}},
		Postcondition: true, Request: block.RequestDigest, Version: FilesEvidenceVersion})
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: complete}
	if result, err := capability.Apply(context.Background(), execution); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("a replay that copied nothing = %+v, %v", result, err)
	}
	if runner.request.Placement.Machine != "mirror" || len(runner.request.Refusals) != 2 {
		t.Fatalf("the run = %+v", runner.request)
	}
}

// The fill runs without the controller stage's clients, so the controller
// block may wait for it; its removal reads only its frozen request.
func TestTheFillPrecedesTheControllerStageAndItsRemovalReadsItsRequest(t *testing.T) {
	_, block := filesRequest(t)
	if !(Files{}).PrecedesController(block.BlockDefinition) {
		t.Fatal("the fill does not precede the controller stage")
	}
	removal, err := (Files{}).Removal(context.Background(), block)
	if err != nil || removal.Description != "remove the artifact mirror files of lab from mirror" ||
		!slices.Equal(removal.Impacts, []string{"remove-content /var/lib/bootwright-hosted/lab/services/artifact-server/mirror/public/mirror/lab"}) {
		t.Fatalf("removal = %+v, %v", removal, err)
	}
}

// The frozen request and the evidence the fill publishes are pinned byte for
// byte, every member populated, and each golden reads back through this
// package's own decoders.
func TestMirrorFilesRequestAndEvidenceGolden(t *testing.T) {
	catalog := mirrorCatalog("mirror")
	machine, _ := catalog.Find(api.Machine, "mirror")
	proxied := api.NewCatalog(append(slices.DeleteFunc(catalog.Objects(), func(object api.Object) bool { return object.Kind() == api.Machine && object.Name() == "mirror" }),
		machine.WithSpec(machine.Spec().With("proxy", api.MapValue(text("proxyRef", "egress"), field("noProxy", api.StringList(".lab.example.test"))))),
		api.NewObject(api.Proxy, "egress", api.Value{}, api.MapValue(text("management", "external"),
			field("connection", api.MapValue(text("httpProxy", "http://proxy.example.test:3128"), text("httpsProxy", "http://proxy.example.test:3128")))))))
	plan := planned(t, proxied, clientArtifacts)
	request, err := DecodeFilesRequest(plan.Definitions[0].Request)
	if err != nil || request.Egress.HTTPSProxy == "" || len(request.Egress.NoProxy) == 0 {
		t.Fatalf("request = %+v, %v", request, err)
	}
	matchesGolden(t, "mirror-files-request", plan.Definitions[0].Request)
	digest, err := reconciliation.RequestDigest(plan.Definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := json.Marshal(FilesEvidence{Absent: true, Files: []FileEvidence{{Path: request.Files[0].Path, Verified: true}}, Postcondition: true, Request: digest, Version: FilesEvidenceVersion})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFilesEvidence(evidence, digest); err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "mirror-files-evidence", evidence)
}

// Every run of a fill is bounded by a deadline that covers each file's stalled
// read, the role's download timeout, with room for the transfer around it, and
// stays within the ceiling the runner holds every request to.
func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	request, block := filesRequest(t)
	for _, files := range []int{1, len(request.Files), MaxFiles} {
		sized := request
		sized.Files = slices.Repeat([]File{request.Files[0]}, files)
		deadline := filesDeadline(sized)
		if deadline <= time.Duration(files)*FileStall || deadline > lifecycle.MaxDeadline {
			t.Errorf("%d files: deadline %s", files, deadline)
		}
	}
	runner := &scriptedRunner{err: errors.New("stopped")}
	_, _ = NewFiles(runner, clientArtifacts).Apply(context.Background(), lifecycle.Execution{Context: "lab", Block: block})
	if runner.request.Deadline != filesDeadline(request) {
		t.Fatalf("the run's deadline is %s, not the request's %s", runner.request.Deadline, filesDeadline(request))
	}
}
