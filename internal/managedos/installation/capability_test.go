package installation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
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

func planInput(verb reconciliation.Verb) lifecycle.PlanInput {
	catalog := labCatalog()
	return lifecycle.PlanInput{
		Verb: verb, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	}
}

func fleetMaterial() map[string]secrets.Material {
	return map[string]secrets.Material{
		"lab-bmc-credentials": secrets.NewMaterial(nil),
		"bootwright-machine-key": secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.PublicKeyPart:  []byte("ssh-ed25519 AAAAPUBLIC bootwright\n"),
			secrets.PrivateKeyPart: []byte("PRIVATE"),
		}),
	}
}

func execution(t *testing.T, digest string) (lifecycle.Execution, Request) {
	t.Helper()
	request := pinnedRequest(t, labCatalog())
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{
		Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: "os-install-rhel-01", Request: canonical},
			RequestDigest:   digest,
		},
		Material: fleetMaterial(),
	}, request
}

func completeEvidence(request Request, digest, marker string) json.RawMessage {
	data, _ := json.Marshal(Evidence{
		Address: request.Address, HostKey: "ssh-ed25519 AAAAHOST", Image: true, Marker: marker,
		Postcondition: true, Power: "On", Reachable: true, Request: digest, Tree: request.Tree != nil,
		TreeIdentity: treeIdentity(request),
	})
	return data
}

// treeIdentity is the identity a tree extracted from the request's frozen
// image carries, and none when it hosts no tree.
func treeIdentity(request Request) string {
	if request.TreeMedia == nil {
		return ""
	}
	return request.TreeMedia.SHA256
}

func TestPlanContributesOneInstallationBlockPerMachine(t *testing.T) {
	plan, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), planInput(reconciliation.Apply))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	definition := plan.Definitions[0]
	if definition.ID != "os-install-rhel-01" || definition.Stage != reconciliation.StageMachines {
		t.Fatalf("definition = %+v", definition)
	}
	if definition.Kind != Kind || definition.Implementation != Implementation {
		t.Fatalf("identity = %s/%s", definition.Kind, definition.Implementation)
	}
	// The installed system stays with the Machine's disks, so this block
	// removes published content alone and consumes no authorization.
	if len(definition.Consumes) != 0 {
		t.Fatalf("consumes = %v", definition.Consumes)
	}
	// The block is well formed only beside the blocks that realize what it
	// requires; alone, the engine refuses it rather than registering work that
	// can never start.
	if _, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions); err == nil {
		t.Fatal("a block whose requirements nothing realizes was accepted")
	}
	whole, err := reconciliation.NewPlan(reconciliation.Apply, append(plan.Definitions, realizing(definition.Requires)...))
	if err != nil {
		t.Fatal("the installation produced a block the plan model refuses:", err)
	}
	if whole.Blocks[len(whole.Blocks)-1].ID != "os-install-rhel-01" {
		t.Fatal("the installation did not order after everything it requires")
	}
}

// realizing is one placeholder block per required object, standing in for the
// capabilities that realize them in a whole plan.
func realizing(references []reconciliation.ObjectRef) []reconciliation.BlockDefinition {
	definitions := make([]reconciliation.BlockDefinition, 0, len(references))
	for index, reference := range references {
		definitions = append(definitions, reconciliation.BlockDefinition{
			ID:          "realizes-" + strings.ToLower(reference.Kind) + "-" + reference.Object,
			Description: "realize " + reference.Object, Stage: reconciliation.StageInfraComponents,
			Kind: reference.Kind, Object: reference.Object, Implementation: "placeholder",
			ContentDigest: strings.Repeat("a", 64),
			Request:       json.RawMessage(`{"index":` + string(rune('0'+index)) + `}`),
		})
	}
	return definitions
}

