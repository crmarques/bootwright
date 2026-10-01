package lifecycle

import (
	"bytes"
	"context"
	"path"
	"reflect"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// A fresh plan previews the very decision a fresh apply registers. A preview
// that succeeds is followed by an apply that registers exactly the plan it
// showed, and a preview that refuses reports the refusal apply makes before it
// registers anything, with the same code, message and remedy. Either way the
// preview writes, binds and locks nothing a read does not.
func TestFreshPlanAndApplyShareOneDecision(t *testing.T) {
	controlled := append([]reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	}, nestedDefinitions()...)
	cases := []struct {
		name        string
		definitions []reconciliation.BlockDefinition
		prepare     func(*harness)
		stages      []string
		// refusal is the code both verbs report, or empty for a plan both
		// accept.
		refusal string
	}{
		{name: "a valid plan", definitions: nestedDefinitions()},
		{name: "a valid selection", definitions: nestedDefinitions(), stages: []string{"substrates"}},
		{
			name: "an unsupported shape", definitions: nestedDefinitions(), refusal: "lifecycle.unsupported",
			prepare: func(h *harness) {
				h.capability.unsupported = []Refusal{{Kind: "ContainerCluster", Name: "sno", Reason: "unsupported"}}
			},
		},
		{
			name: "an unclaimed kind", definitions: nestedDefinitions(), refusal: "lifecycle.unsupported",
			prepare: func(h *harness) { h.service.compiler = testCompiler{state: withEnabledPlaybook()} },
		},
		{name: "a zero-block plan", refusal: "lifecycle.state"},
		{name: "a stage boundary", definitions: controlled, stages: []string{"infra-components"}, refusal: "lifecycle.stage"},
		{
			name: "its own sockets in conflict", definitions: nestedDefinitions(), refusal: "api.invariant",
			prepare: func(h *harness) { h.capability.reservations = conflictingSockets() },
		},
		{
			name: "a wildcard beside its own endpoint socket", definitions: nestedDefinitions(),
			prepare: func(h *harness) {
				h.capability.reservations = append(reservationOf("alpha"), prerequisites.HostReservation{
					Context: testContextName, Kind: "proxy", Service: "beta", Keys: []string{"socket:0.0.0.0:3128", "socket:192.0.2.1:3128"},
				})
			},
		},
		{
			name: "its own sockets in conflict on one SSH host", definitions: nestedDefinitions(), refusal: "api.invariant",
			prepare: func(h *harness) { h.capability.sshClaims = sshSockets("services", "services") },
		},
		{
			name: "one socket on two SSH hosts", definitions: nestedDefinitions(),
			prepare: func(h *harness) { h.capability.sshClaims = sshSockets("services", "storage") },
		},
		{
			name: "its own sockets in conflict through two Machines on one SSH host", definitions: nestedDefinitions(), refusal: "api.invariant",
			prepare: func(h *harness) { h.capability.sshClaims = sshSockets("services", "alias") },
		},
		{
			name: "one socket at one SSH address and two ports", definitions: nestedDefinitions(),
			prepare: func(h *harness) { h.capability.sshClaims = sshSockets("services", "forwarded") },
		},
		{
			name: "an SSH host's socket that the controller also claims", definitions: nestedDefinitions(),
			prepare: func(h *harness) {
				h.capability.reservations = reservationOf("alpha")
				h.capability.sshClaims = []SSHReservation{sshClaim("services", reservationOf("beta")[0])}
			},
		},
		{
			name: "a controller socket claimed again through loopback SSH", definitions: nestedDefinitions(), refusal: "api.invariant",
			prepare: func(h *harness) {
				h.capability.reservations = reservationOf("alpha")
				h.capability.sshClaims = []SSHReservation{sshClaim("loopback", reservationOf("beta")[0])}
			},
		},
		{
			name: "a controller socket claimed again through the controller's own address", definitions: nestedDefinitions(), refusal: "api.invariant",
			prepare: func(h *harness) {
				h.service.compiler = testCompiler{state: withControllerAddress()}
				h.capability.reservations = reservationOf("alpha")
				h.capability.sshClaims = []SSHReservation{sshClaim("declared", reservationOf("beta")[0])}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newPlannedHarness(t, tc.definitions)
			if tc.prepare != nil {
				tc.prepare(h)
			}
			preview, previewErr := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab", Stages: tc.stages})
			if h.workspace.mutations != 0 || h.workspace.runs != 0 || len(h.workspace.area.files) != 0 || len(h.binder.bound) != 0 {
				t.Fatal("the preview locked, bound or wrote durable state")
			}
			result, applyErr := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true, Stages: tc.stages})
			if tc.refusal != "" {
				agreeOnRefusal(t, h, tc.refusal, previewErr, applyErr)
				return
			}
			if previewErr != nil || applyErr != nil {
				t.Fatalf("preview = %v, apply = %v", previewErr, applyErr)
			}
			agreeOnPlan(t, h, preview, result)
		})
	}
}

