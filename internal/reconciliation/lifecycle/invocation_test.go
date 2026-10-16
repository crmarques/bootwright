package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// A capability states its run's deadline only through its invocation, and the
// request carries exactly that deadline, or none when the invocation states
// none, so the runner keeps its default.
func TestRunForCarriesTheInvocationsDeadline(t *testing.T) {
	for _, deadline := range []time.Duration{0, 3*time.Hour + 5*time.Minute} {
		if got := RunFor(Execution{}, Invocation{Deadline: deadline}).Deadline; got != deadline {
			t.Fatalf("an invocation stating %s produced a request stating %s", deadline, got)
		}
	}
}

// An attempt keeps its adapter's output beside its own log, so a failure that
// output explains names the block and that output's file in the operation's
// Logs directory, which only root reads, whatever the capability asked for. An
// execution that names no attempt of a block, as a quiescence probe does,
// points beside the attempt's log.
func TestAFailedAttemptNamesItsBlockAndOutputFile(t *testing.T) {
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "os-install-rhel-01"}}
	const retained = "read the adapter output retained beside this attempt's log"
	for _, test := range []struct {
		execution Execution
		want      string
	}{
		{Execution{Operation: "op-x", Attempt: 2, Block: block},
			"read blocks/os-install-rhel-01/attempt-000002.output in this operation's Logs directory, as root, for why block os-install-rhel-01 failed"},
		{Execution{Operation: "op-x", Attempt: 2, Resolution: 1, Block: block},
			"read blocks/os-install-rhel-01/attempt-000002-resolution-000001.output in this operation's Logs directory, as root, for why block os-install-rhel-01 failed"},
		{Execution{Operation: "op-x", Block: block}, retained},
		{Execution{}, retained},
	} {
		if got := RunFor(test.execution, Invocation{}).OutputRemediation; got != test.want {
			t.Fatalf("attempt %d of %q points an adapter failure at %q, want %q", test.execution.Attempt, test.execution.Block.ID, got, test.want)
		}
	}
}

// The refusals a capability remedies by name reach the runner as its own,
// each with the diagnostic it gave, and a later change to the capability's map
// changes nothing the run already holds.
func TestRunForCarriesTheRefusalsTheCapabilityRemedies(t *testing.T) {
	running := errors.New("Machine/rhel-01 is running")
	refusals := map[string]error{"machine-running": running}
	request := RunFor(Execution{}, Invocation{Refusals: refusals})
	refusals["identity-mismatch"] = errors.New("added later")
	if len(request.Refusals) != 1 || request.Refusals["machine-running"] != running {
		t.Fatalf("the run names the refusals %v", request.Refusals)
	}
	if got := RunFor(Execution{}, Invocation{}).Refusals; got != nil {
		t.Fatalf("an invocation naming no refusal produced %v", got)
	}
}

// An operation binds whole versions, but a run is lent only the parts its
// material files name: a consumer that verifies a listener against a serving
// certificate is lent that certificate and never the key beside it, a Secret
// no file names is not lent at all, and the placement's own files are lent
// like the capability's. The lent parts are the operation's own bytes, so
// clearing the binding's material clears them and no copy is left behind.
func TestRunForLendsOnlyThePartsItsMaterialFilesName(t *testing.T) {
	bound := map[string]secrets.Material{
		"serving": secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.CertificatePart: []byte("CERTIFICATE"), secrets.PrivateKeyPart: []byte("SERVING KEY"),
		}),
		"fleet": secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.PrivateKeyPart: []byte("FLEET KEY"), secrets.PublicKeyPart: []byte("FLEET PUBLIC"),
		}),
		"unread": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("UNREAD")}),
	}
	request := RunFor(Execution{Material: bound}, Invocation{
		Materials: []MaterialFile{{Name: "ca", Part: secrets.CertificatePart, Secret: "serving", Variable: "ca"}},
		Placement: machineref.Placement{Connection: machineref.ConnectionSSH, Machine: "remote", PrivateKeyRef: "fleet"},
	})
	lent := map[string][]secrets.Part{}
	for name, material := range request.Material {
		lent[name] = material.Parts()
	}
	want := map[string][]secrets.Part{"serving": {secrets.CertificatePart}, "fleet": {secrets.PrivateKeyPart}}
	if !reflect.DeepEqual(lent, want) {
		t.Fatalf("the run is lent %v, want %v", lent, want)
	}
	if value, _ := request.Material["serving"].Part(secrets.CertificatePart); string(value) != "CERTIFICATE" {
		t.Fatalf("the lent certificate reads %q", value)
	}
	bound["serving"].Clear()
	if value, _ := request.Material["serving"].Part(secrets.CertificatePart); string(value) == "CERTIFICATE" {
		t.Fatal("the lent certificate is a copy that clearing the binding's material does not reach")
	}
}