// The block waits for its Machine and for every service the guest uses, named
// by API object rather than by another capability's block identity.
func TestPlanRequiresTheMachineAndEveryServiceItUses(t *testing.T) {
	plan, _ := New(nil).WithMedia(labMedia()).Plan(context.Background(), planInput(reconciliation.Apply))
	want := []reconciliation.ObjectRef{
		{Kind: "Machine", Object: "rhel-01"},
		{Kind: "ArtifactServer", Object: "lab-artifacts"},
		{Kind: "DNSServer", Object: "lab-dns"},
		{Kind: "NTPServer", Object: "lab-ntp"},
	}
	if !slices.Equal(plan.Definitions[0].Requires, want) {
		t.Fatalf("requires = %v", plan.Definitions[0].Requires)
	}
}

// Every store entry the installation names is frozen by one shared claim, which
// blocks only deletion and replacement of what it names. A reservation is
// identified by its context, kind and service, so the entries are keys of one
// claim rather than a claim each: two of them would be a duplicate identity the
// store refuses at registration.
func TestPlanFreezesEveryMediaEntryUnderOneClaim(t *testing.T) {
	plan, _ := New(nil).WithMedia(labMedia()).Plan(context.Background(), planInput(reconciliation.Apply))
	var media []prerequisites.HostReservation
	for _, reservation := range plan.Reservations {
		if reservation.Shared {
			media = append(media, reservation)
		}
	}
	if len(media) != 1 {
		t.Fatalf("media claims = %+v", media)
	}
	if media[0].Kind != "media" || media[0].Service != "media" || media[0].Context != testContext {
		t.Fatalf("media reservation = %+v", media[0])
	}
	want := []string{
		managedos.MediaReservationKey("rhel-9.8-x86_64-boot.iso"),
		managedos.MediaReservationKey("rhel-9.8-x86_64-dvd.iso"),
	}
	if !slices.Equal(media[0].Keys, want) {
		t.Fatalf("media claims = %v", media[0].Keys)
	}
	if !slices.Equal(plan.Secrets, []string{"bootwright-machine-key", "lab-artifacts-tls", "lab-bmc-credentials"}) {
		t.Fatalf("secrets = %v", plan.Secrets)
	}
}

// Two Machines installing from the same media still claim it once, because the
// claim belongs to the context rather than to a Machine.
func TestTwoInstallationsShareOneMediaClaim(t *testing.T) {
	catalog := labCatalog(api.NewObject(api.Machine, "rhel-02", api.Value{}, guest().Spec()))
	plan, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	})
	if err != nil || len(plan.Definitions) != 2 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	claims := 0
	for _, reservation := range plan.Reservations {
		if reservation.Shared {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("two installations produced %d media claims", claims)
	}
	// The package tree is published per install profile, so both installations
	// publish the same one. Each names it as a resource it will not share, so
	// the two never extract and rename over that path at the same time.
	for _, definition := range plan.Definitions {
		if len(definition.Exclusive) != 1 || !strings.HasPrefix(definition.Exclusive[0], "path:") {
			t.Fatalf("%s exclusive = %v", definition.ID, definition.Exclusive)
		}
	}
	if plan.Definitions[0].Exclusive[0] != plan.Definitions[1].Exclusive[0] {
		t.Fatalf("two installations of one profile named different trees: %v and %v",
			plan.Definitions[0].Exclusive, plan.Definitions[1].Exclusive)
	}
}

// An installation that publishes no package tree shares nothing, so it never
// waits for another Machine's installation.
func TestAnInstallationWithoutATreeNamesNoExclusiveResource(t *testing.T) {
	plan, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), planInput(reconciliation.Apply))
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	request.Tree = nil
	if keys := request.ExclusiveKeys(); keys != nil {
		t.Fatalf("exclusive = %v", keys)
	}
}

