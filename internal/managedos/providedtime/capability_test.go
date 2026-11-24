package providedtime

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
)

var servers = []string{"192.0.2.123", "ntp.example.test"}

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

func TestTimeApplyCompletesOnlyWithItsProvedPostcondition(t *testing.T) {
	execution := timeExecution(t)
	for _, test := range []struct {
		name     string
		evidence Evidence
		want     reconciliation.Outcome
	}{
		{"already synchronized", Evidence{Servers: servers, State: "complete", Postcondition: true, Version: EvidenceVersion}, reconciliation.OutcomeUnchanged},
		{"converged", Evidence{Servers: servers, State: "complete", Postcondition: true, Changed: true, Version: EvidenceVersion}, reconciliation.OutcomeUnchanged},
		{"other servers", Evidence{Servers: []string{"192.0.2.9"}, State: "complete", Postcondition: true, Version: EvidenceVersion}, reconciliation.OutcomeUnknown},
		{"no postcondition", Evidence{Servers: servers, State: "complete", Version: EvidenceVersion}, reconciliation.OutcomeUnknown},
		{"partial", Evidence{Servers: servers, State: "partial", Version: EvidenceVersion}, reconciliation.OutcomeUnknown},
		{"wrong version", Evidence{Servers: servers, State: "complete", Postcondition: true, Version: "other"}, reconciliation.OutcomeUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := answering(t, test.evidence, reconciliation.OutcomeUnchanged).Apply(t.Context(), execution)
			if result.Outcome != test.want || (test.want == reconciliation.OutcomeUnknown) != (err != nil) {
				t.Fatalf("apply = %+v, %v, want outcome %q", result, err, test.want)
			}
		})
	}
}

func TestTimeObservationNamesItsEffect(t *testing.T) {
	execution := timeExecution(t)
	for _, test := range []struct {
		name     string
		evidence Evidence
		want     reconciliation.EffectState
	}{
		{"synchronized", Evidence{Servers: servers, State: "complete", Postcondition: true, Version: EvidenceVersion}, reconciliation.EffectCompleted},
		{"configured but not proved", Evidence{Servers: servers, State: "partial", Version: EvidenceVersion}, reconciliation.EffectPartial},
		{"another configuration", Evidence{Servers: servers, State: "absent", Version: EvidenceVersion}, reconciliation.EffectNoEffect},
		{"absent claiming a change", Evidence{Servers: servers, State: "absent", Changed: true, Version: EvidenceVersion}, reconciliation.EffectUnknown},
		{"complete without its proof", Evidence{Servers: servers, State: "complete", Version: EvidenceVersion}, reconciliation.EffectPartial},
		{"partial claiming its proof", Evidence{Servers: servers, State: "partial", Postcondition: true, Version: EvidenceVersion}, reconciliation.EffectUnknown},
		{"unknown state", Evidence{Servers: servers, State: "surprise", Version: EvidenceVersion}, reconciliation.EffectUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			observation, err := answering(t, test.evidence, reconciliation.OutcomeUnchanged).Observe(t.Context(), execution)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("observation = %+v, %v, want %q", observation, err, test.want)
			}
		})
	}
}

func TestTimeRemovalRetainsTheHostWithoutRunningAnAdapter(t *testing.T) {
	capability := New(runnerFunc(func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
		t.Fatal("removal reached the time adapter")
		return lifecycle.RunResult{}, nil
	}))
	result, err := capability.Destroy(t.Context(), timeExecution(t))
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("removal = %+v, %v", result, err)
	}
	var evidence Evidence
	if err := json.Unmarshal(result.Evidence, &evidence); err != nil || evidence.State != "retained" || !evidence.Postcondition || strings.Join(evidence.Servers, ",") != strings.Join(servers, ",") {
		t.Fatalf("removal evidence = %+v, %v", evidence, err)
	}
}