// conflictingSockets is two of one context's own services on one port, one of
// them bound to a wildcard, so they could never both listen.
func conflictingSockets() []prerequisites.HostReservation {
	return append(reservationOf("alpha"), prerequisites.HostReservation{
		Context: testContextName, Kind: "proxy", Service: "beta", Keys: []string{"socket::::8443", "socket:fd00::1:8443"},
	})
}

// Two of one context's own blocks whose controller sockets conflict are
// refused while the context plans, naming both and the socket each claims,
// rather than failing at run time when the second one starts listening.
func TestAPlanWhoseOwnSocketsConflictNamesBoth(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.reservations = conflictingSockets()
	_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "api.invariant" ||
		reported[0].Message != "this context's artifact-server alpha at 192.0.2.1:8443 and proxy beta at [::]:8443 cannot both listen on the controller" ||
		reported[0].Remediation != "give one of them another bind address or port" {
		t.Fatalf("refusal = %+v (%v)", reported, err)
	}
}

// sshEndpoints is the SSH address and port each test Machine's placement
// reaches: services and storage are two hosts, alias reaches the host services
// does, forwarded reaches services' address at another port, loopback reaches
// the controller, and declared reaches the address withControllerAddress
// gives the controller.
var sshEndpoints = map[string]struct {
	address string
	port    int
}{
	"services": {"192.0.2.10", 22}, "storage": {"192.0.2.20", 22}, "alias": {"192.0.2.10", 22},
	"forwarded": {"192.0.2.10", 2222}, "loopback": {"127.0.0.1", 22}, "declared": {"192.0.2.1", 22},
}

// sshClaim is one claim placed through a test Machine's SSH access.
func sshClaim(machine string, reservation prerequisites.HostReservation) SSHReservation {
	endpoint := sshEndpoints[machine]
	return SSHReservation{Machine: machine, Address: endpoint.address, Port: endpoint.port, Reservation: reservation}
}

// sshSockets is two of one context's own services at one port, the first bound
// to a wildcard, each placed on the SSH host its Machine reaches.
func sshSockets(first, second string) []SSHReservation {
	return []SSHReservation{
		sshClaim(first, prerequisites.HostReservation{Context: testContextName, Kind: "proxy", Service: "gamma", Keys: []string{"socket:0.0.0.0:3128"}}),
		sshClaim(second, prerequisites.HostReservation{Context: testContextName, Kind: "dns", Service: "delta", Keys: []string{"socket:192.0.2.9:3128"}}),
	}
}

// withControllerAddress is the harness's Environment with its controller
// Machine, which declares 192.0.2.1 among its addresses.
func withControllerAddress() *compilation.State {
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
		)),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
			api.FieldValue{Name: "network", Value: api.MapValue(api.FieldValue{Name: "addresses", Value: api.ListValue(
				api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("fqdn")}, api.FieldValue{Name: "address", Value: api.StringValue("controller.lab.example.test")}),
				api.MapValue(api.FieldValue{Name: "name", Value: api.StringValue("lab")}, api.FieldValue{Name: "address", Value: api.StringValue("192.0.2.1/24")}),
			)})},
		)),
	})
	return compilation.NewState(catalog, catalog, nil)
}

