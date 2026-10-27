package ceph

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

var resolvedDigest = "sha256:" + strings.Repeat("c", 64)

func imageBlock(t *testing.T, cluster, authored string) reconciliation.Block {
	t.Helper()
	request := ImageRequest{Version: ImageVersion, Cluster: cluster, Controller: "controller", Release: PinnedRelease, Base: PinnedRepository, Tag: PinnedTag, Image: authored, Registry: PinnedRegistry, RegistryRef: "registry"}
	if strings.Contains(authored, "@") {
		request.Tag = ""
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{ID: ImageBlockID(cluster), Kind: string(api.CephCluster), Object: cluster, Implementation: ImageImplementation, ContentDigest: ImageContentDigest(), Request: canonical}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
}

func resolvedEvidence(t *testing.T, request, digest string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(ImageEvidence{Postcondition: true, Release: PinnedRelease, Base: PinnedRepository, Tag: PinnedTag, Digest: digest, Platform: PinnedPlatform, Request: request})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func imageProof(t *testing.T, cluster, authored, digest string) lifecycle.BlockEvidence {
	t.Helper()
	block := imageBlock(t, cluster, authored)
	evidence := resolvedEvidence(t, block.RequestDigest, digest)
	if strings.Contains(authored, "@") {
		evidence = bytes.Replace(evidence, []byte(`,"tag":"`+PinnedTag+`"`), nil, 1)
	}
	return lifecycle.BlockEvidence{Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone,
		Evidence: evidence, Block: block}
}

func TestPlanEmitsTheImageBlockEveryCephBlockDependsOn(t *testing.T) {
	catalog := pinnedCatalog(t, nil)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	var definitions []reconciliation.BlockDefinition
	runner := &recordingRunner{}
	image, err := NewImage(runner).Plan(context.Background(), input)
	if err != nil || len(image.Definitions) != 1 || !slices.Equal(image.Secrets, []string{"registry"}) {
		t.Fatalf("image plan=%+v err=%v", image, diagnostics.Of(err))
	}
	definitions = append(definitions, image.Definitions...)
	hosts, err := NewHost(runner).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	definitions = append(definitions, hosts.Definitions...)
	for _, kind := range []api.Kind{api.CephCluster, api.CephPool, api.CephClient} {
		plan, err := New(kind, runner).Plan(context.Background(), input)
		if err != nil {
			t.Fatal(kind, diagnostics.Of(err))
		}
		definitions = append(definitions, plan.Definitions...)
	}
	for _, kind := range []string{"Machine", "MachineInstallProfile"} {
		for _, name := range []string{"node-1", "node-2", "node-3", "rhel"} {
			definitions = append(definitions, reconciliation.BlockDefinition{ID: strings.ToLower(kind) + "-" + name, Description: "synthetic " + name, Stage: reconciliation.StageMachines, Kind: kind, Object: name, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: json.RawMessage(`{}`)})
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	blocks := map[string]reconciliation.Block{}
	for _, block := range plan.Blocks {
		blocks[block.ID] = block
	}
	resolved, ok := blocks["ceph-image-example"]
	if !ok || resolved.Implementation != ImageImplementation || resolved.Stage != reconciliation.StageClusters || resolved.Kind != string(api.CephCluster) || resolved.Object != "example" || len(resolved.Dependencies) != 0 {
		t.Fatalf("image block %+v", resolved)
	}
	request, err := DecodeImageRequest(resolved.Request)
	if err != nil || request.Controller != "controller" || request.Image != PinnedRepository+":"+PinnedTag || request.RegistryRef != "registry" {
		t.Fatalf("image request %+v %v", request, err)
	}
	for _, id := range []string{"ceph-cluster-example", "ceph-pool-volumes", "ceph-client-consumer"} {
		if !slices.Contains(blocks[id].Dependencies, "ceph-image-example") {
			t.Fatalf("%s does not depend on the image block: %v", id, blocks[id].Dependencies)
		}
	}
	if runner.calls != 0 {
		t.Fatal("planning reached the runner")
	}
}

func TestPrepareFillsTheImageFromTheOriginalResolutionWithoutResolvingAgain(t *testing.T) {
	r := coreRequest()
	first := executionFor(t, r)
	frozen := bytes.Clone(first.Block.Request)
	capability := New(api.CephCluster, nil)
	prepared, err := capability.prepare(context.Background(), first)
	if err != nil || prepared.Image != PinnedRepository+"@"+resolvedDigest {
		t.Fatalf("prepared image %q err=%v", prepared.Image, err)
	}
	continued := first
	continued.Operation, continued.Attempt = "apply-continued", 3
	again, err := capability.prepare(context.Background(), continued)
	if err != nil || again.Image != prepared.Image || again.FSID != prepared.FSID {
		t.Fatalf("continuation re-resolved: %q %v", again.Image, err)
	}
	if !bytes.Equal(first.Block.Request, frozen) {
		t.Fatal("prepare changed the frozen request")
	}
	decoded, err := DecodeRequest(frozen)
	if err != nil || decoded.Image != PinnedRepository+":"+PinnedTag {
		t.Fatalf("the frozen request lost its authored reference: %q %v", decoded.Image, err)
	}
	replanned, err := r.Canonical()
	if err != nil || !bytes.Equal(replanned, frozen) {
		t.Fatal("the request digest moved with the resolution")
	}
	fallback := first
	fallback.Proved, fallback.ApplyProved = fallback.ApplyProved, nil
	if current, err := capability.prepare(context.Background(), fallback); err != nil || current.Image != prepared.Image {
		t.Fatalf("current apply's resolution refused: %v", err)
	}
}

func TestPrepareRefusesMissingAmbiguousOrForeignResolution(t *testing.T) {
	for name, damage := range map[string]func(*lifecycle.Execution){
		"absent":    func(e *lifecycle.Execution) { e.ApplyProved = nil },
		"ambiguous": func(e *lifecycle.Execution) { e.ApplyProved = append(e.ApplyProved, e.ApplyProved[0]) },
		"another cluster": func(e *lifecycle.Execution) {
			e.ApplyProved[0] = imageProof(t, "other", PinnedRepository+":"+PinnedTag, resolvedDigest)
		},
		"another authored": func(e *lifecycle.Execution) {
			e.ApplyProved[0] = imageProof(t, "example", PinnedRepository+"@"+resolvedDigest, resolvedDigest)
		},
		"destroy proof":     func(e *lifecycle.Execution) { e.ApplyProved[0].Verb = reconciliation.Destroy },
		"unfinished apply":  func(e *lifecycle.Execution) { e.ApplyProved[0].State = reconciliation.BlockRunning },
		"foreign digest":    func(e *lifecycle.Execution) { e.ApplyProved[0].Block.RequestDigest = strings.Repeat("d", 64) },
		"other implementer": func(e *lifecycle.Execution) { e.ApplyProved[0].Implementation = Implementation },
		"unpinned platform": func(e *lifecycle.Execution) {
			e.ApplyProved[0].Evidence = bytes.Replace(e.ApplyProved[0].Evidence, []byte(PinnedPlatform), []byte("linux/arm64"), 1)
		},
		"non-hex resolved digest": func(e *lifecycle.Execution) {
			e.ApplyProved[0].Evidence = resolvedEvidence(t, e.ApplyProved[0].Block.RequestDigest, "sha256:"+strings.Repeat("g", 64))
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := executionFor(t, coreRequest())
			damage(&e)
			if _, err := New(api.CephCluster, nil).prepare(context.Background(), e); err == nil {
				t.Fatal("missing, ambiguous or foreign image resolution accepted")
			}
		})
	}
}

func TestAuthoredDigestMustEqualTheResolvedChildManifest(t *testing.T) {
	authored := PinnedRepository + "@" + resolvedDigest
	r := coreRequest()
	r.Image = authored
	e := executionFor(t, r)
	if prepared, err := New(api.CephCluster, nil).prepare(context.Background(), e); err != nil || prepared.Image != authored {
		t.Fatalf("matching authored digest refused: %v", err)
	}
	index := "sha256:" + strings.Repeat("e", 64)
	e.ApplyProved = []lifecycle.BlockEvidence{imageProof(t, r.Cluster, PinnedRepository+"@"+index, resolvedDigest)}
	r.Image = PinnedRepository + "@" + index
	e.Block.Request, _ = r.Canonical()
	if _, err := New(api.CephCluster, nil).prepare(context.Background(), e); len(diagnostics.Of(err)) != 1 || !strings.Contains(diagnostics.Of(err)[0].Message, "image-digest-mismatch") {
		t.Fatalf("index digest accepted: %v", diagnostics.Of(err))
	}
	block := imageBlock(t, "example", PinnedRepository+"@"+index)
	evidence := bytes.Replace(resolvedEvidence(t, block.RequestDigest, resolvedDigest), []byte(`,"tag":"`+PinnedTag+`"`), nil, 1)
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: evidence}}
	result, err := NewImage(runner).Apply(context.Background(), imageExecution(block))
	if d := diagnostics.Of(err); result.Outcome != reconciliation.OutcomeFailed || len(d) != 1 || !strings.Contains(d[0].Message, "image-digest-mismatch") || runner.calls != 1 {
		t.Fatalf("resolution of another digest accepted: %+v %v", result, err)
	}
	if imageRefusals()["image-digest-mismatch"] == nil {
		t.Fatal("the adapter's digest refusal is not remedied by name")
	}
}

func imageExecution(block reconciliation.Block) lifecycle.Execution {
	return lifecycle.Execution{ApplyOperation: "apply-original", Operation: "apply-original", Context: "example", Block: block,
		Material: map[string]secrets.Material{"registry": secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("cp"), secrets.PasswordPart: []byte("entitlement-key")})}}
}

