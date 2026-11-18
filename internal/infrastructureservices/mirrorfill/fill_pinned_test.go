package mirrorfill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

type fillSurface interface {
	Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error)
	Removal(context.Context, reconciliation.Block) (lifecycle.Removal, error)
	Apply(context.Context, lifecycle.Execution) (lifecycle.Result, error)
	Destroy(context.Context, lifecycle.Execution) (lifecycle.Result, error)
	Observe(context.Context, lifecycle.Execution) (lifecycle.Observation, error)
	ObserveRemoval(context.Context, lifecycle.Execution) (lifecycle.Observation, error)
	Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error)
	Unresolved(reconciliation.Verb, reconciliation.Block, json.RawMessage) (lifecycle.Unresolved, bool)
	SecretReferences(reconciliation.Block) ([]string, error)
	SecretClasses(reconciliation.Block) (secrets.Classes, error)
	Claimed(api.Catalog) []string
}

type pinnedKind struct {
	name     string
	build    func(Runner) fillSurface
	catalog  api.Catalog
	index    int
	material map[string]secrets.Material
	evidence func(scenario, digest string, request json.RawMessage) []byte
}

var pinnedScenarios = []string{"complete", "empty", "released", "partial", "foreign", "absent beside held", "other request"}

func mustMarshal(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}

func pinnedKinds() []pinnedKind {
	return []pinnedKind{
		{
			name:    "files",
			build:   func(runner Runner) fillSurface { return NewFiles(runner, clientArtifacts) },
			catalog: mirrorCatalog("mirror"),
			evidence: func(scenario, digest string, raw json.RawMessage) []byte {
				request, _ := DecodeFilesRequest(raw)
				helm, kubectl := FileEvidence{Path: request.Files[0].Path, Verified: true}, FileEvidence{Path: request.Files[1].Path, Verified: true}
				evidence := FilesEvidence{Request: digest, Version: FilesEvidenceVersion, Files: []FileEvidence{}}
				switch scenario {
				case "complete":
					evidence.Files, evidence.Postcondition = []FileEvidence{helm, kubectl}, true
				case "empty", "released":
					evidence.Absent = true
				case "partial":
					evidence.Files = []FileEvidence{kubectl}
				case "foreign":
					evidence.Files = []FileEvidence{{Path: "foreign/v1/file", Verified: true}}
				case "absent beside held":
					evidence.Absent, evidence.Files = true, []FileEvidence{helm}
				case "other request":
					evidence.Files, evidence.Postcondition, evidence.Request = []FileEvidence{helm, kubectl}, true, "other"
				}
				return mustMarshal(evidence)
			},
		},
		{
			name:    "images",
			build:   func(runner Runner) fillSurface { return NewImages(runner, pinnedImages) },
			catalog: registryCatalog(),
			evidence: func(scenario, digest string, raw json.RawMessage) []byte {
				request, _ := DecodeImagesRequest(raw)
				first := ImageEvidence{Repository: request.Images[0].Repository, Digest: request.Images[0].Digest, Verified: true}
				second := ImageEvidence{Repository: request.Images[1].Repository, Digest: request.Images[1].Digest, Verified: true}
				evidence := ImagesEvidence{Request: digest, Version: ImagesEvidenceVersion, Images: []ImageEvidence{}}
				switch scenario {
				case "complete":
					evidence.Images, evidence.Postcondition = []ImageEvidence{first, second}, true
				case "empty":
					evidence.Postcondition = true
				case "released":
					evidence.Absent, evidence.Postcondition = true, true
				case "partial":
					evidence.Images = []ImageEvidence{second}
				case "foreign":
					evidence.Images = []ImageEvidence{{Repository: first.Repository, Digest: sushyDigest, Verified: true}}
				case "absent beside held":
					evidence.Absent, evidence.Images = true, []ImageEvidence{first}
				case "other request":
					evidence.Images, evidence.Postcondition, evidence.Request = []ImageEvidence{first, second}, true, "other"
				}
				return mustMarshal(evidence)
			},
		},
		{
			name: "release",
			build: func(runner Runner) fillSurface {
				return NewReleases(runner, clusterReleases(releaseDigest), secretmaterial.RegistryAuth)
			},
			catalog:  registryCatalog(),
			index:    1,
			material: releaseMaterial(),
			evidence: func(scenario, digest string, raw json.RawMessage) []byte {
				component := ComponentEvidence{Name: "cli", Digest: registryDigest}
				evidence := ReleaseEvidence{Request: digest, Version: ReleaseEvidenceVersion, Components: []ComponentEvidence{}}
				switch scenario {
				case "complete":
					evidence.Digest, evidence.Components, evidence.Postcondition = releaseDigest, []ComponentEvidence{component}, true
				case "empty":
					evidence.Postcondition = true
				case "released":
					evidence.Absent, evidence.Postcondition = true, true
				case "partial":
					evidence.Digest, evidence.Components, evidence.Missing = releaseDigest, []ComponentEvidence{component}, 1
				case "foreign":
					evidence.Digest, evidence.Components, evidence.Postcondition = registryDigest, []ComponentEvidence{component}, true
				case "absent beside held":
					evidence.Absent, evidence.Digest = true, releaseDigest
				case "other request":
					evidence.Digest, evidence.Components, evidence.Postcondition, evidence.Request = releaseDigest, []ComponentEvidence{component}, true, "other"
				}
				return mustMarshal(evidence)
			},
		},
	}
}