func TestTimeRefusalsNameTheMachineTheServersAndTheRemedy(t *testing.T) {
	request, err := decodeBlock(timeExecution(t).Block)
	if err != nil {
		t.Fatal(err)
	}
	refused := refusals(request)
	if len(refused) != 5 {
		t.Fatalf("refusals = %v, want the five tokens", refused)
	}
	for reason, err := range refused {
		got := diagnostics.Of(err)
		if len(got) != 1 || got[0].Code != timeRefused.Code || !strings.Contains(got[0].Message, "Machine/witness") || got[0].Remediation == "" {
			t.Fatalf("%s refusal = %+v", reason, got)
		}
	}
	unproved := diagnostics.Of(refused["time-unproved"])[0]
	for _, want := range []string{"192.0.2.123, ntp.example.test", "180 seconds", "chronyc -n sources", ": unknown"} {
		if !strings.Contains(unproved.Message, want) {
			t.Fatalf("time-unproved message %q lacks %q", unproved.Message, want)
		}
	}
	for _, want := range []string{"UDP port 123", "spec.os.provided.ntp", "keeps the configuration it had"} {
		if !strings.Contains(unproved.Remediation, want) {
			t.Fatalf("time-unproved remedy %q lacks %q", unproved.Remediation, want)
		}
	}
}

func TestTimeRefusalsRenderOnlyTheAdapterDetailsTheyRecognize(t *testing.T) {
	request, err := decodeBlock(timeExecution(t).Block)
	if err != nil {
		t.Fatal(err)
	}
	refused := refusals(request)
	render := func(reason, detail string) diagnostics.Diagnostic {
		completed, ok := refused[reason].(interface{ WithDetail(string) error })
		if !ok {
			t.Fatalf("%s does not take a detail", reason)
		}
		return diagnostics.Of(completed.WithDetail(detail))[0]
	}
	conflict := render("time-service-conflict", "systemd-timesyncd.service")
	if !strings.Contains(conflict.Message, "systemd-timesyncd.service") || !strings.Contains(conflict.Remediation, "systemctl disable --now systemd-timesyncd.service") {
		t.Fatalf("conflict = %+v", conflict)
	}
	if other := render("time-service-conflict", "arbitrary text"); strings.Contains(other.Message, "arbitrary") || !strings.Contains(other.Message, "unknown") {
		t.Fatalf("unrecognized unit = %+v", other)
	}
	sources := "192.0.2.123@sU0_192.0.2.124@sS377"
	if got := render("time-unproved", sources); !strings.HasSuffix(got.Message, ": "+sources) {
		t.Fatalf("sources = %+v", got)
	}
	for _, detail := range []string{"none", "unavailable"} {
		if got := render("time-unproved", detail); !strings.HasSuffix(got.Message, ": "+detail) {
			t.Fatalf("%s = %+v", detail, got)
		}
	}
	if got := render("time-unproved", "injected text with spaces"); !strings.HasSuffix(got.Message, ": unknown") {
		t.Fatalf("unrecognized sources = %+v", got)
	}
}

func TestTimeUnresolvedApplyIsDriftNamingTheServers(t *testing.T) {
	execution := timeExecution(t)
	if _, ok := (Capability{}).Unresolved(reconciliation.Destroy, execution.Block, json.RawMessage(`{}`)); ok {
		t.Fatal("a destroy was explained as drift")
	}
	if _, ok := (Capability{}).Unresolved(reconciliation.Apply, execution.Block, nil); ok {
		t.Fatal("a missing record was explained as drift")
	}
	if _, ok := (Capability{}).Unresolved(reconciliation.Apply, execution.Block, json.RawMessage(`{"servers":[]}`)); !ok {
		t.Fatal("an unproved apply was not explained")
	}
}

func timeExecution(t *testing.T) lifecycle.Execution {
	t.Helper()
	id := Identity{Block: blockID("witness"), Context: "example", Object: "witness"}
	request := Request{Identity: id, Servers: servers, Version: Version,
		Placement: machine.Placement{Connection: machine.ConnectionSSH, Port: 22, Address: "192.0.2.234", User: "root",
			KnownHostsRef: "witness-host-key", PrivateKeyRef: "witness-admin", Machine: id.Object}}
	data, err := reconciliation.Freeze(request, "provided time test")
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
	execution := timeExecution(t)
	capability := New(runnerFunc(func(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
		minimum := 10 * time.Minute
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