// Only the public half of the fleet key ever leaves the binding, and it reaches
// the adapter as a value beside the material paths.
func TestOnlyThePublicHalfOfTheFleetKeyReachesTheAdapter(t *testing.T) {
	call, request := execution(t, "digest")
	marker, _ := MarkerFor(request, "digest")
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: completeEvidence(request, "digest", string(marker))}}
	if _, err := New(runner).Apply(context.Background(), call); err != nil {
		t.Fatalf("apply: %v", err)
	}
	values := runner.requests[0].MaterialValues
	if values["authorizedKey"] != "ssh-ed25519 AAAAPUBLIC bootwright" {
		t.Fatalf("authorized key = %q", values["authorizedKey"])
	}
	if values["marker"] != string(marker) {
		t.Fatalf("marker = %q", values["marker"])
	}
	for _, value := range values {
		if strings.Contains(value, "PRIVATE") {
			t.Fatal("a private half reached the adapter as a value")
		}
	}
}

// The adapter substitutes the fleet key's public half into the rendered
// Kickstart, past the renderer's guard, so a half the store already holds that
// would end its quoted sshkey line, or that quoting, refuses before the
// adapter runs. A tab between or around the line's fields, or the carriage
// return of a CRLF line ending, refuses as well, as it does at import, and
// the remedy names the whole line, not only its comment.
func TestAFleetKeyThatWouldLeaveItsKickstartLineRefuses(t *testing.T) {
	lines := []string{
		"ssh-ed25519\tAAAAPUBLIC", "ssh-ed25519 AAAAPUBLIC\tfleet",
		"\tssh-ed25519 AAAAPUBLIC fleet\t", "ssh-ed25519 AAAAPUBLIC fleet\r",
	}
	for _, comment := range []string{
		"fleet\" \u2028%post --nochroot #\u2028touch /mnt/sysroot/root/pwned #\u2028%end #\u2028#",
		"a\u2028b", "a\u2029b", "a\u0085b", "a\x0bb", "a\rb", `a"b`, `a\b`,
	} {
		lines = append(lines, "ssh-ed25519 AAAAPUBLIC "+comment)
	}
	for _, line := range lines {
		call, _ := execution(t, "digest")
		call.Material["bootwright-machine-key"] = secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.PublicKeyPart:  []byte(line + "\n"),
			secrets.PrivateKeyPart: []byte("PRIVATE"),
		})
		runner := &fakeRunner{}
		_, err := New(runner).Apply(context.Background(), call)
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "api.value" || !strings.Contains(reported[0].Remediation, "bootwright-machine-key") || !strings.Contains(reported[0].Remediation, "public line") {
			t.Fatalf("line %+q: apply = %v %#v, want one api.value naming the Secret and its public line", line, err, reported)
		}
		if len(runner.requests) != 0 {
			t.Fatalf("line %+q reached the adapter", line)
		}
	}
}

// A controller that declares a trust bundle is reached through it on every
// operation: the request binds the bundle and the adapter receives it as its
// own file. One that declares none is reached through the system trust store,
// so nothing is bound or written for it.
func TestInstallationMaterialsIncludeTheControllerBundle(t *testing.T) {
	for name, bundle := range map[string]string{"declared": "lab-bmc-ca", "absent": ""} {
		t.Run(name, func(t *testing.T) {
			call, request := execution(t, "digest")
			request.Target.Controller.TrustBundleRef = bundle
			canonical, err := request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			call.Block.Request = canonical
			absent, _ := json.Marshal(Evidence{Absent: true, Postcondition: true, Request: "digest"})
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: absent}}
			if _, err := New(runner).Destroy(context.Background(), call); err != nil {
				t.Fatalf("destroy: %v", err)
			}
			var files []lifecycle.MaterialFile
			for _, file := range runner.requests[0].Materials {
				if file.Name == "bmc-ca" || file.Variable == "controllerCA" {
					files = append(files, file)
				}
			}
			bound := slices.Contains(request.SecretReferences(), "lab-bmc-ca")
			if bundle == "" {
				if len(files) != 0 || bound || strings.Contains(string(canonical), "trustBundleRef") {
					t.Fatalf("a controller with no bundle carried one: %+v, bound %t, %s", files, bound, canonical)
				}
				return
			}
			want := lifecycle.MaterialFile{Name: "bmc-ca", Part: secrets.CertificatePart, Secret: bundle, Variable: "controllerCA"}
			if len(files) != 1 || files[0] != want || !bound {
				t.Fatalf("bundle files = %+v, bound %t", files, bound)
			}
		})
	}
}

