package providedregistration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type runnerFunc func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)

func (f runnerFunc) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	return f(ctx, request)
}

func answering(state, consumer string, postcondition bool, outcome reconciliation.Outcome) Capability {
	return New(runnerFunc(func(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
		evidence, err := json.Marshal(Evidence{Consumer: consumer, Postcondition: postcondition, Request: r.Digest, State: state, Version: EvidenceVersion})
		return lifecycle.RunResult{Outcome: outcome, Evidence: evidence}, err
	}))
}

const consumer = "11111111-2222-3333-4444-555555555555"

func TestRegistrationApplyCompletesOnlyOnProvedConvergence(t *testing.T) {
	execution := registrationExecution(t)
	for _, test := range []struct {
		name          string
		state         string
		consumer      string
		postcondition bool
		want          reconciliation.Outcome
	}{
		{"converged", "converged", consumer, true, reconciliation.OutcomeChanged},
		{"unchanged", "unchanged", consumer, true, reconciliation.OutcomeUnchanged},
		{"converged without its postcondition", "converged", consumer, false, reconciliation.OutcomeUnknown},
		{"diverged", "diverged", consumer, false, reconciliation.OutcomeUnknown},
		{"absent", "absent", "", false, reconciliation.OutcomeUnknown},
		{"another state", "registered", consumer, true, reconciliation.OutcomeUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := answering(test.state, test.consumer, test.postcondition, test.want).Apply(t.Context(), execution)
			if test.want == reconciliation.OutcomeUnknown {
				if err == nil || result.Outcome != reconciliation.OutcomeUnknown {
					t.Fatalf("apply = %+v, %v, want an unproved registration", result, err)
				}
				return
			}
			if err != nil || result.Outcome != test.want {
				t.Fatalf("apply = %+v, %v, want %q", result, err, test.want)
			}
		})
	}
}

func TestRegistrationObservationClassifiesTheHost(t *testing.T) {
	execution := registrationExecution(t)
	for _, test := range []struct {
		state         string
		consumer      string
		postcondition bool
		want          reconciliation.EffectState
	}{
		{"converged", consumer, true, reconciliation.EffectCompleted},
		{"diverged", consumer, false, reconciliation.EffectPartial},
		{"absent", "", false, reconciliation.EffectNoEffect},
		{"converged", consumer, false, reconciliation.EffectPartial},
		{"converged", "", true, reconciliation.EffectUnknown},
		{"diverged", "", false, reconciliation.EffectUnknown},
		{"absent", consumer, false, reconciliation.EffectUnknown},
		{"surprise", consumer, true, reconciliation.EffectUnknown},
	} {
		t.Run(test.state+" "+test.consumer, func(t *testing.T) {
			observation, err := answering(test.state, test.consumer, test.postcondition, reconciliation.OutcomeUnchanged).Observe(t.Context(), execution)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("observation = %+v, %v, want %q", observation, err, test.want)
			}
		})
	}
	foreign := New(runnerFunc(func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
		evidence, err := json.Marshal(Evidence{Consumer: consumer, Postcondition: true, Request: strings.Repeat("0", 64), State: "converged", Version: EvidenceVersion})
		return lifecycle.RunResult{Evidence: evidence}, err
	}))
	if observation, err := foreign.Observe(t.Context(), execution); err != nil || observation.Effect != reconciliation.EffectUnknown {
		t.Fatalf("another request's evidence = %+v, %v", observation, err)
	}
}

func TestRegistrationRunLendsTheCertificatePartAndNoOtherSecret(t *testing.T) {
	execution := registrationExecution(t)
	seen := false
	capability := New(runnerFunc(func(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
		seen = true
		var bundles []lifecycle.MaterialFile
		for _, file := range r.Materials {
			if file.Variable == "satelliteCA" {
				bundles = append(bundles, file)
			}
		}
		if len(bundles) != 1 || bundles[0].Secret != "satellite-ca" || bundles[0].Part != secrets.CertificatePart ||
			r.Variable != "bootwright_provided_registration" || r.Implementation != Implementation || len(r.Materials) != 3 {
			t.Fatalf("%s materials = %+v", r.Operation, r.Materials)
		}
		return lifecycle.RunResult{}, nil
	}))
	_, _ = capability.Apply(t.Context(), execution)
	_, _ = capability.Observe(t.Context(), execution)
	if !seen {
		t.Fatal("the adapter never ran")
	}
}

