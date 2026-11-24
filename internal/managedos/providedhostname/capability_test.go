package providedhostname

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type runnerFunc func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)

func (f runnerFunc) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	return f(ctx, request)
}

func answering(t *testing.T, evidence Evidence, outcome reconciliation.Outcome) Capability {
	t.Helper()
	return New(runnerFunc(func(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
		evidence.Request = r.Digest
		data, err := json.Marshal(evidence)
		return lifecycle.RunResult{Outcome: outcome, Evidence: data}, err
	}))
}

func TestHostnameApplyCompletesOnlyWithItsProvedPostcondition(t *testing.T) {
	execution := hostnameExecution(t)
	for _, test := range []struct {
		name     string
		evidence Evidence
		want     reconciliation.Outcome
	}{
		{"already set", Evidence{Hostname: "witness.example.test", State: "complete", Postcondition: true, Version: EvidenceVersion}, reconciliation.OutcomeUnchanged},
		{"written", Evidence{Hostname: "witness.example.test", State: "complete", Postcondition: true, Changed: true, Version: EvidenceVersion}, reconciliation.OutcomeUnchanged},
		{"other hostname", Evidence{Hostname: "other.example.test", State: "complete", Postcondition: true, Version: EvidenceVersion}, reconciliation.OutcomeUnknown},
		{"no postcondition", Evidence{Hostname: "witness.example.test", State: "complete", Version: EvidenceVersion}, reconciliation.OutcomeUnknown},
		{"absent", Evidence{Hostname: "witness.example.test", State: "absent", Version: EvidenceVersion}, reconciliation.OutcomeUnknown},
		{"wrong version", Evidence{Hostname: "witness.example.test", State: "complete", Postcondition: true, Version: "other"}, reconciliation.OutcomeUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := answering(t, test.evidence, reconciliation.OutcomeUnchanged).Apply(t.Context(), execution)
			if result.Outcome != test.want || (test.want == reconciliation.OutcomeUnknown) != (err != nil) {
				t.Fatalf("apply = %+v, %v, want outcome %q", result, err, test.want)
			}
		})
	}
}

func TestHostnameObservationNamesItsEffect(t *testing.T) {
	execution := hostnameExecution(t)
	for _, test := range []struct {
		name     string
		evidence Evidence
		want     reconciliation.EffectState
	}{
		{"set", Evidence{Hostname: "witness.example.test", State: "complete", Postcondition: true, Version: EvidenceVersion}, reconciliation.EffectCompleted},
		{"differs", Evidence{Hostname: "witness.example.test", State: "absent", Version: EvidenceVersion}, reconciliation.EffectNoEffect},
		{"absent claiming a write", Evidence{Hostname: "witness.example.test", State: "absent", Changed: true, Version: EvidenceVersion}, reconciliation.EffectUnknown},
		{"unknown state", Evidence{Hostname: "witness.example.test", State: "partial", Version: EvidenceVersion}, reconciliation.EffectUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			observation, err := answering(t, test.evidence, reconciliation.OutcomeUnchanged).Observe(t.Context(), execution)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("observation = %+v, %v, want %q", observation, err, test.want)
			}
		})
	}
}

func TestHostnameRemovalRetainsTheHostWithoutRunningAnAdapter(t *testing.T) {
	execution := hostnameExecution(t)
	capability := New(runnerFunc(func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
		t.Fatal("removal reached the hostname adapter")
		return lifecycle.RunResult{}, nil
	}))
	result, err := capability.Destroy(t.Context(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("removal = %+v, %v", result, err)
	}
	var evidence Evidence
	if err := json.Unmarshal(result.Evidence, &evidence); err != nil || evidence.State != "retained" || !evidence.Postcondition || evidence.Hostname != "witness.example.test" {
		t.Fatalf("removal evidence = %+v, %v", evidence, err)
	}
}

func TestHostnameRefusalsNameTheMachineAndTheHostname(t *testing.T) {
	request, err := decodeBlock(hostnameExecution(t).Block)
	if err != nil {
		t.Fatal(err)
	}
	refused := refusals(request)
	if len(refused) != 2 {
		t.Fatalf("refusals = %v, want hostname-write and hostname-unproved", refused)
	}
	for reason, err := range refused {
		got := diagnostics.Of(err)
		if len(got) != 1 || got[0].Code != hostnameRefused.Code {
			t.Fatalf("%s refusal = %+v", reason, got)
		}
	}
	if got := diagnostics.Of(refused["hostname-write"]); got[0].Message != "Machine/witness: the host could not set its hostname to witness.example.test" {
		t.Fatalf("hostname-write message = %q", got[0].Message)
	}
}

func hostnameExecution(t *testing.T) lifecycle.Execution {
	t.Helper()
	id := Identity{Block: blockID("witness"), Context: "example", Object: "witness"}
	request := Request{Identity: id, Hostname: "witness.example.test", Version: Version,
		Placement: machine.Placement{Connection: machine.ConnectionSSH, Port: 22, Address: "192.0.2.234", User: "root",
			KnownHostsRef: "witness-host-key", PrivateKeyRef: "witness-admin", Machine: id.Object}}
	data, err := reconciliation.Freeze(request, "provided OS hostname test")
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{Kind: string(api.Machine), Object: id.Object, ID: id.Block,
		Implementation: Implementation, ContentDigest: ContentDigest(), Request: data}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{ApplyOperation: "op-0123456789abcdef0123456789abcdef",
		Block: reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}}
}

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	execution := hostnameExecution(t)
	capability := New(runnerFunc(func(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
		minimum := 2 * time.Minute
		if r.Operation == "observe" {
			minimum = 90 * time.Second
		}
		if r.Deadline < minimum || r.Deadline > lifecycle.MaxDeadline {
			t.Fatalf("%s deadline = %s, need between %s and %s", r.Operation, r.Deadline, minimum, lifecycle.MaxDeadline)
		}
		return lifecycle.RunResult{}, nil
	}))
	_, _ = capability.Apply(t.Context(), execution)
	_, _ = capability.Observe(t.Context(), execution)
}
