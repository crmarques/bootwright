package providedupdate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type runnerFunc func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)

func (f runnerFunc) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	return f(ctx, request)
}

func TestUpdateDoesNotCompleteWithoutProvedReboot(t *testing.T) {
	execution := updateExecution(t)
	for _, test := range []struct {
		name     string
		changed  int
		rebooted bool
		want     reconciliation.Outcome
	}{
		{"current", 0, false, reconciliation.OutcomeUnchanged},
		{"package change and reboot", 3, true, reconciliation.OutcomeUnchanged},
		{"package change without reboot", 3, false, reconciliation.OutcomeUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			apply := New(runnerFunc(func(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
				evidence, err := json.Marshal(Evidence{Packages: test.changed, Postcondition: true, Rebooted: test.rebooted,
					Release: "9.8", Request: r.Digest, State: "complete", Version: EvidenceVersion})
				return lifecycle.RunResult{Outcome: reconciliation.OutcomeUnchanged, Evidence: evidence}, err
			}))
			result, _ := apply.Apply(t.Context(), execution)
			if result.Outcome != test.want {
				t.Fatalf("apply outcome = %q, want %q", result.Outcome, test.want)
			}
		})
	}
}

func TestUpdateRemovalRetainsTheHostWithoutRunningAnAdapter(t *testing.T) {
	execution := updateExecution(t)
	capability := New(runnerFunc(func(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
		t.Fatal("removal reached the update adapter")
		return lifecycle.RunResult{}, nil
	}))
	result, err := capability.Destroy(t.Context(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeUnchanged {
		t.Fatalf("removal = %+v, %v", result, err)
	}
	var evidence Evidence
	if err := json.Unmarshal(result.Evidence, &evidence); err != nil || evidence.State != "retained" || !evidence.Postcondition {
		t.Fatalf("removal evidence = %+v, %v", evidence, err)
	}
}

func updateExecution(t *testing.T) lifecycle.Execution {
	t.Helper()
	id := Identity{Block: blockID("witness"), Context: "example", Object: "witness"}
	request := Request{Identity: id, Policy: PolicyWithinRelease, Release: "9.8", Version: Version,
		Placement: machine.Placement{Connection: machine.ConnectionSSH, Port: 22, Address: "192.0.2.234", User: "root",
			KnownHostsRef: "witness-host-key", PrivateKeyRef: "witness-admin", Machine: id.Object}}
	data, err := reconciliation.Freeze(request, "provided OS update test")
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
	execution := updateExecution(t)
	capability := New(runnerFunc(func(_ context.Context, r lifecycle.RunRequest) (lifecycle.RunResult, error) {
		// The role gives the update 30 minutes, Ansible reboot up to two
		// 5-minute waits and final verification 90 seconds.
		minimum := 40*time.Minute + 90*time.Second
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
