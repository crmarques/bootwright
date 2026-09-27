//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"net"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// contractRow is one binding buildCapabilities offers and the example that
// plans blocks for it.
type contractRow struct {
	kind           string
	implementation string
	example        string
	// probes marks the one Quiescent that observes through the runner.
	probes bool
	// absence is what this binding's absence evidence carries beyond the
	// fields every binding's shares.
	absence map[string]any
	// presence is every field but the request digest of evidence that proves
	// the attempt's frozen block realized, built from that block's own request
	// and the material the attempt binds. Every runner-driven row has one, so
	// the digest and kind checks are held to the contract on evidence that
	// passes every other check. With its postcondition unproved it is the
	// row's partial realization.
	presence func(*testing.T, lifecycle.Execution) map[string]any
	// retains marks a binding whose own spec has its removal retain what its
	// apply realized, so its absence evidence proves that removal and not that
	// the apply had no effect, and an observation leaves it unknown:
	// specs/managed-os.md keeps the installed system with the Machine's disks,
	// and specs/substrates.md retains a claimed physical machine.
	retains bool
	// noEffect is what this binding's positive no-effect evidence carries
	// beyond its request digest, when its removal's absence does not prove
	// it: specs/managed-os.md proves an installation had no effect from a
	// powered-off Machine with no marker and no published content.
	noEffect map[string]any
	// noEffectUnreported says why a retaining binding names no no-effect
	// evidence: its spec proves no effect from something its adapter does not
	// report. Every retaining row names exactly one of the two.
	noEffectUnreported string
	// indivisible marks a binding whose block realizes nothing it could leave
	// part way, as specs/substrates.md says of a physical machine, whose
	// replay finds nothing realized that could drift, so an observation leaves
	// partial evidence unknown.
	indivisible bool
}

func (r contractRow) binding() lifecycle.CapabilityBinding {
	return lifecycle.CapabilityBinding{Kind: r.kind, Implementation: r.implementation}
}

func contractRows() []contractRow {
	return []contractRow{
		{kind: clients.Kind, implementation: clients.Implementation, example: "lab-rhel"},
		{kind: libvirt.MachineKind, implementation: libvirt.MachineImplementation, example: "lab-rhel", probes: true,
			absence: map[string]any{"answered": true}, presence: contractMachinePresence},
		{kind: baremetal.Kind, implementation: baremetal.Implementation, example: "lab-baremetal", presence: contractBareMetalPresence,
			retains: true, indivisible: true,
			noEffectUnreported: "specs/substrates.md proves no effect only from a claim never published, which the controller's reservations hold, not the adapter's evidence"},
		{kind: installation.Kind, implementation: installation.Implementation, example: "lab-rhel", presence: contractInstallationPresence, retains: true,
			noEffect: map[string]any{"power": "Off"}},
		{kind: libvirt.HostKind, implementation: libvirt.HostImplementation, example: "lab-rhel", presence: contractHostPresence},
		{kind: agentinstall.Kind, implementation: agentinstall.MediaImplementation, example: "lab-sno", presence: contractMediaPresence},
		{kind: agentinstall.Kind, implementation: agentinstall.InstallImplementation, example: "lab-sno", presence: contractClusterPresence},
		{kind: string(proxy.Definition().Kind), implementation: proxy.Definition().Implementation, example: "lab-rhel", presence: contractServicePresence},
		{kind: string(dnsserver.Definition().Kind), implementation: dnsserver.Definition().Implementation, example: "lab-rhel", presence: contractServicePresence},
		{kind: string(ntpserver.Definition().Kind), implementation: ntpserver.Definition().Implementation, example: "lab-rhel", presence: contractServicePresence},
		{kind: artifactserver.Kind, implementation: artifactserver.Implementation, example: "lab-rhel", presence: contractArtifactServerPresence},
	}
}

// contractPlanningOnly is the one exemption from the effect checks: the
// controller stage runs no adapter, exactly as
// TestBoundPlaybooksCoverExactlyTheOfferedCapabilities exempts it, so there is
// no runner request of its to hold to the contract.
func contractPlanningOnly(row contractRow) bool {
	return row.kind == clients.Kind && row.implementation == clients.Implementation
}

// contractKnownDeviations names every contract property a binding fails today,
// by "<implementation>/<property>", and the backlog item or candidate that
// repairs it. It is exact: a failure missing from it, an entry whose property
// now holds and an entry naming no binding or property all fail, so it only
// shrinks.
func contractKnownDeviations() map[string]string {
	return map[string]string{}
}

// contractProperties is every property the suite holds a binding to.
var contractProperties = []string{
	"plan-deterministic", "plan-identity", "plan-canonical", "planning-runs-nothing",
	"removal-reads-frozen-block", "removal-refuses-malformed-request",
	"apply-request", "observe-request", "destroy-request",
	"apply-outcome-of-runner-error", "destroy-outcome-of-runner-error",
	"apply-evidence-proves-request", "destroy-evidence-proves-request", "observe-evidence-proves-request",
	"apply-evidence-proves-presence", "destroy-evidence-proves-absence",
	"observe-absence-proves-no-effect", "observe-no-effect-proves-no-effect", "observe-partial-proves-partial",
	"observe-unproved", "quiescence-unproved",
}

