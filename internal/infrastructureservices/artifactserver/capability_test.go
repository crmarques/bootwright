package artifactserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type fakeRunner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *fakeRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return testMoment }

func planInput(t *testing.T, verb reconciliation.Verb, objects ...api.Object) lifecycle.PlanInput {
	t.Helper()
	catalog := catalogOf(objects...)
	return lifecycle.PlanInput{
		Verb:       verb,
		Context:    lifecycle.ContextIdentity{Name: testContext, Revision: "rev-1"},
		State:      compilation.NewState(catalog, catalog, nil),
		Controller: "controller",
	}
}

func TestPlanFreezesOneBlockPerManagedServer(t *testing.T) {
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Apply, controller(), artifactServer()))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	definition := plan.Definitions[0]
	if definition.ID != "artifact-server-lab-artifacts" || definition.Kind != Kind || definition.Implementation != Implementation {
		t.Fatalf("definition = %+v", definition)
	}
	if definition.ContentDigest != ContentDigest() || len(definition.ContentDigest) != 64 {
		t.Fatal("the block does not carry this build's content digest")
	}
	if !slices.Equal(plan.Secrets, []string{"artifact-server-tls"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
	if len(plan.Reservations) != 1 || plan.Reservations[0].Context != testContext || plan.Reservations[0].Kind != "artifact-server" {
		t.Fatalf("reservations = %+v", plan.Reservations)
	}
	groups := []string{}
	for _, group := range definition.Groups {
		groups = append(groups, group.ID)
	}
	if !slices.Equal(groups, []string{"pull-image", "publish-configuration", "start-service", "verify-readiness"}) {
		t.Fatalf("apply groups = %v", groups)
	}
	if _, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions); err != nil {
		t.Fatal("the capability produced a block the plan model refuses:", err)
	}
}

func TestDestroyPlanUsesRemovalGroups(t *testing.T) {
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Destroy, controller(), artifactServer()))
	if err != nil {
		t.Fatal(err)
	}
	groups := []string{}
	for _, group := range plan.Definitions[0].Groups {
		groups = append(groups, group.ID)
	}
	if !slices.Equal(groups, []string{"stop-service", "remove-configuration", "verify-absence"}) {
		t.Fatalf("destroy groups = %v", groups)
	}
}

// A server bound to a managed bridge's host address, or bound to a wildcard
// with an endpoint at that address, waits for the provider block that creates
// the bridge, as every managed service does.
func TestAServerBoundToAManagedBridgeRequiresItsProvider(t *testing.T) {
	provider := api.NewObject(api.InfraProvider, "lab-libvirt", api.Value{}, api.MapValue(
		field("libvirt", api.MapValue(text("machineRef", "controller"), text("uri", "qemu:///system"))),
		field("networkAttachments", api.ListValue(api.MapValue(text("name", "lab-guests"), field("libvirt", api.MapValue(
			text("bridge", "virbr-lab"), text("management", "managed"), text("address", "198.51.100.1/24"),
		))))),
	))
	addresses := controller().Spec().Get("network", "addresses").Items()
	host := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("network", api.MapValue(field("addresses", api.ListValue(append(addresses,
		api.MapValue(text("name", "guests"), text("address", "198.51.100.1/24")),
	)...)))))
	endpoints := func(https string) api.FieldValue {
		return field("endpoints", api.ListValue(
			api.MapValue(text("name", "ip-https"), text("listenerRef", "https"), text("addressRef", https)),
			api.MapValue(text("name", "ip-http"), text("listenerRef", "http"), text("addressRef", "ip")),
		))
	}
	requires := []reconciliation.ObjectRef{{Kind: "InfraProvider", Object: "lab-libvirt"}}
	capability := New(&fakeRunner{}, fixedClock{})
	for name, test := range map[string]struct {
		server api.Object
		want   []reconciliation.ObjectRef
	}{
		"the bridge host address":                 {artifactServer(text("bindAddress", "198.51.100.1")), requires},
		"another address":                         {artifactServer(text("bindAddress", "192.0.2.1")), nil},
		"a wildcard whose endpoint is the bridge": {artifactServer(text("bindAddress", "0.0.0.0"), endpoints("guests")), requires},
		"a wildcard with no endpoint on it":       {artifactServer(text("bindAddress", "0.0.0.0"), endpoints("ip")), nil},
	} {
		plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Apply, host, provider, test.server))
		if err != nil || len(plan.Definitions) != 1 {
			t.Fatalf("%s: plan = %+v (%v)", name, plan, err)
		}
		if !slices.Equal(plan.Definitions[0].Requires, test.want) {
			t.Fatalf("%s requires %+v, want %+v", name, plan.Definitions[0].Requires, test.want)
		}
	}
}