func TestImageResolutionRunsOnTheControllerWithOnlyTheRegistryLoan(t *testing.T) {
	block := imageBlock(t, "example", PinnedRepository+":"+PinnedTag)
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: resolvedEvidence(t, block.RequestDigest, resolvedDigest)}}
	e := imageExecution(block)
	result, err := NewImage(runner).Apply(context.Background(), e)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged || runner.calls != 1 {
		t.Fatalf("resolution refused: %+v %v", result, err)
	}
	sent := runner.request
	if !sent.NativeErrors {
		t.Fatal("Ceph invocation did not opt in to typed native errors")
	}
	if sent.Implementation != ImageImplementation || sent.Operation != "apply" || sent.Placement != (machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"}) ||
		!bytes.Equal(sent.Canonical, block.Request) || sent.Deadline <= 0 || sent.Deadline > lifecycle.MaxDeadline {
		t.Fatalf("resolution request %+v", sent)
	}
	if len(sent.Materials) != 1 || sent.Materials[0].Variable != "registryJSON" || sent.Materials[0].Secret != registryMaterial {
		t.Fatalf("resolution material %+v", sent.Materials)
	}
	loaned, ok := sent.Material[registryMaterial].Part(secrets.ValuePart)
	if !ok || len(loaned) == 0 || bytes.ContainsFunc(loaned, func(r rune) bool { return r != 0 }) {
		t.Fatal("the registry loan outlived the run")
	}
	if strings.Contains(string(result.Evidence), "entitlement-key") {
		t.Fatal("evidence carries the credential")
	}
	for _, operation := range []string{"observe", "destroy"} {
		runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":true,"postcondition":true,"request":"` + block.RequestDigest + `"}`)}
		var effect reconciliation.EffectState
		if operation == "observe" {
			observation, err := NewImage(runner).Observe(context.Background(), e)
			if err != nil {
				t.Fatal(err)
			}
			effect = observation.Effect
		} else if result, err := NewImage(runner).Destroy(context.Background(), e); err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
			t.Fatalf("destroy %+v %v", result, err)
		}
		if runner.request.Operation != operation || len(runner.request.Materials) != 0 {
			t.Fatalf("%s lent material %+v", operation, runner.request.Materials)
		}
		if operation == "observe" && effect != reconciliation.EffectNoEffect {
			t.Fatalf("a lost resolution observed %q, want no effect so it repeats", effect)
		}
	}
	calls := runner.calls
	if observation, err := NewImage(runner).ObserveRemoval(context.Background(), e); err != nil || observation.Effect != reconciliation.EffectCompleted || runner.calls != calls {
		t.Fatalf("removal resolution ran or refused: %+v %v", observation, err)
	}
}