// A destroy removes published content and needs no key, so it never reopens
// the public half it does not use.
func TestADestroyCarriesNoAuthorizedKey(t *testing.T) {
	call, _ := execution(t, "digest")
	absent, _ := json.Marshal(Evidence{Absent: true, Postcondition: true, Request: "digest"})
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: absent}}
	if _, err := New(runner).Destroy(context.Background(), call); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, present := runner.requests[0].MaterialValues["authorizedKey"]; present {
		t.Fatal("a destroy reopened the fleet key's public half")
	}
}

// A guest that answers with a different marker is not this installation, so the
// attempt refuses rather than reporting a completion it cannot prove.
func TestApplyAcceptsOnlyTheMarkerItFroze(t *testing.T) {
	call, request := execution(t, "digest")
	marker, _ := MarkerFor(request, "digest")
	other, _ := MarkerFor(request, "another")
	for name, evidence := range map[string]json.RawMessage{
		"another marker":  completeEvidence(request, "digest", string(other)),
		"no host key":     mutate(request, "digest", string(marker), func(e *Evidence) { e.HostKey = "" }),
		"media inserted":  mutate(request, "digest", string(marker), func(e *Evidence) { e.Media = "install.iso" }),
		"powered off":     mutate(request, "digest", string(marker), func(e *Evidence) { e.Power = "Off" }),
		"tree missing":    mutate(request, "digest", string(marker), func(e *Evidence) { e.Tree = false }),
		"another address": mutate(request, "digest", string(marker), func(e *Evidence) { e.Address = "198.51.100.99" }),
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: evidence}}
			result, err := New(runner).Apply(context.Background(), call)
			if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
				t.Fatalf("result = %+v (%v)", result, err)
			}
		})
	}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: completeEvidence(request, "digest", string(marker))}}
	result, err := New(runner).Apply(context.Background(), call)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("a replay that proved the frozen marker = %+v (%v)", result, err)
	}
}

func mutate(request Request, digest, marker string, damage func(*Evidence)) json.RawMessage {
	var evidence Evidence
	_ = json.Unmarshal(completeEvidence(request, digest, marker), &evidence)
	damage(&evidence)
	data, _ := json.Marshal(evidence)
	return data
}

// An observation proves completion, positive absence, or nothing. A guest that
// answers with a different marker is not absence: it stays unknown.
func TestObservationMapsEvidenceToTheEffectItProves(t *testing.T) {
	call, request := execution(t, "digest")
	marker, _ := MarkerFor(request, "digest")
	other, _ := MarkerFor(request, "another")
	fresh, _ := json.Marshal(Evidence{Power: "Off", Request: "digest"})
	foreign, _ := json.Marshal(Evidence{Marker: string(other), Power: "On", Request: "digest"})
	absenceForm, _ := json.Marshal(Evidence{Absent: true, Postcondition: true, Request: "digest"})
	staged, _ := json.Marshal(Evidence{Power: "Off", Request: "digest", TreeStaging: true, Work: true})
	for name, test := range map[string]struct {
		runner *fakeRunner
		want   reconciliation.EffectState
	}{
		"complete":    {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: completeEvidence(request, "digest", string(marker))}}, reconciliation.EffectCompleted},
		"nothing yet": {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: fresh}}, reconciliation.EffectNoEffect},
		// An apply killed while it extracted the tree left a partial copy
		// beneath the served root, which is an effect this operation owns.
		"killed while staging the tree": {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: staged}}, reconciliation.EffectPartial},
		"another marker":                {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: foreign}}, reconciliation.EffectUnknown},
		// The absence form reports no power, so it can never prove that an
		// apply stopped before it published anything had no effect; the
		// observation publishes the presence form, which carries the power.
		"absence form, no power": {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: absenceForm}}, reconciliation.EffectUnknown},
		"failed":                 {&fakeRunner{err: errors.New("unreachable")}, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			observation, err := New(test.runner).Observe(context.Background(), call)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("observation = %+v (%v)", observation, err)
			}
		})
	}
}

