package lifecycle

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestContinuationUsesSavedPlanWithoutCallingPlanner(t *testing.T) {
	t.Parallel()
	for _, change := range []struct {
		name  string
		apply func(*harness)
	}{
		{"default", func(h *harness) {
			h.capability.definitions[0].Request = json.RawMessage(`{"name":"new-default","port":10443}`)
		}},
		{"extra block", func(h *harness) { h.capability.definitions = append(h.capability.definitions, definition("extra")) }},
		{"planner refusal", func(h *harness) { h.capability.planErr = failure("lifecycle.state", "planner refused", "") }},
	} {
		t.Run(change.name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			failedApply(t, h)
			operation, _ := durableOperation(t, h)
			frozen, err := recordsOf(h).ReadPlan(context.Background(), operation.ID)
			if err != nil {
				t.Fatal(err)
			}
			change.apply(h)
			h.service.options.Executable.Commit = "1234567"
			h.capability.planned = func() { t.Error("continuation called planner") }
			result, err := apply(h)
			if err != nil || result.Receipt.Operation != operation.ID {
				t.Fatalf("continuation = %+v (%v)", result, err)
			}
			retained, err := recordsOf(h).ReadPlan(context.Background(), operation.ID)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := frozen.Digest()
			after, _ := retained.Digest()
			if before != after || len(h.capability.applies) != 2 {
				t.Fatalf("plan or executed blocks changed: %s/%s %v", before, after, h.capability.applies)
			}
		})
	}
}

func TestChangedExecutionFingerprintRefusesBeforeEffects(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "artifact-server-lab")
	applyLeftUnknown(t, h)
	current := h.service.options.Contracts
	h.service.options.Contracts = func(block reconciliation.Block) (ExecutionContract, bool) {
		contract, ok := current(block)
		contract.Fingerprint = strings.Repeat("8", 64)
		return contract, ok
	}
	h.service.options.Executable.Commit = "1234567"
	refusedUnchanged(t, h, reconciliation.Apply,
		"this executable does not support the recorded execution contract of block artifact-server-lab",
		"install bootwright devel (abcdef1), which registered this operation, and run bootwright install")
}

func TestChangedPredecessorContractRefusesEvidenceUse(t *testing.T) {
	t.Parallel()
	h, _ := producerAndConsumer(t)
	f := newLineageFixture(t, h, consumerDefinition())
	current := h.service.options.Contracts
	h.service.options.Contracts = func(block reconciliation.Block) (ExecutionContract, bool) {
		contract, ok := current(block)
		if block.ID == "producer-a" {
			contract.Fingerprint = strings.Repeat("8", 64)
		}
		return contract, ok
	}
	before := untouchedOf(h)
	if evidence, err := h.service.applyDependencies(context.Background(), f.store, f.head.ID, "consumer"); err == nil || len(evidence) != 0 {
		t.Fatal("unsupported producer evidence was lent to a consumer")
	}
	before.require(t, h)
}

func TestObservedDiffRefusesUnsupportedEvidenceBeforeObservation(t *testing.T) {
	t.Parallel()
	f, observer, _ := observedFixture(t, "artifact-server-lab")
	current := f.service.options.Contracts
	f.service.options.Contracts = func(block reconciliation.Block) (ExecutionContract, bool) {
		contract, ok := current(block)
		contract.Fingerprint = strings.Repeat("8", 64)
		return contract, ok
	}
	before := len(observer.observes)
	if _, err := f.service.Diff(context.Background(), DiffRequest{ContextName: testContextName, Observe: true}); err == nil {
		t.Fatal("diff interpreted unsupported evidence")
	}
	if len(observer.observes) != before {
		t.Fatal("diff launched an observer before proving its evidence contract")
	}
}
