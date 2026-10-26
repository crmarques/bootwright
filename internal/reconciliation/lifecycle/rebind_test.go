package lifecycle

import (
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// rotatedHarness applies alpha, whose frozen request names the BMC credential
// bmc, binding its first version, and then rotates bmc, as an operator who
// changed the controller's password and imported the new one leaves it. The
// controller accepts only the current password from then on.
func rotatedHarness(t *testing.T, access []string) (*harness, *[]string) {
	t.Helper()
	alpha := definition("alpha")
	alpha.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"alpha"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{alpha})
	h.capability.secrets = []string{"bmc"}
	h.capability.access = map[string][]string{"alpha": access}
	h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	h.binder.versions = map[string]string{"bmc": "ver-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second")}
	completeApply(t, h)
	h.binder.versions["bmc"] = "ver-2"
	return h, presentedTo(h)
}

// presentedTo makes the controller accept only the second password, and
// lists every password it was presented, in order, read before the engine
// clears the material it lent.
func presentedTo(h *harness) *[]string {
	seen := &[]string{}
	h.capability.presenting = func(execution Execution) bool {
		*seen = append(*seen, presented(execution))
		return presented(execution) == "second"
	}
	return seen
}

func password(value string) secrets.Material {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte(value)})
}

// presented is the BMC password an execution was lent.
func presented(execution Execution) string {
	value, _ := execution.Material["bmc"].Part(secrets.PasswordPart)
	return string(value)
}

func destroyWith(h *harness, authorizations ...string) error {
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Authorizations: authorizations, SkipConfirmation: true})
	return err
}

// A rotated BMC credential strands the removal that presents the bound
// version, and the recorded rebind exit removes it: the removal registers with
// a new binding version that pins the current password, runs the block's own
// removal, which proves what it takes back, and releases both versions once
// it completes. The apply's binding is never rewritten.
func TestARotatedAccessCredentialIsRemovedOnlyThroughTheRebindExit(t *testing.T) {
	h, seen := rotatedHarness(t, []string{"bmc"})
	applied, _ := durableOperation(t, h)
	if err := destroyWith(h); err == nil {
		t.Fatal("a removal presenting the rotated-out password completed")
	}
	if !slices.Equal(*seen, []string{"first"}) || len(h.binder.rebinds) != 0 {
		t.Fatalf("the removal presented %q after %d rebinds, want only the bound version", *seen, len(h.binder.rebinds))
	}
	failed, _ := durableOperation(t, h)
	if failed.Verb != reconciliation.Destroy || failed.State != reconciliation.OperationFailed || !slices.Equal(failed.Bindings, applied.Bindings) {
		t.Fatalf("the stranded removal = %s %s naming %v, want a failed destroy naming %v", failed.Verb, failed.State, failed.Bindings, applied.Bindings)
	}

	destroys := len(h.capability.destroys)
	if err := destroyWith(h, RebindAuthorization+"bmc"); err != nil {
		t.Fatal(err)
	}
	if len(h.binder.rebinds) != 1 || h.binder.rebinds[0].BindingID != applied.Bindings[0] || !slices.Equal(h.binder.rebinds[0].Names, []string{"bmc"}) {
		t.Fatalf("rebinds = %+v, want one of %s naming bmc", h.binder.rebinds, applied.Bindings[0])
	}
	rebound := "bind-2"
	if h.binder.pins[rebound]["bmc"] != "ver-2" || h.binder.pins[applied.Bindings[0]]["bmc"] != "ver-1" {
		t.Fatalf("pins = %v, want %s at ver-2 beside the apply's binding at ver-1", h.binder.pins, rebound)
	}
	if len(h.capability.destroys) != destroys+1 || !slices.Equal(*seen, []string{"first", "second"}) {
		t.Fatalf("the rebound removal ran %v presenting %q", h.capability.destroys[destroys:], *seen)
	}
	for _, binding := range []string{applied.Bindings[0], rebound} {
		if !slices.Contains(h.binder.released, binding) {
			t.Fatalf("released = %v, want %s released by the completed removal", h.binder.released, binding)
		}
	}
	log := string(h.workspace.area.files[path.Join(currentOperation(t, h), "logs", "operation.jsonl")])
	if !strings.Contains(log, `"event":"rebind"`) || !strings.Contains(log, applied.Bindings[0]+" rebound as "+rebound+" at the current version of bmc") {
		t.Fatalf("operation log = %s, want the rebind recorded", log)
	}
}

// A continuation rebinds the same way, and writes the new binding version
// into its own operation's record, so every later attempt and its removal
// reopen it.
func TestAContinuationRecordsTheRebindInItsOperation(t *testing.T) {
	alpha := definition("alpha")
	alpha.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"alpha"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{alpha})
	h.capability.secrets = []string{"bmc"}
	h.capability.access = map[string][]string{"alpha": {"bmc"}}
	h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	h.binder.versions = map[string]string{"bmc": "ver-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second")}
	seen := presentedTo(h)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an apply presenting a refused password completed")
	}
	h.binder.versions["bmc"] = "ver-2"
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, Authorizations: []string{RebindAuthorization + "bmc"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	operation, states := durableOperation(t, h)
	if operation.Verb != reconciliation.Apply || operation.State != reconciliation.OperationDone || states["alpha"] != reconciliation.BlockDone {
		t.Fatalf("the continued apply = %s %s with %v", operation.Verb, operation.State, states)
	}
	if !slices.Equal(*seen, []string{"first", "second"}) || !slices.Equal(operation.Bindings, []string{"bind-2"}) || h.binder.pins["bind-2"]["bmc"] != "ver-2" {
		t.Fatalf("the apply names %v pinned %v, want the rebound bind-2 at ver-2", operation.Bindings, h.binder.pins)
	}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the removal of the rebound apply refused: %v", err)
	}
}