// contractRunner is a scripted runner: it records every request and answers
// each with the one result it was given.
type contractRunner struct {
	mutex    sync.Mutex
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *contractRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	request.Canonical = slices.Clone(request.Canonical)
	r.requests = append(r.requests, request)
	return lifecycle.RunResult{Outcome: r.result.Outcome, Evidence: slices.Clone(r.result.Evidence)}, r.err
}

// script forgets every earlier request and answers the next ones with result
// and err.
func (r *contractRunner) script(result lifecycle.RunResult, err error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.requests, r.result, r.err = nil, result, err
}

func (r *contractRunner) sent() []lifecycle.RunRequest {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return slices.Clone(r.requests)
}

// contractExample is one example compiled and planned by every binding.
type contractExample struct {
	state   *compilation.State
	apply   lifecycle.PlanInput
	destroy lifecycle.PlanInput
	plan    reconciliation.Plan
	plans   map[lifecycle.CapabilityBinding]lifecycle.CapabilityPlan
}

func contractExampleOf(t *testing.T, resolver capabilityResolver, name string) contractExample {
	t.Helper()
	state, _ := compileAcceptance(t, exampleDirectory(t, name))
	example := contractExample{
		state:   state,
		apply:   lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: name}, State: state, Controller: "controller"},
		destroy: lifecycle.PlanInput{Verb: reconciliation.Destroy, Context: lifecycle.ContextIdentity{Name: name}, State: state, Controller: "controller"},
		plans:   map[lifecycle.CapabilityBinding]lifecycle.CapabilityPlan{},
	}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
		contribution, err := capability.Plan(context.Background(), example.apply)
		if err != nil {
			// A binding may refuse an example it has no row for, as the
			// installation refuses lab-baremetal's physical Machine.
			continue
		}
		example.plans[binding] = contribution
		definitions = append(definitions, contribution.Definitions...)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatalf("%s: the plan model refuses what the bindings planned: %v", name, err)
	}
	example.plan = plan
	return example
}