// Two Machines whose SSH access reaches one address and port are one host, so
// their blocks' conflicting sockets refuse while the context plans, naming
// that host and both Machines, as they would under one Machine's name.
func TestTwoMachinesReachingOneSSHHostConflictThere(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.sshClaims = sshSockets("services", "alias")
	_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "api.invariant" ||
		reported[0].Message != "this context's proxy gamma at 0.0.0.0:3128 and dns delta at 192.0.2.9:3128 cannot both listen on the SSH host at 192.0.2.10:22, which Machine/alias and Machine/services both reach" ||
		reported[0].Remediation != "give one of them another bind address or port" {
		t.Fatalf("refusal = %+v (%v)", reported, err)
	}
}

// A Machine whose SSH access reaches the controller, over loopback or at an
// address the controller Machine declares, places its blocks on the
// controller, so a socket it claims that the controller's own blocks or
// another such Machine's also claim refuses while the context plans, naming
// the controller and each Machine that reaches it.
func TestAMachineReachingTheControllerConflictsThere(t *testing.T) {
	for name, tc := range map[string]struct {
		reservations []prerequisites.HostReservation
		claims       []SSHReservation
		host         string
	}{
		"over loopback beside the controller's own": {
			reservations: reservationOf("alpha"), claims: []SSHReservation{sshClaim("loopback", reservationOf("beta")[0])},
			host: "the controller, which Machine/loopback reaches over SSH",
		},
		"at a declared address beside the controller's own": {
			reservations: reservationOf("alpha"), claims: []SSHReservation{sshClaim("declared", reservationOf("beta")[0])},
			host: "the controller, which Machine/declared reaches over SSH",
		},
		"through two Machines": {
			claims: []SSHReservation{sshClaim("loopback", reservationOf("alpha")[0]), sshClaim("declared", reservationOf("beta")[0])},
			host:   "the controller, which Machine/declared and Machine/loopback both reach over SSH",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newPlannedHarness(t, nestedDefinitions())
			h.service.compiler = testCompiler{state: withControllerAddress()}
			h.capability.reservations, h.capability.sshClaims = tc.reservations, tc.claims
			_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
			reported := diagnostics.Of(err)
			want := "this context's artifact-server alpha at 192.0.2.1:8443 and artifact-server beta at 192.0.2.1:8443 cannot both listen on " + tc.host
			if len(reported) != 1 || reported[0].Code != "api.invariant" || reported[0].Message != want {
				t.Fatalf("refusal = %+v (%v), want %q", reported, err, want)
			}
		})
	}
}

// Two of one context's own blocks placed on one SSH host whose sockets
// conflict are refused while the context plans, naming both, the socket each
// claims and that host, and a plan with conflicts on two hosts names the first
// host in name order.
func TestAPlanWhoseOwnSocketsConflictOnAnSSHHostNamesTheHost(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.sshClaims = append(sshSockets("storage", "storage"), sshSockets("services", "services")...)
	_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "api.invariant" ||
		reported[0].Message != "this context's proxy gamma at 0.0.0.0:3128 and dns delta at 192.0.2.9:3128 cannot both listen on the SSH host Machine/services" ||
		reported[0].Remediation != "give one of them another bind address or port" {
		t.Fatalf("refusal = %+v (%v)", reported, err)
	}
}

// An SSH host's claims are compared within the context and never published,
// because two contexts targeting one SSH host are not coordinated: the apply
// reserves its controller claims alone.
func TestAnApplyPublishesOnlyItsControllerClaims(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.reservations = reservationOf("alpha")
	h.capability.sshClaims = sshSockets("services", "storage")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(h.workspace.reservations, reservationOf("alpha")) {
		t.Fatalf("published %+v, want the controller's claims alone", h.workspace.reservations)
	}
}