// Only an access credential every block naming it presents is rebound. A
// Secret a block rests on as effect material, a Secret already presented at
// its current version, a plan that reopens no binding and a malformed token
// each refuse before anything is confirmed, rebound, bound or registered.
func TestTheRebindExitRefusesAnythingButARotatedAccessCredential(t *testing.T) {
	cases := map[string]struct {
		access         []string
		rotate         bool
		authorizations []string
		message        string
	}{
		"effect material": {
			access: nil, rotate: true, authorizations: []string{RebindAuthorization + "bmc"},
			message: "Secret bmc is not only an access credential this destroy presents: step 1 (remove alpha) rest on the version its operation bound",
		},
		"current already": {
			access: []string{"bmc"}, rotate: false, authorizations: []string{RebindAuthorization + "bmc"},
			message: "Secret bmc is already presented at its current version, so there is nothing to rebind",
		},
		"unbound": {
			access: []string{"bmc"}, rotate: true, authorizations: []string{RebindAuthorization + "other"},
			message: "pins no version of Secret other",
		},
		"malformed": {
			access: []string{"bmc"}, rotate: true, authorizations: []string{RebindAuthorization + "bmc", RebindAuthorization + "bmc"},
			message: "--authorize rebind=bmc must name one Secret once",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, _ := rotatedHarness(t, tc.access)
			if !tc.rotate {
				h.binder.versions["bmc"] = "ver-1"
			}
			before, _ := durableOperation(t, h)
			issued, executions := h.binder.issued, len(h.capability.executions)
			err := destroyWith(h, tc.authorizations...)
			reported := diagnostics.Of(err)
			if len(reported) == 0 || reported[0].Code != "lifecycle.authorization" || !strings.Contains(reported[0].Message, tc.message) {
				t.Fatalf("refusal = %+v (%v), want lifecycle.authorization containing %q", reported, err, tc.message)
			}
			after, _ := durableOperation(t, h)
			if h.binder.issued != issued || len(h.binder.rebinds) != 0 || len(h.capability.executions) != executions || after.ID != before.ID || h.confirmer.asked != 0 {
				t.Fatalf("the refusal issued %d bindings, rebound %v, ran %d executions, moved %s to %s or asked %d confirmations",
					h.binder.issued-issued, h.binder.rebinds, len(h.capability.executions)-executions, before.ID, after.ID, h.confirmer.asked)
			}
		})
	}
	t.Run("fresh apply", func(t *testing.T) {
		h := newHarness(t, "alpha")
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, Authorizations: []string{RebindAuthorization + "artifact-server-tls"}, SkipConfirmation: true})
		reported := diagnostics.Of(err)
		if len(reported) == 0 || reported[0].Code != "lifecycle.authorization" || !strings.Contains(reported[0].Message, "reopens no Secret binding") {
			t.Fatalf("refusal = %+v (%v)", reported, err)
		}
		if h.binder.issued != 0 {
			t.Fatalf("a refused fresh apply issued %d bindings", h.binder.issued)
		}
	})
}

// Only the capability's own word counts a block as using a Secret: a request
// string equal to the Secret's name in a member the capability does not report
// as a reference is no use of it.
func TestAnUnreportedRequestStringIsNoUseOfTheSecret(t *testing.T) {
	alpha := definition("alpha")
	alpha.Request = json.RawMessage(`{"credentialsRef":"other","name":"bmc"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{alpha})
	h.capability.secrets = []string{"bmc"}
	h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	h.binder.versions = map[string]string{"bmc": "ver-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second")}
	completeApply(t, h)
	h.binder.versions["bmc"] = "ver-2"
	err := destroyWith(h, RebindAuthorization+"bmc")
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != "lifecycle.authorization" || !strings.Contains(reported[0].Message, "no step of this destroy presents Secret bmc") {
		t.Fatalf("refusal = %+v (%v), want no step presenting Secret bmc", reported, err)
	}
	if len(h.binder.rebinds) != 0 {
		t.Fatalf("a refused rebind rebound %v", h.binder.rebinds)
	}
}

// secretBlind is a capability that implements the port and nothing optional.
type secretBlind struct{ Capability }

// A block whose capability cannot name the Secrets it references refuses the
// rebind, naming the step, before anything is rebound or confirmed.
func TestARebindRefusesABlockWhoseCapabilityCannotNameItsSecrets(t *testing.T) {
	h, _ := rotatedHarness(t, []string{"bmc"})
	h.service.capabilities = blindResolver{h.service.capabilities}
	err := destroyWith(h, RebindAuthorization+"bmc")
	reported := diagnostics.Of(err)
	want := "step 1 (remove alpha) has a capability that cannot name the Secrets it references, so no Secret is rebound"
	if len(reported) == 0 || reported[0].Code != "lifecycle.authorization" || !strings.Contains(reported[0].Message, want) {
		t.Fatalf("refusal = %+v (%v), want lifecycle.authorization containing %q", reported, err, want)
	}
	if len(h.binder.rebinds) != 0 || h.confirmer.asked != 0 {
		t.Fatalf("the refusal rebound %v and asked %d confirmations", h.binder.rebinds, h.confirmer.asked)
	}
}

type blindResolver struct{ CapabilityResolver }

func (r blindResolver) Resolve(kind, implementation string) (Capability, bool) {
	capability, found := r.CapabilityResolver.Resolve(kind, implementation)
	return secretBlind{capability}, found
}