func TestSSHPlacementReservesNothing(t *testing.T) {
	ssh := api.MapValue(
		text("addressRef", "ip"),
		field("auth", api.MapValue(text("privateKeyRef", "services-key"))),
		text("knownHostsRef", "services-host-key"),
	)
	host := api.NewObject(api.Machine, "services", api.Value{},
		controller().Spec().Without("access").With("access", api.MapValue(field("ssh", ssh))))
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Apply, host, artifactServer(text("machineRef", "services"))))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Reservations) != 0 {
		t.Fatalf("an SSH placement reserved host resources: %+v", plan.Reservations)
	}
}

func execution(t *testing.T, material secrets.Material) lifecycle.Execution {
	t.Helper()
	capability := New(&fakeRunner{}, fixedClock{})
	plan, err := capability.Plan(context.Background(), planInput(t, reconciliation.Apply, controller(), artifactServer()))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{
		Operation: "op-1", Attempt: 1, Block: frozen.Blocks[0],
		Material: map[string]secrets.Material{"artifact-server-tls": material},
	}
}

func TestApplyValidatesTheAdapterOutcomeAndEvidence(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: presenceEvidence(request, call.Block.RequestDigest, fingerprint)}}
	result, err := New(runner, fixedClock{}).Apply(context.Background(), call)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	if len(runner.requests) != 1 || runner.requests[0].Operation != "apply" {
		t.Fatalf("adapter invocation = %+v", runner.requests)
	}
	runner.result.Outcome = "unchanged"
	result, err = New(runner, fixedClock{}).Apply(context.Background(), call)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("replay = %+v (%v)", result, err)
	}
	for name, outcome := range map[string]string{"no-effect": "no-effect", "failed": "failed", "invented": "done"} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: outcome}}
			result, err := New(runner, fixedClock{}).Apply(context.Background(), call)
			if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("outcome %q was accepted: %+v (%v)", outcome, result, err)
			}
		})
	}
}

func TestApplyRefusesBeforeTheAdapterWhenMaterialIsUnusable(t *testing.T) {
	material, _ := issue(t, certificateOptions{
		ips: []string{"203.0.113.9"}, notBefore: testMoment.Add(-time.Hour), notAfter: testMoment.Add(time.Hour),
	})
	runner := &fakeRunner{}
	result, err := New(runner, fixedClock{}).Apply(context.Background(), execution(t, material))
	if err == nil || result.Outcome != reconciliation.OutcomeFailed {
		t.Fatalf("an uncovered certificate reached the adapter: %+v (%v)", result, err)
	}
	if len(runner.requests) != 0 {
		t.Fatal("the adapter ran despite an unusable certificate")
	}
	missing := execution(t, material)
	missing.Material = nil
	if _, err := New(runner, fixedClock{}).Apply(context.Background(), missing); err == nil {
		t.Fatal("an attempt without its bound material reached the adapter")
	}
	if len(runner.requests) != 0 {
		t.Fatal("the adapter ran without bound material")
	}
}

func TestAdapterFailureIsUnknownNotFailed(t *testing.T) {
	material, _ := issue(t, validOptions())
	runner := &fakeRunner{err: errors.New("lost")}
	result, err := New(runner, fixedClock{}).Apply(context.Background(), execution(t, material))
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("a lost adapter result was not unknown: %+v (%v)", result, err)
	}
}