// A removal withdraws published content whatever the guest holds and keeps
// the installed system, so its resolution reads only that content: none left
// is its completion whatever marker or power the guest reports, the whole
// completion is a removal that had no effect, and any content left is partial.
func TestARemovalObservationReadsWhatTheRemovalProves(t *testing.T) {
	call, request := execution(t, "digest")
	marker, _ := MarkerFor(request, "digest")
	other, _ := MarkerFor(request, "another")
	encode := func(evidence Evidence) json.RawMessage {
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for name, test := range map[string]struct {
		runner *fakeRunner
		want   reconciliation.EffectState
	}{
		"absent":                         {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Absent: true, Postcondition: true, Request: "digest"})}}, reconciliation.EffectCompleted},
		"never installed":                {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Power: "Off", Request: "digest"})}}, reconciliation.EffectCompleted},
		"installed and withdrawn":        {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Marker: string(marker), Power: "On", Request: "digest"})}}, reconciliation.EffectCompleted},
		"another installation":           {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Marker: string(other), Power: "On", Request: "digest"})}}, reconciliation.EffectCompleted},
		"complete":                       {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: completeEvidence(request, "digest", string(marker))}}, reconciliation.EffectNoEffect},
		"partial":                        {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: mutate(request, "digest", string(marker), func(e *Evidence) { e.Postcondition = false })}}, reconciliation.EffectPartial},
		"content under a foreign marker": {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Image: true, Marker: string(other), Power: "On", Request: "digest"})}}, reconciliation.EffectPartial},
		"content on a running guest":     {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Image: true, Power: "On", Request: "digest"})}}, reconciliation.EffectPartial},
		"tree and private left":          {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Marker: string(marker), Power: "On", Private: true, Tree: true, Request: "digest"})}}, reconciliation.EffectPartial},
		"private left":                   {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Marker: string(marker), Power: "On", Private: true, Request: "digest"})}}, reconciliation.EffectPartial},
		"content read alone, none left":  {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Request: "digest"})}}, reconciliation.EffectCompleted},
		"content read alone, image left": {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Image: true, Request: "digest"})}}, reconciliation.EffectPartial},
		"tree left without its marker":   {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Request: "digest", TreeContent: true})}}, reconciliation.EffectPartial},
		"staging tree left":              {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Request: "digest", TreeStaging: true})}}, reconciliation.EffectPartial},
		"work area left":                 {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Power: "Off", Request: "digest", Work: true})}}, reconciliation.EffectPartial},
		"another request":                {&fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(Evidence{Absent: true, Postcondition: true, Request: "another"})}}, reconciliation.EffectUnknown},
		"failed":                         {&fakeRunner{err: errors.New("unreachable")}, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			observation, err := New(test.runner).ObserveRemoval(context.Background(), call)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("removal observation = %+v (%v), want %s", observation, err, test.want)
			}
			if len(test.runner.requests) != 1 || test.runner.requests[0].Operation != "observe" {
				t.Fatalf("adapter invocation = %+v", test.runner.requests)
			}
			if test.runner.requests[0].MaterialValues["observes"] != "removal" {
				t.Fatalf("the removal observation was not scoped to the published content: %v", test.runner.requests[0].MaterialValues)
			}
		})
	}
}