// frozen is every block the plan froze for one binding.
func (e contractExample) frozen(binding lifecycle.CapabilityBinding) []reconciliation.Block {
	var blocks []reconciliation.Block
	for _, block := range e.plan.Blocks {
		if block.Kind == binding.Kind && block.Implementation == binding.Implementation {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// contractFindings collects which property each binding broke.
type contractFindings struct {
	failures map[string][]string
}

func (f *contractFindings) record(row contractRow, property, message string) {
	key := row.implementation + "/" + property
	f.failures[key] = append(f.failures[key], message)
}

// TestEveryCapabilityHonoursTheCapabilityContract runs one contract against
// every binding this build offers. Planning is pure, repeatable and canonical
// and runs nothing; a removal reads only its own frozen block. Every
// runner-driven binding sends exactly the frozen request under the block's own
// identity and a bounded deadline, keeps a lost or unknown-flagged runner
// failure unknown, accepts no outcome whose evidence does not prove this very
// request, and never reads a failed or empty observation, or one of another
// request, as proof. Evidence naming another request is the binding's own
// proof with only its digest swapped, so the digest check alone refuses it.
// Evidence is held to its kind as well: under this very request an apply
// accepts only presence and a removal only absence, and an observation never
// reads absence or a partial realization as completion but as positive no
// effect and partial; a binding whose removal's absence proves only that
// removal is held to its own no-effect evidence instead, and one its spec
// leaves with no partial state reads a partial realization as unknown. A
// failed or empty probe never reads as quiescent.
func TestEveryCapabilityHonoursTheCapabilityContract(t *testing.T) {
	runner := &contractRunner{}
	resolver := buildCapabilitiesWith(systemClock{}, exampleControllerPorts(t), runner)
	examples := map[string]contractExample{}
	findings := &contractFindings{failures: map[string][]string{}}
	for _, row := range contractRows() {
		example, built := examples[row.example]
		if !built {
			example = contractExampleOf(t, resolver, row.example)
			examples[row.example] = example
		}
		capability, ok := resolver.Resolve(row.kind, row.implementation)
		if !ok {
			t.Fatalf("%s/%s does not resolve", row.kind, row.implementation)
		}
		if len(example.frozen(row.binding())) == 0 {
			t.Fatalf("%s plans no %s block", row.example, row.implementation)
		}
		contractPlanning(t, row, example, capability, runner, findings)
		if row.retains && (row.noEffect == nil) == (row.noEffectUnreported == "") {
			t.Fatalf("%s retains what its apply realized, so its row names exactly one of its no-effect evidence and why its adapter reports none", row.implementation)
		}
		if !contractPlanningOnly(row) && row.presence == nil {
			t.Fatalf("%s drives the runner but its row builds no presence evidence to hold its digest check to", row.implementation)
		}
		if !contractPlanningOnly(row) {
			contractEffects(t, row, example, capability, runner, findings)
		}
	}
	contractCompare(t, findings.failures, contractKnownDeviations())
}

// contractCompare holds the findings to the known deviations exactly.
func contractCompare(t *testing.T, failures map[string][]string, known map[string]string) {
	t.Helper()
	implementations := map[string]bool{}
	for _, row := range contractRows() {
		implementations[row.implementation] = true
	}
	for key, reason := range known {
		implementation, property, _ := strings.Cut(key, "/")
		if !implementations[implementation] || !slices.Contains(contractProperties, property) {
			t.Errorf("known deviation %s names no binding or property of the suite: remove it", key)
		}
		if !regexp.MustCompile(`^(candidate:[a-z0-9]+(-[a-z0-9]+)*|[A-Z][0-9]+( \([a-z0-9 ,]+\))?)$`).MatchString(reason) {
			t.Errorf("known deviation %s names %q, which is neither a backlog item nor a candidate", key, reason)
		}
		if _, failing := failures[key]; !failing {
			t.Errorf("%s now holds: remove its known deviation", key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(failures)) {
		if _, deviating := known[key]; !deviating {
			t.Errorf("%s: %s", key, strings.Join(failures[key], "; "))
		}
	}
}

// contractPlanning holds a binding's planning, removal and derived answers to
// the contract on its example.
func contractPlanning(t *testing.T, row contractRow, example contractExample, capability lifecycle.Capability, runner *contractRunner, findings *contractFindings) {
	t.Helper()
	ctx := context.Background()
	runner.script(lifecycle.RunResult{}, errors.New("planning must not reach the runner"))
	first, firstErr := capability.Plan(ctx, example.apply)
	second, secondErr := capability.Plan(ctx, example.apply)
	if firstErr != nil || secondErr != nil || !reflect.DeepEqual(first, second) {
		findings.record(row, "plan-deterministic", fmt.Sprintf("two apply plans differ (%v, %v)", firstErr, secondErr))
	}
	for _, definition := range first.Definitions {
		if definition.Kind != row.kind || definition.Implementation != row.implementation {
			findings.record(row, "plan-identity", definition.ID+" is planned as "+definition.Kind+"/"+definition.Implementation)
		}
		if _, err := reconciliation.RequestDigest(definition); err != nil {
			findings.record(row, "plan-canonical", definition.ID+": "+err.Error())
		}
	}
	removals, err := capability.Plan(ctx, example.destroy)
	if err != nil {
		findings.record(row, "removal-reads-frozen-block", "destroy planning fails: "+err.Error())
	}
	planned := map[string]reconciliation.BlockDefinition{}
	for _, definition := range removals.Definitions {
		planned[definition.ID] = definition
	}
	if reporter, ok := capability.(lifecycle.UnsupportedReporter); ok {
		reporter.Unsupported(example.state)
	}
	for _, block := range example.frozen(row.binding()) {
		contractRemoval(t, row, capability, block, planned[block.ID], findings)
		if !row.probes {
			if _, err := capability.Quiescent(ctx, lifecycle.Probe{Block: block}); err != nil {
				findings.record(row, "planning-runs-nothing", block.ID+": a derived quiescence fails: "+err.Error())
			}
		}
	}
	if sent := runner.sent(); len(sent) != 0 {
		findings.record(row, "planning-runs-nothing", fmt.Sprintf("planning, removal and derived answers sent %d runner requests", len(sent)))
	}
}

// contractRemoval proves a removal reads only its frozen block and says what
// destroy-verb planning says about it.
func contractRemoval(t *testing.T, row contractRow, capability lifecycle.Capability, block reconciliation.Block, planned reconciliation.BlockDefinition, findings *contractFindings) {
	t.Helper()
	ctx := context.Background()
	before := contractCloneBlock(t, block)
	removal, err := capability.Removal(ctx, block)
	if err != nil {
		findings.record(row, "removal-reads-frozen-block", block.ID+": "+err.Error())
	}
	if !reflect.DeepEqual(block, before) {
		findings.record(row, "removal-reads-frozen-block", block.ID+": the removal changed its frozen block")
	}
	if removal.Description != planned.Description || !slices.Equal(removal.Impacts, planned.Impacts) ||
		!slices.Equal(removal.Consumes, planned.Consumes) || !contractGroupsEqual(removal.Groups, planned.Groups) {
		findings.record(row, "removal-reads-frozen-block", fmt.Sprintf("%s: the removal says %+v, destroy planning %+v", block.ID, removal, planned))
	}
	malformed := contractCloneBlock(t, block)
	malformed.Request = json.RawMessage(`{}`)
	if _, err := capability.Removal(ctx, malformed); len(diagnostics.Of(err)) == 0 {
		findings.record(row, "removal-refuses-malformed-request", block.ID+": a removal of an empty request did not refuse with a diagnostic")
	}
}

func contractGroupsEqual(left, right []reconciliation.Group) bool {
	return slices.EqualFunc(left, right, func(x, y reconciliation.Group) bool {
		return x.ID == y.ID && x.Description == y.Description && slices.Equal(x.Machines, y.Machines)
	})
}

// contractCloneBlock deep-copies a frozen block through its own encoding, so
// nothing a removal is handed aliases the copy it is compared with.
func contractCloneBlock(t *testing.T, block reconciliation.Block) reconciliation.Block {
	t.Helper()
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	var copied reconciliation.Block
	if err := json.Unmarshal(data, &copied); err != nil {
		t.Fatal(err)
	}
	copied.Requires = slices.Clone(block.Requires)
	return copied
}

// contractEffects holds a runner-driven binding's attempts, observations and
// probe to the contract, over every block it froze.
func contractEffects(t *testing.T, row contractRow, example contractExample, capability lifecycle.Capability, runner *contractRunner, findings *contractFindings) {
	t.Helper()
	material := contractMaterial(t, example, example.plans[row.binding()].Secrets)
	for _, block := range example.frozen(row.binding()) {
		execution := lifecycle.Execution{
			Operation: "op-0123456789abcdef0123456789abcdef", Attempt: 1, Block: block,
			Launch: prerequisites.PythonLaunch{Loader: "/loader"}, Bundle: prerequisites.BundleLocation{Path: "/bundle"},
			Material: material,
			LocateTool: func(_ context.Context, tool controller.InstalledTool) (string, error) {
				return "/var/lib/bootwright/tools/" + tool.Kind + "/" + tool.Version + "/" + tool.Executable, nil
			},
		}
		contractRequests(row, block, capability, execution, runner, findings)
		contractRunnerFailures(row, block, capability, execution, runner, findings)
		contractEvidence(t, row, example, block, capability, execution, runner, findings)
		contractObservations(row, block, capability, execution, runner, findings)
	}
}

// contractCall performs one operation, reporting the outcome an attempt
// records or the effect an observation proves.
func contractCall(capability lifecycle.Capability, operation string, execution lifecycle.Execution) (lifecycle.Result, lifecycle.Observation, error) {
	ctx := context.Background()
	switch operation {
	case "apply":
		result, err := capability.Apply(ctx, execution)
		return result, lifecycle.Observation{}, err
	case "destroy":
		result, err := capability.Destroy(ctx, execution)
		return result, lifecycle.Observation{}, err
	}
	observation, err := capability.Observe(ctx, execution)
	return lifecycle.Result{}, observation, err
}

// contractRequests proves every operation sends exactly one request carrying
// the frozen bytes under the block's identity and a bounded deadline.
func contractRequests(row contractRow, block reconciliation.Block, capability lifecycle.Capability, execution lifecycle.Execution, runner *contractRunner, findings *contractFindings) {
	for _, operation := range []string{"apply", "observe", "destroy"} {
		runner.script(lifecycle.RunResult{Outcome: "changed", Evidence: json.RawMessage(`{}`)}, nil)
		_, _, _ = contractCall(capability, operation, execution)
		sent := runner.sent()
		property := operation + "-request"
		if len(sent) != 1 {
			findings.record(row, property, fmt.Sprintf("%s sent %d requests", block.ID, len(sent)))
			continue
		}
		request := sent[0]
		switch {
		case request.Implementation != row.implementation:
			findings.record(row, property, block.ID+" ran "+request.Implementation)
		case request.Operation != operation:
			findings.record(row, property, block.ID+" ran the "+request.Operation+" operation")
		case request.Digest != block.RequestDigest:
			findings.record(row, property, block.ID+" named another request digest")
		case !bytes.Equal(request.Canonical, block.Request):
			findings.record(row, property, block.ID+" crossed other bytes than its frozen request")
		case request.Deadline < 0 || request.Deadline > lifecycle.MaxDeadline:
			findings.record(row, property, fmt.Sprintf("%s asked for a %s deadline", block.ID, request.Deadline))
		}
	}
}

// contractRunnerFailures proves an attempt keeps a lost or unknown-flagged
// runner failure unknown and records any other diagnosed one failed.
func contractRunnerFailures(row contractRow, block reconciliation.Block, capability lifecycle.Capability, execution lifecycle.Execution, runner *contractRunner, findings *contractFindings) {
	scripted := []struct {
		name string
		err  error
		want reconciliation.Outcome
	}{
		{"an undiagnosed failure", errors.New("the adapter's result was lost"), reconciliation.OutcomeUnknown},
		{"a failure diagnosed unknown", diagnostics.NewFailure("lifecycle.unknown", "the adapter cannot tell what it did", ""), reconciliation.OutcomeUnknown},
		{"a diagnosed failure", diagnostics.NewFailure("lifecycle.state", "the adapter refused within its boundary", ""), reconciliation.OutcomeFailed},
	}
	for _, operation := range []string{"apply", "destroy"} {
		for _, script := range scripted {
			runner.script(lifecycle.RunResult{}, script.err)
			result, _, err := contractCall(capability, operation, execution)
			if result.Outcome != script.want || err == nil {
				findings.record(row, operation+"-outcome-of-runner-error",
					fmt.Sprintf("%s: %s gave %q (%v), want %q", block.ID, script.name, result.Outcome, err, script.want))
			}
		}
	}
}

// contractEvidence proves no changed or unchanged outcome is accepted, and no
// observation proves anything, on evidence that is empty or names another
// request: only positive evidence for this very request leaves unknown. The
// foreign evidence is this block's own presence, absence and partial fixtures
// with only the request digest swapped, and contractEvidenceControls proves
// the presence and absence fixtures naming this block's digest prove their
// operations, so a refusal of either by that operation is the digest check's
// alone. It then holds the same fixtures under this block's digest to their
// kind through contractEvidenceKinds.
func contractEvidence(t *testing.T, row contractRow, example contractExample, block reconciliation.Block, capability lifecycle.Capability, execution lifecycle.Execution, runner *contractRunner, findings *contractFindings) {
	t.Helper()
	other := ""
	for _, candidate := range example.plan.Blocks {
		if candidate.RequestDigest != block.RequestDigest {
			other = candidate.RequestDigest
			break
		}
	}
	contractEvidenceControls(t, row, block, capability, execution, runner)
	foreign := []json.RawMessage{
		contractPresence(t, row, execution, other), contractAbsence(t, row, other), contractPartial(t, row, execution, other),
	}
	if row.noEffect != nil {
		foreign = append(foreign, contractNoEffect(t, row, other))
	}
	for _, operation := range []string{"apply", "destroy"} {
		for _, outcome := range []string{"changed", "unchanged"} {
			for _, evidence := range append([]json.RawMessage{json.RawMessage(`{}`)}, foreign...) {
				runner.script(lifecycle.RunResult{Outcome: outcome, Evidence: evidence}, nil)
				result, _, _ := contractCall(capability, operation, execution)
				if result.Outcome == reconciliation.OutcomeChanged || result.Outcome == reconciliation.OutcomeUnchanged {
					findings.record(row, operation+"-evidence-proves-request",
						fmt.Sprintf("%s: %s on %s gave %q", block.ID, outcome, evidence, result.Outcome))
				}
			}
		}
	}
	for _, evidence := range foreign {
		runner.script(lifecycle.RunResult{Outcome: "unchanged", Evidence: evidence}, nil)
		if _, observation, _ := contractCall(capability, "observe", execution); observation.Effect != reconciliation.EffectUnknown {
			findings.record(row, "observe-evidence-proves-request",
				fmt.Sprintf("%s: an observation of %s proved %q", block.ID, evidence, observation.Effect))
		}
	}
	contractEvidenceKinds(t, row, block, capability, execution, runner, findings)
}

// contractEvidenceKinds holds each operation to the kind of its evidence under
// this block's own digest, since only positive evidence of what an operation
// proves may justify its effect state (specs/state-reconciliation.md, attempts
// and resolution outcomes). An apply reporting either outcome on absence or a
// partial realization is neither changed nor unchanged, so nothing absent is
// recorded realized, and a removal reporting either on presence or a partial
// realization is neither, so nothing still running is recorded removed. An
// observation proves positive no effect from absence and partial from a
// partial realization, never completion, so nothing absent or half built is
// resolved done. A binding whose removal retains what its apply realized
// leaves that absence unknown instead, and is held to the no-effect evidence
// its spec names, when it names one, as absence is held elsewhere; an
// indivisible one leaves partial evidence unknown, as its spec says. Neither
// operation accepts that no-effect evidence, since a direct result with
// positive no effect is never success. The presence and absence fixtures are
// those contractEvidenceControls proves the other operation accepts, so
// refusing them is the kind check's alone. The blocks are an apply's, so an
// observation here is an apply's resolution; how a destroy's resolution reads
// presence is S27's to settle.
func contractEvidenceKinds(t *testing.T, row contractRow, block reconciliation.Block, capability lifecycle.Capability, execution lifecycle.Execution, runner *contractRunner, findings *contractFindings) {
	t.Helper()
	presence := contractPresence(t, row, execution, block.RequestDigest)
	absence := contractAbsence(t, row, block.RequestDigest)
	partial := contractPartial(t, row, execution, block.RequestDigest)
	attempts := []struct {
		operation string
		property  string
		evidence  []json.RawMessage
	}{
		{"apply", "apply-evidence-proves-presence", []json.RawMessage{absence, partial}},
		{"destroy", "destroy-evidence-proves-absence", []json.RawMessage{presence, partial}},
	}
	var noEffect json.RawMessage
	if row.noEffect != nil {
		noEffect = contractNoEffect(t, row, block.RequestDigest)
		attempts[0].evidence = append(attempts[0].evidence, noEffect)
		attempts[1].evidence = append(attempts[1].evidence, noEffect)
	}
	for _, attempt := range attempts {
		for _, outcome := range []string{"changed", "unchanged"} {
			for _, evidence := range attempt.evidence {
				runner.script(lifecycle.RunResult{Outcome: outcome, Evidence: evidence}, nil)
				result, _, _ := contractCall(capability, attempt.operation, execution)
				if result.Outcome == reconciliation.OutcomeChanged || result.Outcome == reconciliation.OutcomeUnchanged {
					findings.record(row, attempt.property, fmt.Sprintf("%s: %s on %s gave %q", block.ID, outcome, evidence, result.Outcome))
				}
			}
		}
	}
	type observationRule struct {
		property string
		evidence json.RawMessage
		want     reconciliation.EffectState
		unproved bool
	}
	observations := []observationRule{
		{"observe-absence-proves-no-effect", absence, reconciliation.EffectNoEffect, row.retains},
		{"observe-partial-proves-partial", partial, reconciliation.EffectPartial, row.indivisible},
	}
	if noEffect != nil {
		observations = append(observations, observationRule{"observe-no-effect-proves-no-effect", noEffect, reconciliation.EffectNoEffect, false})
	}
	for _, observed := range observations {
		want := observed.want
		if observed.unproved {
			want = reconciliation.EffectUnknown
		}
		runner.script(lifecycle.RunResult{Outcome: "unchanged", Evidence: observed.evidence}, nil)
		if _, observation, _ := contractCall(capability, "observe", execution); observation.Effect != want {
			findings.record(row, observed.property,
				fmt.Sprintf("%s: an observation of %s proved %q, want %q", block.ID, observed.evidence, observation.Effect, want))
		}
	}
}

// contractEvidenceControls proves the evidence fixtures sound: presence naming
// this block's digest is accepted by an apply reporting either outcome and
// observed as completion, and absence naming it proves the removal. A fixture
// a binding no longer accepts fails the suite itself, because a refusal of its
// foreign copy would then prove nothing about the digest, nor a refusal of it
// by the other operation anything about the kind. The partial fixture is
// presence with its postcondition unproved; the observe-partial-proves-partial
// property is its control.
func contractEvidenceControls(t *testing.T, row contractRow, block reconciliation.Block, capability lifecycle.Capability, execution lifecycle.Execution, runner *contractRunner) {
	t.Helper()
	presence := contractPresence(t, row, execution, block.RequestDigest)
	absence := contractAbsence(t, row, block.RequestDigest)
	controls := []struct {
		operation string
		outcome   string
		evidence  json.RawMessage
		want      reconciliation.Outcome
	}{
		{"apply", "changed", presence, reconciliation.OutcomeChanged},
		{"apply", "unchanged", presence, reconciliation.OutcomeUnchanged},
		{"destroy", "changed", absence, reconciliation.OutcomeChanged},
	}
	for _, control := range controls {
		runner.script(lifecycle.RunResult{Outcome: control.outcome, Evidence: control.evidence}, nil)
		if result, _, err := contractCall(capability, control.operation, execution); result.Outcome != control.want {
			t.Errorf("%s: the evidence fixture %s no longer proves a %s of %s reporting %s (%q, %v)",
				row.implementation, control.evidence, control.operation, block.ID, control.outcome, result.Outcome, err)
		}
	}
	runner.script(lifecycle.RunResult{Outcome: "unchanged", Evidence: presence}, nil)
	if _, observation, err := contractCall(capability, "observe", execution); observation.Effect != reconciliation.EffectCompleted {
		t.Errorf("%s: the presence evidence fixture %s is no longer observed as the completion of %s (%q, %v)",
			row.implementation, presence, block.ID, observation.Effect, err)
	}
}

// contractPresence is the row's presence evidence for the attempt's block,
// naming one request digest.
func contractPresence(t *testing.T, row contractRow, execution lifecycle.Execution, digest string) json.RawMessage {
	t.Helper()
	fields := row.presence(t, execution)
	fields["request"] = digest
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// contractPartial is the row's partial realization of the attempt's block,
// naming one request digest: its presence evidence with the postcondition
// unproved, so everything the frozen request names is reported present while
// the adapter proved no settled state, which every binding that is not
// indivisible reads as this context's work part way through.
func contractPartial(t *testing.T, row contractRow, execution lifecycle.Execution, digest string) json.RawMessage {
	t.Helper()
	fields := row.presence(t, execution)
	fields["request"], fields["postcondition"] = digest, false
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// contractNoEffect is the row's positive no-effect evidence for one request
// digest: nothing the apply would realize reported, and what the binding's own
// spec requires to prove it.
func contractNoEffect(t *testing.T, row contractRow, digest string) json.RawMessage {
	t.Helper()
	fields := map[string]any{"request": digest}
	maps.Copy(fields, row.noEffect)
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// contractAbsence is absence evidence for one request digest: removal proved,
// nothing owned left, and what the binding's own evidence adds.
func contractAbsence(t *testing.T, row contractRow, digest string) json.RawMessage {
	t.Helper()
	fields := map[string]any{"absent": true, "postcondition": true, "request": digest}
	maps.Copy(fields, row.absence)
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// contractMachinePresence proves a libvirt machine realized: the hypervisor
// answered with the frozen domain, owned, every frozen disk present at its
// size, and the controller unit running the frozen image and exposing the
// frozen system with a power state.
func contractMachinePresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := libvirt.DecodeMachineRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	disks := []map[string]any{}
	for _, disk := range request.Disks {
		disks = append(disks, map[string]any{"name": disk.Name, "present": true, "sizeGiB": disk.SizeGiB})
	}
	return map[string]any{
		"postcondition": true, "answered": true, "domain": request.Domain, "owned": true, "state": "running",
		"unit": "active", "controller": request.Controller.Image, "system": request.UUID, "power": "On", "disks": disks,
	}
}

// contractBareMetalPresence proves a physical machine answered: an identity,
// a power state and every declared hardware address in its inventory.
func contractBareMetalPresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := baremetal.DecodeRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"postcondition": true, "uuid": "4c4c4544-0000-1000-8000-000000000001", "manufacturer": "Contract", "model": "Contract",
		"serial": "CONTRACT1", "power": "On", "addresses": request.Addresses(),
	}
}

// contractInstallationPresence proves an installation finished: the frozen
// marker for this block's digest, the frozen address answering on a host key
// as the fleet account, the media ejected, the machine running and every
// publication its boot needs present.
func contractInstallationPresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := installation.DecodeRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := installation.MarkerFor(request, execution.Block.RequestDigest)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"postcondition": true, "marker": string(marker), "address": request.Address,
		"hostKey": "ssh-ed25519 contract-host-key", "reachable": true,
		"power": "On", "image": true, "tree": request.Tree != nil,
	}
}

// contractHostPresence proves a provider host realized: the closure present,
// every frozen driver daemon active and enabled, the URI answering, the pool
// active and every frozen network present, managed ones active and owned.
func contractHostPresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := libvirt.DecodeHostRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	services := []map[string]any{}
	for _, name := range request.Services {
		services = append(services, map[string]any{"name": name, "state": "active", "enabled": true})
	}
	networks := []map[string]any{}
	for _, network := range request.Networks {
		networks = append(networks, map[string]any{"name": network.Name, "managed": network.Managed, "bridge": true, "state": "active", "owned": network.Managed})
	}
	return map[string]any{
		"postcondition": true, "hypervisor": true, "services": services, "uri": true, "pool": "active", "networks": networks,
	}
}

// contractMediaPresence proves boot media published from this block's request
// by the declared release's installer.
func contractMediaPresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := agentinstall.DecodeMediaRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"postcondition": true, "image": true, "inputs": execution.Block.RequestDigest, "installer": request.Release.Version,
	}
}

// contractClusterPresence proves the cluster this operation installed answers
// at the declared release, reports its installation completed, holds every
// declared node and presents no boot media.
func contractClusterPresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := agentinstall.DecodeInstallRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	identity := "contract-cluster-identity"
	return map[string]any{
		"postcondition": true, "identity": identity, "cluster": identity, "release": request.Release.Version, "completed": true,
	}
}