// A fresh destroy over an incomplete apply resolves the apply's block through
// this same observation, so a server the apply left all present whose listener
// never answered, or answers with another certificate, is this context's own
// server not yet ready: partial, which that destroy then removes, rather than
// unknown, which refuses it.
func TestObserveMapsLiveEvidenceToItsEffectState(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	present := func(change func(*Evidence)) json.RawMessage {
		var evidence Evidence
		if err := json.Unmarshal(presenceEvidence(request, call.Block.RequestDigest, fingerprint), &evidence); err != nil {
			t.Fatal(err)
		}
		change(&evidence)
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	silent := func(e *Evidence) { e.Listeners = []ListenerEvidence{} }
	foreignLeaf := func(e *Evidence) {
		for index := range e.Listeners {
			if e.Listeners[index].Protocol == "https" {
				e.Listeners[index].Fingerprint = testFingerprint
			}
		}
	}
	for name, tc := range map[string]struct {
		result lifecycle.RunResult
		err    error
		want   reconciliation.EffectState
	}{
		"complete":                   {lifecycle.RunResult{Outcome: "unchanged", Evidence: presenceEvidence(request, call.Block.RequestDigest, fingerprint)}, nil, reconciliation.EffectCompleted},
		"absent":                     {lifecycle.RunResult{Outcome: "unchanged", Evidence: absenceEvidence(call.Block.RequestDigest)}, nil, reconciliation.EffectNoEffect},
		"partial":                    {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":true,"listeners":[],"postcondition":false,"request":"` + call.Block.RequestDigest + `","unit":"active"}`)}, nil, reconciliation.EffectPartial},
		"present and silent":         {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(silent)}, nil, reconciliation.EffectPartial},
		"present, another leaf":      {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(foreignLeaf)}, nil, reconciliation.EffectPartial},
		"silent, another request":    {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(func(e *Evidence) { silent(e); e.Request = testDigest })}, nil, reconciliation.EffectUnknown},
		"a postcondition unreported": {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(func(e *Evidence) { silent(e); e.Unit = "inactive" })}, nil, reconciliation.EffectUnknown},
		"nothing owned":              {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":false,"listeners":[],"postcondition":false,"request":"` + call.Block.RequestDigest + `","unit":""}`)}, nil, reconciliation.EffectUnknown},
		"adapter failed":             {lifecycle.RunResult{}, errors.New("unreachable"), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: tc.result, err: tc.err}
			observation, err := New(runner, fixedClock{}).Observe(context.Background(), call)
			if err != nil || observation.Effect != tc.want {
				t.Fatalf("observation = %+v (%v), want %s", observation, err, tc.want)
			}
		})
	}
}