func TestImageApplyRefusesUnprovedResolution(t *testing.T) {
	block := imageBlock(t, "example", PinnedRepository+":"+PinnedTag)
	for name, evidence := range map[string]json.RawMessage{
		"empty":          json.RawMessage(`{}`),
		"another digest": resolvedEvidence(t, strings.Repeat("d", 64), resolvedDigest),
		"absent":         json.RawMessage(`{"absent":true,"postcondition":true,"request":"` + block.RequestDigest + `"}`),
		"arm64":          bytes.Replace(resolvedEvidence(t, block.RequestDigest, resolvedDigest), []byte(PinnedPlatform), []byte("linux/arm64"), 1),
		"another tag":    bytes.Replace(resolvedEvidence(t, block.RequestDigest, resolvedDigest), []byte(PinnedTag), []byte("latest"), 1),
		"unknown member": bytes.Replace(resolvedEvidence(t, block.RequestDigest, resolvedDigest), []byte(`{`), []byte(`{"index":"x",`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: evidence}}
			if result, err := NewImage(runner).Apply(context.Background(), imageExecution(block)); err == nil || result.Outcome == reconciliation.OutcomeChanged {
				t.Fatalf("unproved resolution accepted: %+v", result)
			}
		})
	}
}

func TestImageRequestGoldenIsTheAdapterCall(t *testing.T) {
	block := imageBlock(t, "example", PinnedRepository+":"+PinnedTag)
	data, err := json.Marshal(struct {
		Digest  string          `json:"digest"`
		Request json.RawMessage `json:"request"`
	}{block.RequestDigest, block.Request})
	if err != nil {
		t.Fatal(err)
	}
	matchesGoldenFile(t, filepath.Join("..", "..", "..", "ansible", "collections", "ansible_collections", "bootwright", "core", "tests", "fixtures", "ceph-image-call.golden"), data, false)
}