// contractServicePresence proves a managed network service realized: its unit
// active, the frozen image running, its content root present and every probe
// target answering on the frozen port.
func contractServicePresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := managedservice.DecodeRequest(execution.Block.Request, execution.Block.Implementation)
	if err != nil {
		t.Fatal(err)
	}
	answers := []map[string]any{}
	for _, address := range request.ProbeTargets() {
		answers = append(answers, map[string]any{"address": address, "answer": "contract answer", "port": request.Port})
	}
	return map[string]any{
		"postcondition": true, "unit": "active", "container": request.Image, "contentRoot": true, "answers": answers,
	}
}

// contractArtifactServerPresence proves an artifact server realized: its unit
// active, the frozen image running, its content root present, and every probe
// target answering with an HTTP status line, each HTTPS one presenting the
// certificate the attempt binds.
func contractArtifactServerPresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := artifactserver.DecodeRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := ""
	if request.TLS != nil {
		certificate, err := artifactserver.ValidateServingCertificate(execution.Material[request.TLS.Secret], nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		fingerprint = certificate.Fingerprint
	}
	listeners := []map[string]any{}
	for _, target := range request.ProbeTargets() {
		listener := map[string]any{"address": target.Address, "name": target.Name, "port": target.Port, "protocol": target.Protocol, "status": "HTTP/1.1 200 OK"}
		if target.Protocol == "https" {
			listener["fingerprint"] = fingerprint
		}
		listeners = append(listeners, listener)
	}
	return map[string]any{
		"postcondition": true, "unit": "active", "container": request.Image, "contentRoot": true, "listeners": listeners,
	}
}