func failureText(err error) string {
	if err == nil {
		return "nil"
	}
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return "plain: " + err.Error()
	}
	var parts []string
	for _, entry := range reported {
		object := "-"
		if entry.Object != nil {
			object = entry.Object.Kind + "/" + entry.Object.Name
		}
		parts = append(parts, fmt.Sprintf("%s|%s|%s|%s", entry.Code, entry.Message, entry.Remediation, object))
	}
	return strings.Join(parts, " ;; ")
}

func runSummary(runner *scriptedRunner) string {
	request := runner.request
	if request.Implementation == "" {
		return "no run"
	}
	digest := sha256.Sum256(request.Canonical)
	var materials []string
	for _, lent := range request.Materials {
		materials = append(materials, lent.Name+"/"+lent.Variable+"/"+string(lent.Part)+"/"+lent.Secret)
	}
	refusals := slices.Sorted(maps.Keys(request.Refusals))
	var messages []string
	for _, reason := range refusals {
		messages = append(messages, reason+"="+failureText(request.Refusals[reason]))
	}
	return fmt.Sprintf("run %s %s var=%s deadline=%s canonical=%s machine=%s materials=%v material=%v refusals=%s",
		request.Implementation, request.Operation, request.Variable, request.Deadline, hex.EncodeToString(digest[:6]), request.Placement.Machine,
		materials, slices.Sorted(maps.Keys(request.Material)), strings.Join(messages, " // "))
}