// A completed destroy leaves the context where the next plan is fresh again,
// so the preview it offers is the same decision the next apply takes.
func TestAFreshPlanAfterADestroyRefusesAsItsApplyDoes(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.unsupported = []Refusal{{Kind: "Machine", Name: "guest", Reason: "unsupported"}}
	mutations, files := h.workspace.mutations, len(h.workspace.area.files)
	_, previewErr := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if h.workspace.mutations != mutations || len(h.workspace.area.files) != files {
		t.Fatal("the preview wrote durable state")
	}
	_, applyErr := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(previewErr); code != "lifecycle.unsupported" {
		t.Fatalf("preview after a destroy = %q (%v)", code, previewErr)
	}
	if !reflect.DeepEqual(diagnostics.Of(previewErr), diagnostics.Of(applyErr)) {
		t.Fatalf("preview reported %+v, apply reported %+v", diagnostics.Of(previewErr), diagnostics.Of(applyErr))
	}
}

// previewCase is one durable state, the service over it, and the verb its
// preview decides as, run with the stages the row selects and the tokens its
// plan consumes.
type previewCase struct {
	service   Service
	workspace *testWorkspace
	presenter *testPresenter
	verb      reconciliation.Verb
	stages    []string
	tokens    []string
}

func (c previewCase) run(ctx context.Context) (*OperationResult, error) {
	if c.verb == reconciliation.Destroy {
		return c.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Authorizations: c.tokens, SkipConfirmation: true})
	}
	return c.service.Apply(ctx, ApplyRequest{ContextName: testContextName, Authorizations: c.tokens, SkipConfirmation: true, Stages: c.stages})
}

// harnessCase passes the token only where the capability consumes it, since a
// token the plan does not consume refuses.
func harnessCase(h *harness, verb reconciliation.Verb, stages ...string) previewCase {
	var tokens []string
	if len(h.capability.consumes) != 0 {
		tokens = dataLoss()
	}
	return previewCase{service: h.service, workspace: h.workspace, presenter: h.presenter, verb: verb, stages: stages, tokens: tokens}
}

func killedCase(run killedRun, verb reconciliation.Verb) previewCase {
	service, _ := run.repairing()
	return previewCase{service: service, workspace: run.snapshot.workspace, presenter: run.rig.harness.presenter, verb: verb, tokens: dataLoss()}
}

// previewRow is one state a preview decides over, and what its verb then does:
// refuses with refusal (and message, where the row names it), settles after
// only a finalization, or presents the plan of shown. A continuation names the
// receipt's next action; a fresh plan names none. fails is the code a verb
// that presented its plan then fails with.
type previewRow struct {
	name             string
	prepare          func(t *testing.T) previewCase
	refusal, message string
	settles          bool
	shown            reconciliation.Verb
	next, fails      string
}

// Every preview is the decision of the verb it previews. It refuses where that
// verb refuses, with the same diagnostics; it says so where that verb only
// completes an interrupted finalization and settles; and otherwise it shows the
// very plan that verb then presents. It writes, binds and locks nothing.
func TestEveryPreviewDecidesAsItsVerbDoes(t *testing.T) {
	ctx := context.Background()
	for _, row := range previewRows(ctx) {
		t.Run(row.name, func(t *testing.T) {
			c := row.prepare(t)
			current := currentIn(t, c.service)
			records, evidence, mutations := c.workspace.area.clone(), slices.Clone(c.workspace.evidence), c.workspace.mutations
			preview, previewErr := c.service.Plan(ctx, PlanRequest{ContextName: testContextName, Stages: c.stages})
			if !sameFiles(c.workspace.area, records) || !bytes.Equal(c.workspace.evidence, evidence) || c.workspace.mutations != mutations {
				t.Fatal("the preview wrote durable state or took the exclusive lock")
			}
			presented := len(c.presenter.presented)
			result, verbErr := c.run(ctx)
			switch {
			case row.refusal != "":
				reported := diagnostics.Of(previewErr)
				if firstCode(previewErr) != row.refusal || (row.message != "" && reported[0].Message != row.message) ||
					!reflect.DeepEqual(reported, diagnostics.Of(verbErr)) {
					t.Fatalf("preview = %+v (%v), %s = %+v (%v)", reported, previewErr, c.verb, diagnostics.Of(verbErr), verbErr)
				}
			case row.settles:
				want := Receipt{Operation: current, Verb: "plan", State: "preview", Next: "continue-" + string(c.verb)}
				if previewErr != nil || verbErr != nil || !preview.Finalizes || !preview.Continuation || preview.Receipt != want ||
					preview.Verb != string(c.verb) || !result.Settled || result.Recovered != RecoveredFinalization ||
					result.Receipt.Operation != current || len(c.presenter.presented) != presented {
					t.Fatalf("preview = %+v (%v), %s = %+v (%v)", preview, previewErr, c.verb, result, verbErr)
				}
			default:
				agreeOnPresentation(t, row, c, current, preview, previewErr, verbErr, presented)
			}
		})
	}
}