// contractObservations proves a failed or empty observation proves nothing,
// and that a probe reading one is never quiescent.
func contractObservations(row contractRow, block reconciliation.Block, capability lifecycle.Capability, execution lifecycle.Execution, runner *contractRunner, findings *contractFindings) {
	scripted := []struct {
		name   string
		result lifecycle.RunResult
		err    error
	}{
		{"after a runner failure", lifecycle.RunResult{}, errors.New("the observation could not be made")},
		{"on empty evidence", lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{}`)}, nil},
	}
	for _, script := range scripted {
		runner.script(script.result, script.err)
		if _, observation, _ := contractCall(capability, "observe", execution); observation.Effect != reconciliation.EffectUnknown {
			findings.record(row, "observe-unproved", fmt.Sprintf("%s: an observation %s proved %q", block.ID, script.name, observation.Effect))
		}
		if !row.probes {
			continue
		}
		runner.script(script.result, script.err)
		quiescence, err := capability.Quiescent(context.Background(), lifecycle.Probe{
			Block: block, Launch: execution.Launch, Bundle: execution.Bundle, Material: execution.Material,
		})
		if err == nil && quiescence.Settled() {
			findings.record(row, "quiescence-unproved", fmt.Sprintf("%s: a probe %s reads quiescent", block.ID, script.name))
		}
	}
}

// contractMaterial synthesizes well-formed material for every Secret a plan
// declares, by the type its declaration names.
func contractMaterial(t *testing.T, example contractExample, references []string) map[string]secrets.Material {
	t.Helper()
	material := map[string]secrets.Material{}
	for _, reference := range references {
		declaration, found := example.state.Effective().Find(api.Secret, reference)
		if !found {
			t.Fatalf("the plan declares the Secret %s, which the example does not", reference)
		}
		switch kind := declaration.Spec().Get("type").Text(); kind {
		case "tlsCertificate":
			material[reference] = contractServingCertificate(t, contractServedAddresses(t, example))
		case "usernamePassword":
			material[reference] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("admin"), secrets.PasswordPart: []byte("contract-password")})
		case "dockerConfigJson":
			material[reference] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"auths":{"registry.example.test":{"auth":"Y29udHJhY3Q6c2VjcmV0"}}}`)})
		case "sshKeyPair":
			material[reference] = contractKeyPair(t, reference)
		default:
			t.Fatalf("the Secret %s is a %s, which the suite synthesizes no material for", reference, kind)
		}
	}
	return material
}