// A removal's resolution reads the published content alone, while the apply's
// resolution reads the machine as well, so only the removal scopes the
// observation it asks the adapter for.
func TestOnlyARemovalScopesTheObservationToThePublishedContent(t *testing.T) {
	call, _ := execution(t, "digest")
	for name, test := range map[string]struct {
		observe func(Capability, context.Context, lifecycle.Execution) (lifecycle.Observation, error)
		want    string
	}{
		"apply":   {Capability.Observe, ""},
		"removal": {Capability.ObserveRemoval, "removal"},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: json.RawMessage(`{"request":"digest"}`)}}
			if _, err := test.observe(New(runner), context.Background(), call); err != nil {
				t.Fatal(err)
			}
			if len(runner.requests) != 1 {
				t.Fatalf("adapter invocations = %d", len(runner.requests))
			}
			scope, scoped := runner.requests[0].MaterialValues["observes"]
			if scope != test.want || scoped != (test.want != "") {
				t.Fatalf("observation scope = %q (%v), want %q", scope, scoped, test.want)
			}
		})
	}
}

// An operation registered before a physical installation or a private
// publication was refused still carries one in its frozen request. Its apply
// refuses before the adapter boots or publishes anything, naming the Machine,
// while its destroy and its observation still run, because they install
// nothing and are how the operator leaves that operation.
func TestAFrozenRefusedInstallationRefusesOnlyItsApply(t *testing.T) {
	for name, test := range map[string]struct {
		freeze  func(*Request)
		message string
	}{
		"physical target": {
			func(r *Request) { r.Target.Physical, r.Target.Hardware = true, &Hardware{RootDevice: "/dev/sda"} },
			"this operation froze a physical installation of Machine/rhel-01, which this executable refuses",
		},
		"private publication": {
			func(r *Request) {
				r.Private = &Publication{Path: "private/os/rhel-01", URL: "https://artifacts.lab.example.test/private/os/rhel-01"}
			},
			"this operation froze a private publication for Machine/rhel-01 that the publicly served installer image would expose, which this executable refuses",
		},
	} {
		t.Run(name, func(t *testing.T) {
			call, request := execution(t, "digest")
			test.freeze(&request)
			canonical, err := request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			call.Block.Request, call.Context = canonical, "lab-b"
			marker, _ := MarkerFor(request, "digest")
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: completeEvidence(request, "digest", string(marker))}}
			result, err := New(runner).Apply(context.Background(), call)
			if err == nil || result.Outcome != reconciliation.OutcomeFailed {
				t.Fatalf("an apply of a frozen refused installation = %+v (%v)", result, err)
			}
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != test.message {
				t.Fatalf("refusal = %#v", reported)
			}
			if reported[0].Remediation != "run bootwright destroy --context lab-b to end this operation, then plan it again under this executable" {
				t.Fatalf("remediation = %q", reported[0].Remediation)
			}
			if len(runner.requests) != 0 {
				t.Fatal("a refused apply reached the adapter")
			}
			absent, _ := json.Marshal(Evidence{Absent: true, Postcondition: true, Request: "digest"})
			runner = &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: absent}}
			if _, err := New(runner).Destroy(context.Background(), call); err != nil || len(runner.requests) != 1 {
				t.Fatalf("destroy of a frozen refused installation = %v after %d invocations", err, len(runner.requests))
			}
			fresh, _ := json.Marshal(Evidence{Power: "Off", Request: "digest"})
			runner = &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: fresh}}
			observation, err := New(runner).Observe(context.Background(), call)
			if err != nil || observation.Effect != reconciliation.EffectNoEffect {
				t.Fatalf("observation of a frozen refused installation = %+v (%v)", observation, err)
			}
		})
	}
}