// agreeOnPresentation proves the verb presented exactly the plan its preview
// showed, under the receipt the row names.
func agreeOnPresentation(t *testing.T, row previewRow, c previewCase, current string, preview *PlanResult, previewErr, verbErr error, presented int) {
	t.Helper()
	if previewErr != nil || (verbErr != nil && firstCode(verbErr) != row.fails) || (verbErr == nil && row.fails != "") {
		t.Fatalf("preview = %v, %s = %v", previewErr, c.verb, verbErr)
	}
	if preview.Finalizes || len(c.presenter.presented) != presented+1 {
		t.Fatalf("preview = %+v, the %s presented %d plans", preview, c.verb, len(c.presenter.presented)-presented)
	}
	shown, previewed := c.presenter.presented[presented], *preview
	shown.Context, shown.Receipt, previewed.Context, previewed.Receipt = ContextIdentity{}, Receipt{}, ContextIdentity{}, Receipt{}
	if !reflect.DeepEqual(shown, previewed) {
		t.Fatalf("the %s presented %+v, the preview showed %+v", c.verb, shown, previewed)
	}
	want := Receipt{Operation: "none", Verb: "plan", State: "preview", Next: string(row.shown)}
	if row.next != "" {
		want = Receipt{Operation: current, Verb: "plan", State: "preview", Next: row.next}
	}
	if preview.Receipt != want || preview.Verb != string(row.shown) || preview.Context.Name != testContextName {
		t.Fatalf("preview receipt = %+v verb %s, want %+v verb %s", preview.Receipt, preview.Verb, want, row.shown)
	}
}

func currentIn(t *testing.T, service Service) string {
	t.Helper()
	status, err := service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle == nil {
		return ""
	}
	return status.Lifecycle.Operation
}