// contractServedAddresses is every endpoint address the example's artifact
// servers froze, which a serving certificate must cover.
func contractServedAddresses(t *testing.T, example contractExample) []string {
	t.Helper()
	var addresses []string
	for _, block := range example.plan.Blocks {
		if block.Implementation != artifactserver.Implementation {
			continue
		}
		request, err := artifactserver.DecodeRequest(block.Request)
		if err != nil {
			t.Fatal(err)
		}
		for _, endpoint := range request.Endpoints {
			if !slices.Contains(addresses, endpoint.Address) {
				addresses = append(addresses, endpoint.Address)
			}
		}
	}
	return addresses
}

// contractServingCertificate is a self-signed, non-CA server certificate valid
// now whose subject alternative names cover every address.
func contractServingCertificate(t *testing.T, addresses []string) secrets.Material {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "contract.example.test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: false,
	}
	for _, address := range addresses {
		if ip := net.ParseIP(address); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, address)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		secrets.PrivateKeyPart:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}),
	})
}

// contractKeyPair is an ed25519 SSH key pair in the OpenSSH encodings a bound
// sshKeyPair Secret carries.
func contractKeyPair(t *testing.T, comment string) secrets.Material {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := ssh.MarshalPrivateKey(private, comment)
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.PrivateKeyPart: pem.EncodeToMemory(encoded),
		secrets.PublicKeyPart:  ssh.MarshalAuthorizedKey(authorized),
	})
}

// TestEveryBindingJoinsTheCapabilityContractSuite keeps the suite's rows and
// this build's bindings one list: a binding with no row, or a row naming no
// binding, fails.
func TestEveryBindingJoinsTheCapabilityContractSuite(t *testing.T) {
	rows := map[lifecycle.CapabilityBinding]bool{}
	for _, row := range contractRows() {
		if rows[row.binding()] {
			t.Errorf("%s/%s has two rows", row.kind, row.implementation)
		}
		rows[row.binding()] = true
	}
	for _, binding := range buildCapabilities(systemClock{}, controllerDependencies{}).Bindings() {
		if !rows[binding] {
			t.Errorf("%s/%s is offered by this build but has no row in the capability contract suite", binding.Kind, binding.Implementation)
		}
		delete(rows, binding)
	}
	for binding := range rows {
		t.Errorf("the capability contract suite has a row for %s/%s, which this build does not offer", binding.Kind, binding.Implementation)
	}
}