// The operation binds the artifact server's serving certificate Secret whole,
// because the server's own block writes its key, but every run of an
// installation that fetches privately from that server is lent only the
// certificate it verifies the fetch against, and of every other Secret only
// the parts its material files name.
func TestAnInstallationRunIsLentOnlyTheServingCertificateOfTheServersSecret(t *testing.T) {
	for _, operation := range []string{"apply", "destroy", "observe"} {
		t.Run(operation, func(t *testing.T) {
			call, request := execution(t, "digest")
			request.Private = &Publication{Path: "private/os/rhel-01", URL: "https://artifacts.lab.example.test/private/os/rhel-01"}
			request.Target.HostKeyRef, request.TLSCertificateRef = "rhel-01-host-key", "lab-artifacts-tls"
			call.Material = fleetMaterial()
			call.Material["lab-bmc-credentials"] = secrets.NewMaterial(map[secrets.Part][]byte{
				secrets.UsernamePart: []byte("admin"), secrets.PasswordPart: []byte("BMC PASSWORD"),
			})
			call.Material["rhel-01-host-key"] = secrets.NewMaterial(map[secrets.Part][]byte{
				secrets.PrivateKeyPart: []byte("HOST KEY"), secrets.PublicKeyPart: []byte("ssh-ed25519 AAAAHOST\n"),
			})
			call.Material["lab-artifacts-tls"] = secrets.NewMaterial(map[secrets.Part][]byte{
				secrets.CertificatePart: []byte("-----BEGIN CERTIFICATE-----\nSERVING\n-----END CERTIFICATE-----\n"),
				secrets.PrivateKeyPart:  []byte("-----BEGIN PRIVATE KEY-----\nSERVING KEY\n-----END PRIVATE KEY-----\n"),
			})
			marker, _ := MarkerFor(request, "digest")
			runner := &fakeRunner{}
			capability := New(runner)
			if _, err := capability.run(context.Background(), call, operation, request, marker, ""); err != nil {
				t.Fatalf("run: %v", diagnostics.Of(err))
			}
			sent := runner.requests[0]
			if parts := sent.Material["lab-artifacts-tls"].Parts(); !slices.Equal(parts, []secrets.Part{secrets.CertificatePart}) {
				t.Fatalf("the serving certificate Secret is lent as %v", parts)
			}
			for name, material := range sent.Material {
				for _, part := range material.Parts() {
					if !slices.ContainsFunc(sent.Materials, func(file lifecycle.MaterialFile) bool { return file.Secret == name && file.Part == part }) {
						t.Fatalf("the run is lent the %s part of %s, which no material file names", part, name)
					}
				}
			}
		})
	}
}

func TestAnUnconfiguredCapabilityRefusesBeforeAnyEffect(t *testing.T) {
	call, _ := execution(t, "digest")
	if _, err := New(nil).Apply(context.Background(), call); err == nil {
		t.Fatal("an unconfigured installation capability ran")
	}
}

// An attempt whose fleet binding cannot be reopened refuses before it boots
// anything, because it could not authorize the account it installs.
func TestAnAttemptWithoutItsFleetBindingRefuses(t *testing.T) {
	call, _ := execution(t, "digest")
	call.Material = map[string]secrets.Material{"lab-bmc-credentials": secrets.NewMaterial(nil)}
	runner := &fakeRunner{}
	if _, err := New(runner).Apply(context.Background(), call); err == nil {
		t.Fatal("an attempt without its fleet binding ran")
	}
	if len(runner.requests) != 0 {
		t.Fatal("a refused attempt still reached the adapter")
	}
}

func TestUnsupportedReadsTheCompiledStateOrNothing(t *testing.T) {
	if unsupported := New(nil).Unsupported(nil); unsupported != nil {
		t.Fatalf("unsupported without state = %v", unsupported)
	}
	catalog := labCatalog(api.NewObject(api.MachineInstallProfile, "rhel-9-8", api.Value{}, api.MapValue(
		field("installer", api.MapValue(field("templateClone", api.MapValue()))),
	)))
	unsupported := New(nil).Unsupported(compilation.NewState(catalog, catalog, nil))
	want := []lifecycle.Refusal{{
		Kind: "Machine", Name: "rhel-01",
		Reason:      "this executable installs an operating system only through the anaconda installer, which the install profile does not select",
		Remediation: "select spec.installer.anaconda on MachineInstallProfile/rhel-9-8",
	}}
	if !slices.Equal(unsupported, want) {
		t.Fatalf("unsupported = %+v, want %+v", unsupported, want)
	}
}