func TestRegistrationRefusalsNameTheMachineTheSatelliteAndTheBundle(t *testing.T) {
	request := registrationRequest()
	table := refusals(request)
	for _, reason := range []string{"registration-absent", "registration-foreign", "registration-trust", "registration-unavailable", "registration-content-root"} {
		got := diagnostics.Of(table[reason])
		if len(got) != 1 || got[0].Code != registrationRefused.Code || !strings.Contains(got[0].Message+got[0].Remediation, "Machine/witness") ||
			!strings.Contains(got[0].Message+got[0].Remediation, "satellite.example.test") || !strings.HasSuffix(got[0].Remediation, "and repeat apply") {
			t.Errorf("%s = %+v", reason, got)
		}
	}
	if got := diagnostics.Of(table["registration-trust"]); !strings.Contains(got[0].Message, "Secret/satellite-ca") || !strings.Contains(got[0].Remediation, "Secret/satellite-ca") {
		t.Errorf("trust refusal = %+v", got)
	}
	if got := diagnostics.Of(table["registration-foreign"]); !strings.Contains(got[0].Remediation, "spec.os.provided.subscription.entitlementRef") {
		t.Errorf("foreign refusal = %+v", got)
	}
	if got := diagnostics.Of(table["registration-content-root"]); !strings.Contains(got[0].Message, "https://satellite.example.test/pulp/content") {
		t.Errorf("content root refusal = %+v", got)
	}
}

func TestRegistrationRemovalRetainsTheHostWithoutRunningAnAdapter(t *testing.T) {
	execution := registrationExecution(t)
	capability := New(runnerFunc(func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
		t.Fatal("removal reached the registration adapter")
		return lifecycle.RunResult{}, nil
	}))
	result, err := capability.Destroy(t.Context(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("removal = %+v, %v", result, err)
	}
	var evidence Evidence
	if err := json.Unmarshal(result.Evidence, &evidence); err != nil || evidence.State != "retained" || !evidence.Postcondition || evidence.Request != execution.Block.RequestDigest {
		t.Fatalf("removal evidence = %+v, %v", evidence, err)
	}
	removal, err := capability.Removal(t.Context(), execution.Block)
	if err != nil || removal.Description != "retain the Satellite registration of witness" {
		t.Fatalf("removal = %+v, %v", removal, err)
	}
	if quiet, err := capability.Quiescent(t.Context(), lifecycle.Probe{}); err != nil || quiet.State != lifecycle.Quiescent {
		t.Fatalf("quiescence = %+v, %v", quiet, err)
	}
}

func TestRegistrationUnresolvedNamesTheRegistration(t *testing.T) {
	execution := registrationExecution(t)
	got, ok := Capability{}.Unresolved(reconciliation.Apply, execution.Block, json.RawMessage(`{}`))
	if !ok || !strings.Contains(got.Reason, "the Satellite registration of Machine witness") {
		t.Fatalf("unresolved = %+v, %v", got, ok)
	}
	if _, ok := (Capability{}).Unresolved(reconciliation.Destroy, execution.Block, json.RawMessage(`{}`)); ok {
		t.Fatal("a destroy was reported unresolved")
	}
}

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	execution := registrationExecution(t)
	capability := New(runnerFunc(func(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
		minimum := 4 * time.Minute
		if r.Operation == "observe" {
			minimum = 90 * time.Second
		}
		if r.Deadline <= minimum || r.Deadline > lifecycle.MaxDeadline {
			t.Fatalf("%s deadline = %s, need between %s and %s", r.Operation, r.Deadline, minimum, lifecycle.MaxDeadline)
		}
		return lifecycle.RunResult{}, nil
	}))
	_, _ = capability.Apply(t.Context(), execution)
	_, _ = capability.Observe(t.Context(), execution)
}

func registrationRequest() Request {
	id := Identity{Block: blockID("witness"), Context: "example", Object: "witness"}
	return Request{EntitlementRef: "rhel", Identity: id, Version: Version,
		Satellite: Satellite{ContentBaseURL: "https://satellite.example.test/pulp/content", Hostname: "satellite.example.test", TrustBundleRef: "satellite-ca"},
		Placement: machine.Placement{Connection: machine.ConnectionSSH, Port: 22, Address: "192.0.2.234", User: "root",
			KnownHostsRef: "witness-host-key", PrivateKeyRef: "witness-admin", Machine: id.Object}}
}

func registrationExecution(t *testing.T) lifecycle.Execution {
	t.Helper()
	request := registrationRequest()
	data, err := reconciliation.Freeze(request, "provided registration test")
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{Kind: string(api.Machine), Object: request.Identity.Object, ID: request.Identity.Block,
		Implementation: Implementation, ContentDigest: ContentDigest(), Request: data}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.Execution{ApplyOperation: "op-0123456789abcdef0123456789abcdef",
		Block: reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}}
}