func pinnedTranscript(t *testing.T, kind pinnedKind) []byte {
	t.Helper()
	var out strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&out, format+"\n", args...) }
	ctx := context.Background()
	state := compilation.NewState(kind.catalog, kind.catalog, nil)
	input := func(verb reconciliation.Verb) lifecycle.PlanInput {
		return lifecycle.PlanInput{Verb: verb, Context: lifecycle.ContextIdentity{Name: "lab"}, State: state, Controller: "controller"}
	}
	surface := kind.build(nil)
	var block reconciliation.Block
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		plan, err := surface.Plan(ctx, input(verb))
		line("plan %s: err=%s definitions=%d secrets=%v", verb, failureText(err), len(plan.Definitions), plan.Secrets)
		for _, definition := range plan.Definitions {
			digest, err := reconciliation.RequestDigest(definition)
			if err != nil {
				t.Fatal(err)
			}
			line("  definition %s digest=%s", definition.ID, digest)
			indented, err := json.MarshalIndent(definition, "  ", "  ")
			if err != nil {
				t.Fatal(err)
			}
			line("%s", indented)
		}
		if verb == reconciliation.Apply {
			digest, err := reconciliation.RequestDigest(plan.Definitions[kind.index])
			if err != nil {
				t.Fatal(err)
			}
			block = reconciliation.Block{BlockDefinition: plan.Definitions[kind.index], RequestDigest: digest}
		}
	}
	noState := input(reconciliation.Apply)
	noState.State = nil
	plan, err := surface.Plan(ctx, noState)
	line("plan without state: err=%s definitions=%d secrets=%v", failureText(err), len(plan.Definitions), plan.Secrets)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	plan, err = surface.Plan(cancelled, input(reconciliation.Apply))
	line("plan cancelled: err=%v definitions=%d", err, len(plan.Definitions))

	removal, err := surface.Removal(ctx, block)
	line("removal: %q impacts=%q consumes=%q err=%s", removal.Description, removal.Impacts, removal.Consumes, failureText(err))
	_, err = surface.Removal(cancelled, block)
	line("removal cancelled: %v", err)
	broken := block
	broken.Request = json.RawMessage(`{`)
	_, err = surface.Removal(ctx, broken)
	line("removal of a broken request: %s", failureText(err))
	references, err := surface.SecretReferences(block)
	line("references: %v err=%s", references, failureText(err))
	_, err = surface.SecretReferences(broken)
	line("references of a broken request: %s", failureText(err))
	classes, err := surface.SecretClasses(block)
	line("classes: %v err=%s", classes, failureText(err))
	_, err = surface.SecretClasses(broken)
	line("classes of a broken request: %s", failureText(err))
	quiescence, err := surface.Quiescent(ctx, lifecycle.Probe{})
	line("quiescent: %+v err=%v", quiescence, err)
	line("claimed: %v; none declared: %v", surface.Claimed(kind.catalog), surface.Claimed(api.NewCatalog(nil)))

	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		for _, scenario := range pinnedScenarios {
			data := kind.evidence(scenario, block.RequestDigest, block.Request)
			unresolved, ok := surface.Unresolved(verb, block, data)
			line("unresolved %s %s: %v %+v", verb, scenario, ok, unresolved)
		}
		_, ok := surface.Unresolved(verb, block, json.RawMessage(`{`))
		line("unresolved %s garbage: %v", verb, ok)
		_, ok = surface.Unresolved(verb, broken, nil)
		line("unresolved %s broken request: %v", verb, ok)
	}

	execution := func() lifecycle.Execution {
		return lifecycle.Execution{Context: "lab", Block: block, Material: maps.Clone(kind.material)}
	}
	operations := []struct {
		name string
		call func(fillSurface, lifecycle.Execution) (string, error)
	}{
		{"apply", func(s fillSurface, e lifecycle.Execution) (string, error) {
			result, err := s.Apply(ctx, e)
			return fmt.Sprintf("outcome=%s evidence=%s", result.Outcome, result.Evidence), err
		}},
		{"destroy", func(s fillSurface, e lifecycle.Execution) (string, error) {
			result, err := s.Destroy(ctx, e)
			return fmt.Sprintf("outcome=%s evidence=%s", result.Outcome, result.Evidence), err
		}},
		{"observe", func(s fillSurface, e lifecycle.Execution) (string, error) {
			result, err := s.Observe(ctx, e)
			return fmt.Sprintf("effect=%s evidence=%s", result.Effect, result.Evidence), err
		}},
		{"observe removal", func(s fillSurface, e lifecycle.Execution) (string, error) {
			result, err := s.ObserveRemoval(ctx, e)
			return fmt.Sprintf("effect=%s evidence=%s", result.Effect, result.Evidence), err
		}},
	}
	for _, operation := range operations {
		for _, scenario := range pinnedScenarios {
			for _, outcome := range []reconciliation.Outcome{reconciliation.OutcomeChanged, reconciliation.OutcomeUnchanged, reconciliation.OutcomeFailed} {
				runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: outcome, Evidence: kind.evidence(scenario, block.RequestDigest, block.Request)}}
				text, err := operation.call(kind.build(runner), execution())
				line("%s %s %s: %s err=%s", operation.name, scenario, outcome, text, failureText(err))
				if scenario == "complete" && outcome == reconciliation.OutcomeChanged {
					line("  %s", runSummary(runner))
				}
			}
		}
		garbage := &scriptedRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{`)}}
		text, err := operation.call(kind.build(garbage), execution())
		line("%s garbage evidence: %s err=%s", operation.name, text, failureText(err))
		for _, failed := range []struct {
			name    string
			failure error
		}{
			{"plain", fmt.Errorf("adapter stopped")},
			{"unknown", unprovedStop.Fail("adapter lost", "retry")},
			{"state", diagnostics.NewFailureWithRemediation("lifecycle.state", "adapter refused", "", "fix")},
		} {
			name, failure := failed.name, failed.failure
			failing := &scriptedRunner{err: failure}
			text, err := operation.call(kind.build(failing), execution())
			line("%s runner error %s: %s err=%s", operation.name, name, text, failureText(err))
		}
		text, err = operation.call(kind.build(nil), execution())
		line("%s unconfigured runner: %s err=%s", operation.name, text, failureText(err))
		foreign := execution()
		foreign.Block.Object = "someone-else"
		unrun := &scriptedRunner{}
		text, err = operation.call(kind.build(unrun), foreign)
		line("%s foreign block object: %s err=%s %s", operation.name, text, failureText(err), runSummary(unrun))
		foreign = execution()
		foreign.Block.Kind = "Machine"
		text, err = operation.call(kind.build(unrun), foreign)
		line("%s foreign block kind: %s err=%s", operation.name, text, failureText(err))
		undecodable := execution()
		undecodable.Block.Request = json.RawMessage(`{`)
		text, err = operation.call(kind.build(unrun), undecodable)
		line("%s undecodable request: %s err=%s", operation.name, text, failureText(err))
		text, err = operation.call(kind.build(unrun), lifecycle.Execution{})
		line("%s empty execution: %s err=%s", operation.name, text, failureText(err))
	}
	return []byte(out.String())
}

var unprovedStop = diagnostics.Stop{Code: "test.unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved, Cause: diagnostics.CauseUnclassified,
	Recovery: diagnostics.RecoveryRepeat, Meaning: "a test adapter lost its outcome", Remedy: "retry"}

// The three fill capabilities are pinned through their public surface, one
// transcript each: every plan, refusal, outcome, effect, unresolved reading,
// removal, secret reference and class, and each run request, so a change to
// the shared lifecycle moves a golden before it moves a request.
func TestTheFillCapabilitiesKeepTheirPinnedBehaviour(t *testing.T) {
	for _, kind := range pinnedKinds() {
		t.Run(kind.name, func(t *testing.T) {
			matchesTextGolden(t, "fill-behaviour-"+kind.name, pinnedTranscript(t, kind))
		})
	}
}

func TestTheFillCapabilitiesRefuseACancelledContextBeforeAnyRun(t *testing.T) {
	for _, kind := range pinnedKinds() {
		plan, err := kind.build(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "lab"},
			State: compilation.NewState(kind.catalog, kind.catalog, nil), Controller: "controller"})
		if err != nil {
			t.Fatal(err)
		}
		digest, _ := reconciliation.RequestDigest(plan.Definitions[kind.index])
		block := reconciliation.Block{BlockDefinition: plan.Definitions[kind.index], RequestDigest: digest}
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		runner := &scriptedRunner{}
		surface := kind.build(runner)
		execution := lifecycle.Execution{Context: "lab", Block: block, Material: kind.material}
		_, applyErr := surface.Apply(cancelled, execution)
		_, observeErr := surface.Observe(cancelled, execution)
		_, removalErr := surface.ObserveRemoval(cancelled, execution)
		_, removes := surface.Removal(cancelled, block)
		for name, err := range map[string]error{"apply": applyErr, "observe": observeErr, "observe removal": removalErr, "removal": removes} {
			if err != context.Canceled {
				t.Errorf("%s %s: %v", kind.name, name, err)
			}
		}
		if runner.request.Implementation != "" {
			t.Errorf("%s: a cancelled context ran %s", kind.name, runner.request.Implementation)
		}
	}
}