// refusingRunner names one refusal, as the adapter does before it fails, and
// answers the diagnostic its caller remedies that refusal with.
type refusingRunner struct{ reason string }

func (r refusingRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	named, ok := request.Refusals[r.reason]
	if !ok {
		return lifecycle.RunResult{}, errors.New("the refusal breaks the protocol")
	}
	return lifecycle.RunResult{}, named
}

// A store entry changed after the plan froze it is refused by the attempt
// before its first use, and that refusal reaches the operator naming the
// Machine, the image and the way back.
func TestAnApplyNamesAChangedStoreEntry(t *testing.T) {
	call, _ := execution(t, strings.Repeat("1", 64))
	call.Context = "lab-b"
	_, err := New(refusingRunner{reason: "media-changed-tree"}).Apply(context.Background(), call)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Object == nil || reported[0].Object.Kind != "Machine" || reported[0].Object.Name != "rhel-01" {
		t.Fatalf("diagnostics = %#v (%v)", reported, err)
	}
	if !strings.Contains(reported[0].Message, dvdImageName) {
		t.Fatalf("the message %q does not name the DVD", reported[0].Message)
	}
	want := "take this context back with bootwright destroy --context lab-b, import the image again with bootwright media add --name " +
		dvdImageName + " --from-file <path>, or --from-url <url> --sha256 <digest>, then run bootwright apply --context lab-b"
	if reported[0].Remediation != want {
		t.Fatalf("remediation = %q", reported[0].Remediation)
	}
}

// An https publication is fetched through its listener before any machine is
// given it, verified against the certificate its server presents, so the
// request names that certificate, the operation freezes it and an apply binds
// its public half. An http publication names none.
func TestAnHttpsPublicationBindsItsServingCertificate(t *testing.T) {
	call, request := execution(t, "digest")
	if request.Image.CertificateRef != "lab-artifacts-tls" {
		t.Fatalf("the https image names certificate %q", request.Image.CertificateRef)
	}
	if request.Tree == nil || request.Tree.CertificateRef != "" {
		t.Fatalf("the http tree = %+v", request.Tree)
	}
	if !slices.Contains(request.SecretReferences(), "lab-artifacts-tls") {
		t.Fatalf("secret references = %v", request.SecretReferences())
	}
	runner := &fakeRunner{err: errors.New("stop")}
	_, _ = New(runner).Apply(context.Background(), call)
	if len(runner.requests) != 1 {
		t.Fatalf("runs = %d", len(runner.requests))
	}
	want := lifecycle.MaterialFile{Name: "image-ca", Part: secrets.CertificatePart, Secret: "lab-artifacts-tls", Variable: "imageCertificate"}
	if !slices.Contains(runner.requests[0].Materials, want) {
		t.Fatalf("materials = %+v", runner.requests[0].Materials)
	}
	for _, file := range runner.requests[0].Materials {
		if file.Name == "tree-ca" {
			t.Fatalf("the http tree bound a certificate: %+v", file)
		}
	}
	secured := request
	tree := *request.Tree
	tree.CertificateRef = "tree-tls"
	secured.Tree = &tree
	if !slices.Contains(secured.SecretReferences(), "tree-tls") {
		t.Fatalf("secret references = %v", secured.SecretReferences())
	}
	treeFile := lifecycle.MaterialFile{Name: "tree-ca", Part: secrets.CertificatePart, Secret: "tree-tls", Variable: "treeCertificate"}
	if !slices.Contains(publicationCertificates(secured), treeFile) {
		t.Fatalf("an https tree bound %+v", publicationCertificates(secured))
	}
}