func previewRows(ctx context.Context) []previewRow {
	applied := []reconciliation.Verb{reconciliation.Apply}
	destroyed := func(t *testing.T, h *harness) {
		t.Helper()
		completeApply(t, h)
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
	}
	paused := func(t *testing.T, stages string) previewCase {
		h := newPlannedHarness(t, nestedDefinitions())
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true, Stages: []string{"infra-components"}}); err != nil {
			t.Fatal(err)
		}
		return harnessCase(h, reconciliation.Apply, stages)
	}
	removalOutcome := func(t *testing.T, h *harness, outcome reconciliation.Outcome) {
		t.Helper()
		completeApply(t, h)
		h.capability.outcomes = []Result{{Outcome: outcome}}
		if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
			t.Fatalf("a removal whose block reported %s reported success", outcome)
		}
	}
	return []previewRow{
		{name: "a destroy of a completed apply", shown: reconciliation.Destroy, prepare: func(t *testing.T) previewCase {
			h := newHarness(t, "alpha", "bravo")
			completeApply(t, h)
			return harnessCase(h, reconciliation.Destroy)
		}},
		{name: "a destroy of a completed apply whose finalization is due", shown: reconciliation.Destroy, prepare: func(t *testing.T) previewCase {
			h := newHarness(t, "alpha", "bravo")
			completeApply(t, h)
			h.workspace.evidence = evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
			return harnessCase(h, reconciliation.Destroy)
		}},
		{name: "a destroy of a completed apply with a lost block record", refusal: "lifecycle.state", prepare: func(t *testing.T) previewCase {
			h := newHarness(t, "alpha", "bravo", "charlie")
			current := completeApply(t, h)
			h.workspace.area.mutex.Lock()
			delete(h.workspace.area.files, path.Join(current, "blocks", "bravo", "state.json"))
			h.workspace.area.mutex.Unlock()
			return harnessCase(h, reconciliation.Destroy)
		}},
		{name: "a fresh apply after a completed destroy", shown: reconciliation.Apply, prepare: func(t *testing.T) previewCase {
			h := newHarness(t, "alpha", "bravo")
			destroyed(t, h)
			return harnessCase(h, reconciliation.Apply)
		}},
		{name: "a fresh apply after a completed destroy whose finalization is due", shown: reconciliation.Apply, prepare: func(t *testing.T) previewCase {
			h := newHarness(t, "alpha", "bravo")
			destroyed(t, h)
			h.workspace.evidence = evidenceBytes(t, reconciliation.Destroy, reconciliation.OperationRunning)
			return harnessCase(h, reconciliation.Apply)
		}},
		{
			name: "a continuation whose selection admits nothing", refusal: "lifecycle.stage", message: "the selected stages have nothing to start",
			prepare: func(t *testing.T) previewCase { return paused(t, "clusters") },
		},
		{
			name: "a continuation whose selection excludes the failed block", refusal: "lifecycle.stage",
			message: "the block this operation must retry is outside the selected stages",
			prepare: func(t *testing.T) previewCase {
				h := newHarness(t, "alpha")
				failApply(t, h, "alpha")
				return harnessCase(h, reconciliation.Apply, "machines")
			},
		},
		{
			name: "a continuation whose selection admits work", shown: reconciliation.Apply, next: "continue-apply",
			prepare: func(t *testing.T) previewCase { return paused(t, "substrates") },
		},
		{name: "a running apply whose blocks are all done", settles: true, prepare: func(t *testing.T) previewCase {
			return killedCase(killedAt(ctx, t, nil, reconciliation.Apply, "replace <op>/operation.json#1"), reconciliation.Apply)
		}},
		{
			name: "a running apply whose blocks are all done over a changed input", refusal: "lifecycle.state",
			message: "the desired state changed after this apply completed",
			prepare: func(t *testing.T) previewCase {
				c := killedCase(killedAt(ctx, t, nil, reconciliation.Apply, "replace <op>/operation.json#1"), reconciliation.Apply)
				c.workspace.inputs = desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
					desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
				}}
				return c
			},
		},
		{name: "a running destroy whose blocks are all done", settles: true, prepare: func(t *testing.T) previewCase {
			return killedCase(killedAt(ctx, t, applied, reconciliation.Destroy, "replace <op>/operation.json#1"), reconciliation.Destroy)
		}},
		{name: "an unknown apply whose blocks are all done", settles: true, prepare: func(t *testing.T) previewCase {
			return harnessCase(unknownApplyWithEveryBlockDone(t), reconciliation.Apply)
		}},
		{name: "an unknown destroy whose blocks are all done", settles: true, prepare: func(t *testing.T) previewCase {
			return harnessCase(unknownDestroyWithEveryBlockDone(t), reconciliation.Destroy)
		}},
		{name: "a failed destroy", shown: reconciliation.Destroy, prepare: func(t *testing.T) previewCase {
			h := newHarness(t, "alpha", "bravo")
			removalOutcome(t, h, reconciliation.OutcomeFailed)
			return harnessCase(h, reconciliation.Destroy)
		}},
		{name: "a failed destroy whose blocks are all done", settles: true, prepare: func(t *testing.T) previewCase {
			h := newHarness(t, "alpha")
			removalOutcome(t, h, reconciliation.OutcomeFailed)
			rewriteState(t, h, path.Join(currentOperation(t, h), "blocks", "alpha", "state.json"), string(reconciliation.BlockDone))
			return harnessCase(h, reconciliation.Destroy)
		}},
		{
			// The harness answers an unscripted observation as still unknown,
			// so the repeated destroy presents its plan and then fails again.
			name: "an unknown destroy that still holds an unresolved block", shown: reconciliation.Destroy, next: "resolve", fails: "lifecycle.unknown",
			prepare: func(t *testing.T) previewCase {
				h := newHarness(t, "alpha", "bravo")
				removalOutcome(t, h, reconciliation.OutcomeUnknown)
				return harnessCase(h, reconciliation.Destroy)
			},
		},
	}
}