func TestAFailedResolutionKeepsItsAdapterOutputInTheAttempt(t *testing.T) {
	block := imageBlock(t, "example", PinnedRepository+":"+PinnedTag)
	e := imageExecution(block)
	e.Attempt = 2
	output := &bytes.Buffer{}
	e.Output = output
	refused := imageRefusals()["discovery"]
	runner := &failingRunner{err: refused}
	result, err := NewImage(runner).Apply(context.Background(), e)
	if err != refused || result.Outcome != reconciliation.OutcomeUnknown || runner.request.Output != output ||
		!strings.Contains(runner.request.OutputRemediation, "attempt-000002.output") || runner.request.Refusals["discovery"] == nil {
		t.Fatalf("a failed resolution lost its adapter output: %+v %v %q", result, err, runner.request.OutputRemediation)
	}
}

type failingRunner struct {
	err     error
	request lifecycle.RunRequest
}

func (r *failingRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.request = request
	return lifecycle.RunResult{}, r.err
}

func TestRegistryLoanUsesTheSelectedRegistryWithoutSubstitution(t *testing.T) {
	execution := imageExecution(imageBlock(t, "example", PinnedRepository+":"+PinnedTag))
	material, err := imageRegistryMaterial(execution, "registry", "registry.example.test/selected")
	if err != nil {
		t.Fatal(err)
	}
	defer material.Clear()
	data, ok := material.Part(secrets.ValuePart)
	if !ok {
		t.Fatal("missing registry material")
	}
	var value map[string]string
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if value["url"] != "registry.example.test/selected" || value["username"] != "cp" || value["password"] != "entitlement-key" {
		t.Fatal("selected registry material changed")
	}
}