// A removal's resolution reads the same observation for what the removal
// proves, so a server still present is a removal that had no effect rather
// than one that completed. It reads only what the removal takes back: a
// listener that stays silent or presents another certificate leaves a present
// server a removal with no effect rather than an unresolvable one. Each of the
// unit, the container of the frozen image and the content root decides on its
// own, whatever postcondition the observation reports beside it, so whatever
// is left of them, down to any one alone, is a removal part way through. Only
// the presence form reports what is left: an absence form that still reports
// the server contradicts itself and stays unknown.
func TestARemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	digest := call.Block.RequestDigest
	present := func(change func(*Evidence)) json.RawMessage {
		var evidence Evidence
		if err := json.Unmarshal(presenceEvidence(request, digest, fingerprint), &evidence); err != nil {
			t.Fatal(err)
		}
		change(&evidence)
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	silent := func(e *Evidence) { e.Listeners = []ListenerEvidence{} }
	foreignLeaf := func(e *Evidence) {
		for index := range e.Listeners {
			if e.Listeners[index].Protocol == "https" {
				e.Listeners[index].Fingerprint = testFingerprint
			}
		}
	}
	if ValidatePresence(present(foreignLeaf), request, digest, fingerprint) == nil {
		t.Fatal("the foreign-leaf fixture presents the bound certificate")
	}
	for name, tc := range map[string]struct {
		result lifecycle.RunResult
		err    error
		want   reconciliation.EffectState
	}{
		"absent":                       {lifecycle.RunResult{Outcome: "unchanged", Evidence: absenceEvidence(digest)}, nil, reconciliation.EffectCompleted},
		"present":                      {lifecycle.RunResult{Outcome: "unchanged", Evidence: presenceEvidence(request, digest, fingerprint)}, nil, reconciliation.EffectNoEffect},
		"present and silent":           {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(silent)}, nil, reconciliation.EffectNoEffect},
		"present, another leaf":        {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(foreignLeaf)}, nil, reconciliation.EffectNoEffect},
		"unit without a container":     {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":true,"listeners":[],"postcondition":true,"request":"` + digest + `","unit":"active"}`)}, nil, reconciliation.EffectPartial},
		"another image running":        {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(func(e *Evidence) { e.Container = "registry.example.test/other:1" })}, nil, reconciliation.EffectPartial},
		"unit stopped, root left":      {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":true,"listeners":[],"postcondition":false,"request":"` + digest + `","unit":"inactive"}`)}, nil, reconciliation.EffectPartial},
		"unit stopped, container left": {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(func(e *Evidence) { silent(e); e.Unit = "inactive"; e.Postcondition = false })}, nil, reconciliation.EffectPartial},
		"content root gone":            {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(func(e *Evidence) { e.ContentRoot = false; e.Postcondition = false })}, nil, reconciliation.EffectPartial},
		"only the content root left":   {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":true,"listeners":[],"postcondition":false,"request":"` + digest + `","unit":""}`)}, nil, reconciliation.EffectPartial},
		"only the unit left":           {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":false,"listeners":[],"postcondition":false,"request":"` + digest + `","unit":"inactive"}`)}, nil, reconciliation.EffectPartial},
		"only the container left":      {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(func(e *Evidence) { silent(e); e.Unit, e.ContentRoot, e.Postcondition = "", false, false })}, nil, reconciliation.EffectPartial},
		"nothing reported":             {lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"absent":false,"container":"","contentRoot":false,"listeners":[],"postcondition":false,"request":"` + digest + `","unit":""}`)}, nil, reconciliation.EffectUnknown},
		"absence form, server left":    {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(func(e *Evidence) { e.Absent = true })}, nil, reconciliation.EffectUnknown},
		"silent, another request":      {lifecycle.RunResult{Outcome: "unchanged", Evidence: present(func(e *Evidence) { silent(e); e.Request = testDigest })}, nil, reconciliation.EffectUnknown},
		"another request":              {lifecycle.RunResult{Outcome: "unchanged", Evidence: absenceEvidence(testDigest)}, nil, reconciliation.EffectUnknown},
		"adapter failed":               {lifecycle.RunResult{}, errors.New("unreachable"), reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: tc.result, err: tc.err}
			observation, err := New(runner, fixedClock{}).ObserveRemoval(context.Background(), call)
			if err != nil || observation.Effect != tc.want {
				t.Fatalf("removal observation = %+v (%v), want %s", observation, err, tc.want)
			}
			if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" {
				t.Fatalf("adapter invocation = %+v", runner.requests)
			}
			readings := 0
			for _, validate := range []error{
				ValidateAbsence(tc.result.Evidence, digest), ValidateUnremoved(tc.result.Evidence, request, digest), ValidateRemovalUnfinished(tc.result.Evidence, request, digest),
			} {
				if validate == nil {
					readings++
				}
			}
			if readings > 1 {
				t.Fatalf("the evidence proves %d removal effects at once", readings)
			}
		})
	}
}

func TestDestroyRequiresPositiveAbsence(t *testing.T) {
	material, fingerprint := issue(t, validOptions())
	call := execution(t, material)
	request, err := DecodeRequest(call.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: absenceEvidence(call.Block.RequestDigest)}}
	result, err := New(runner, fixedClock{}).Destroy(context.Background(), call)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("destroy = %+v (%v)", result, err)
	}
	if runner.requests[0].Operation != "destroy" {
		t.Fatalf("adapter operation = %q", runner.requests[0].Operation)
	}
	present := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: presenceEvidence(request, call.Block.RequestDigest, fingerprint)}}
	result, err = New(present, fixedClock{}).Destroy(context.Background(), call)
	if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
		t.Fatalf("destroy accepted presence evidence: %+v (%v)", result, err)
	}
}

func TestUnconfiguredCapabilityPerformsNoWork(t *testing.T) {
	material, _ := issue(t, validOptions())
	if _, err := (Capability{}).Apply(context.Background(), execution(t, material)); err == nil {
		t.Fatal("an unconfigured capability applied")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(&fakeRunner{}, fixedClock{}).Apply(ctx, execution(t, material)); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled attempt = %v", err)
	}
}