// RunFor marks every request an attempt does not name as its own effect
// read-only, and every request no operation record names unrecorded, from the
// execution's attempt and resolution numbers alone, so no call site can
// forget either mark.
func TestRunForDerivesReadOnlyAndUnrecorded(t *testing.T) {
	for _, test := range []struct {
		name                 string
		execution            Execution
		readOnly, unrecorded bool
	}{
		{"a probe", Probe{}.Execution(), true, true},
		{"a resolution", Execution{Attempt: 2, Resolution: 1}, true, false},
		{"an attempt", Execution{Attempt: 1}, false, false},
		{"an attempt marked read-only", Execution{Attempt: 1, ReadOnly: true}, true, false},
	} {
		request := RunFor(test.execution, Invocation{Operation: "apply"})
		if request.ReadOnly != test.readOnly || request.Unrecorded != test.unrecorded {
			t.Fatalf("%s is read-only %t and unrecorded %t, want %t and %t", test.name, request.ReadOnly, request.Unrecorded, test.readOnly, test.unrecorded)
		}
	}
}

// keepingCapability is the journey's capability with a copy its removal
// would delete, recording every execution a keep is handed.
type keepingCapability struct {
	*testCapability
	kept []Execution
}

func (c *keepingCapability) Keeps(block reconciliation.Block) (KeptEntry, bool, error) {
	return KeptEntry{Block: block.ID, Name: "kubeconfig"}, true, nil
}

func (c *keepingCapability) Keep(_ context.Context, execution Execution) ([]Produced, error) {
	c.kept = append(c.kept, execution)
	return nil, nil
}

// Over an apply and a destroy that each leave their block unknown and are
// resolved, every attempt the engine makes reaches the runner able to change
// its target, and every resolution, quiescence probe and keep reaches it
// read-only.
func TestEveryExecutionDeclaresItsReadOnlyness(t *testing.T) {
	h := newHarness(t, "install")
	capability := &keepingCapability{testCapability: h.capability}
	h.service.capabilities = testResolver{capability: capability}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := apply(h); err == nil {
		t.Fatal("an unknown apply completed")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := destroy(h); err == nil {
		t.Fatal("an unknown removal completed")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	if _, err := destroy(h); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(h.capability.calls, "observe:install") || !slices.Contains(h.capability.calls, "observe-removal:install") ||
		len(h.capability.applies) == 0 || len(h.capability.destroys) == 0 || len(h.capability.probed) == 0 || len(capability.kept) == 0 {
		t.Fatalf("the journey made %v, destroyed %v, probed %d times and kept %d times", h.capability.calls, h.capability.destroys, len(h.capability.probed), len(capability.kept))
	}
	for _, execution := range h.capability.executions {
		if got, want := RunFor(execution, Invocation{}).ReadOnly, execution.Resolution != 0; got != want {
			t.Fatalf("attempt %d resolution %d is read-only %t, want %t", execution.Attempt, execution.Resolution, got, want)
		}
	}
	for _, execution := range append(slices.Clone(h.capability.probed), capability.kept...) {
		if !RunFor(execution, Invocation{}).ReadOnly {
			t.Fatalf("a probe or keep at attempt %d resolution %d is not read-only", execution.Attempt, execution.Resolution)
		}
	}
}

func TestNativeErrorAdmissionRequiresTheInvocationsExplicitOptIn(t *testing.T) {
	if RunFor(Execution{}, Invocation{}).NativeErrors {
		t.Fatal("native prose enabled by default")
	}
	if !RunFor(Execution{}, Invocation{NativeErrors: true}).NativeErrors {
		t.Fatal("explicit native diagnostic contract was lost")
	}
}