func TestAFrozenImageRequestWhoseBaseIsTheRegistryHostDoesNotDecode(t *testing.T) {
	for base, refused := range map[string]bool{PinnedRegistry: true, PinnedRepository: false} {
		request := ImageRequest{Version: ImageVersion, Cluster: "example", Controller: "controller", Release: PinnedRelease, Base: base, Tag: PinnedTag, Image: base + ":" + PinnedTag, Registry: PinnedRegistry, RegistryRef: "registry"}
		canonical, err := request.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeImageRequest(canonical); (err != nil) != refused {
			t.Fatalf("base %q decoded with err=%v", base, err)
		}
	}
}

func TestImageRequestBindsTheEntitlementTrustBundle(t *testing.T) {
	plan := func(catalog api.Catalog) (lifecycle.CapabilityPlan, reconciliation.Block) {
		t.Helper()
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
		plan, err := NewImage(&recordingRunner{}).Plan(context.Background(), input)
		if err != nil || len(plan.Definitions) != 1 {
			t.Fatalf("image plan %+v %v", plan, diagnostics.Of(err))
		}
		digest, err := reconciliation.RequestDigest(plan.Definitions[0])
		if err != nil {
			t.Fatal(err)
		}
		return plan, reconciliation.Block{BlockDefinition: plan.Definitions[0], RequestDigest: digest}
	}
	plain, block := plan(pinnedCatalog(t, nil))
	if !slices.Equal(plain.Secrets, []string{"registry"}) || bytes.Contains(block.Request, []byte("registryTrustBundleRef")) {
		t.Fatalf("a plan without a bundle: %v %s", plain.Secrets, block.Request)
	}
	trusted, block := plan(withRegistryTrust(pinnedCatalog(t, nil)))
	request, err := DecodeImageRequest(block.Request)
	if err != nil || request.RegistryTrustBundleRef != "registry-ca" || !slices.Equal(trusted.Secrets, []string{"registry", "registry-ca"}) {
		t.Fatalf("a plan with a bundle: %+v %v %v", request, trusted.Secrets, err)
	}
	matchesGolden(t, "request-image-trust-bundle", block.Request, false)
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: resolvedEvidence(t, block.RequestDigest, resolvedDigest)}}
	e := imageExecution(block)
	if _, err := NewImage(runner).Apply(context.Background(), e); err == nil || runner.calls != 0 {
		t.Fatalf("resolution ran without its bound trust bundle: %v", err)
	}
	e.Material["registry-ca"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte(gatewayCertificatePEM)})
	if result, err := NewImage(runner).Apply(context.Background(), e); err != nil || result.Outcome != reconciliation.OutcomeUnchanged || runner.calls != 1 {
		t.Fatalf("resolution refused: %+v %v", result, diagnostics.Of(err))
	}
	want := []lifecycle.MaterialFile{{Name: "registry", Secret: registryMaterial, Part: secrets.ValuePart, Variable: "registryJSON"},
		{Name: "registry-ca", Secret: "registry-ca", Part: secrets.CertificatePart, Variable: "registryCA"}}
	if !slices.Equal(runner.request.Materials, want) {
		t.Fatalf("resolution material %+v", runner.request.Materials)
	}
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":true,"postcondition":true,"request":"` + block.RequestDigest + `"}`)}
	if _, err := NewImage(runner).Observe(context.Background(), e); err != nil || len(runner.request.Materials) != 0 {
		t.Fatalf("observe lent %+v %v", runner.request.Materials, err)
	}
}