// A destroy accepts no stage selection, so a preview of one refuses any
// selection, over a completed apply and over a failed destroy alike, and
// writes nothing.
func TestADestroyPreviewRefusesAStageSelection(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, h *harness){
		"a completed apply": func(t *testing.T, h *harness) { completeApply(t, h) },
		"a failed destroy": func(t *testing.T, h *harness) {
			completeApply(t, h)
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
			if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
				t.Fatal("a failed removal reported success")
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			prepare(t, h)
			records, evidence, mutations := h.workspace.area.clone(), slices.Clone(h.workspace.evidence), h.workspace.mutations
			_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName, Stages: []string{"infra-components"}})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "lifecycle.stage" || reported[0].Remediation != "repeat bootwright plan without --stage" {
				t.Fatalf("preview = %+v (%v)", reported, err)
			}
			if !sameFiles(h.workspace.area, records) || !bytes.Equal(h.workspace.evidence, evidence) || h.workspace.mutations != mutations {
				t.Fatal("the refused preview wrote durable state")
			}
		})
	}
}

// agreeOnRefusal proves both verbs refused with identical diagnostics and that
// the apply refused before it presented, locked or registered anything.
func agreeOnRefusal(t *testing.T, h *harness, code string, previewErr, applyErr error) {
	t.Helper()
	if firstCode(applyErr) != code {
		t.Fatalf("apply refusal = %q (%v)", firstCode(applyErr), applyErr)
	}
	if !reflect.DeepEqual(diagnostics.Of(previewErr), diagnostics.Of(applyErr)) {
		t.Fatalf("preview reported %+v (%v), apply reported %+v", diagnostics.Of(previewErr), previewErr, diagnostics.Of(applyErr))
	}
	if len(h.presenter.presented) != 0 || h.workspace.mutations != 0 || len(h.workspace.area.files) != 0 || len(h.binder.bound) != 0 {
		t.Fatal("the refused apply presented, bound or registered")
	}
}

// agreeOnPlan proves the apply presented exactly the preview and froze exactly
// the blocks it showed.
func agreeOnPlan(t *testing.T, h *harness, preview *PlanResult, result *OperationResult) {
	t.Helper()
	if preview.Receipt != (Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "apply"}) || preview.Verb != "apply" {
		t.Fatalf("preview = %+v", preview)
	}
	if len(h.presenter.presented) != 1 {
		t.Fatalf("apply presented %d plans", len(h.presenter.presented))
	}
	presented, shown := h.presenter.presented[0], *preview
	presented.Context, presented.Receipt, shown.Context, shown.Receipt = ContextIdentity{}, Receipt{}, ContextIdentity{}, Receipt{}
	if !reflect.DeepEqual(presented, shown) {
		t.Fatalf("apply presented %+v, plan previewed %+v", presented, shown)
	}
	store := operationstore.New(h.workspace.area, time.Now)
	frozen, err := store.ReadPlan(context.Background(), result.Receipt.Operation)
	if err != nil {
		t.Fatal(err)
	}
	registered := steps(frozen, nil)
	unmarked := make([]PlanStep, len(preview.Steps))
	for index, step := range preview.Steps {
		step.Selection, step.WaitsOn = "", ""
		unmarked[index] = step
	}
	if !reflect.DeepEqual(registered, unmarked) {
		t.Fatalf("apply registered %+v, plan previewed %+v", registered, unmarked)
	}
}

// withEnabledPlaybook is the harness's Environment with an enabled
// CustomPlaybook, an object no capability realizes.
func withEnabledPlaybook() *compilation.State {
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
		)),
		api.NewObject(api.CustomPlaybook, "tune", api.Value{}, api.MapValue(
			api.FieldValue{Name: "enabled", Value: api.BoolValue(true)},
		)),
	})
	return compilation.NewState(catalog, catalog, nil)
}
