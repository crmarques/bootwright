package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestDestroyGraphCompletionRefusesRequiredSkippedWork(t *testing.T) {
	ledger := workflow.RunLedger{Tasks: []workflow.TaskLedgerEntry{
		{
			ID:            "destroy.machine-infra.demo",
			Kind:          workflow.DestroyTaskKindMachineInfra,
			Label:         "Machine infrastructure demo",
			ResourceKeys:  []string{"demo", workflow.DestroyMachineResourceKeyPrefix + "demo-0"},
			Status:        workflow.TaskStatusSkipped,
			SkippedReason: "no remote hosts matched task limit",
		},
		{
			ID:           "destroy.storage-clusters.data",
			Kind:         workflow.DestroyTaskKindStorageCluster,
			ResourceKeys: []string{"data"},
			Status:       workflow.TaskStatusOK,
		},
	}}
	outcome, err := destroyGraphCompletion(ledger, resolvedInvocation{
		verb:        invocationDestroy,
		contextName: "matrix",
		flags: invocationFlags{
			selection:    runSelection{stage: "infra", machines: "demo-0"},
			purgeHistory: true,
			yes:          true,
		},
	})
	if err == nil {
		t.Fatal("a selected destructive task skipped without completion proof must fail closed")
	}
	for _, want := range []string{"Machine infrastructure demo", "no remote hosts matched task limit", "records and substrate-release authorization were kept", "bootwright destroy --yes --stage infra --machines demo-0 --purge-history --ask-become-pass=false --context matrix"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("skip refusal must contain %q: %v", want, err)
		}
	}
	if outcome.Covers(workflow.DestroyTaskKindMachineInfra, "demo") {
		t.Fatal("a skipped machine teardown must not count as successful cleanup evidence")
	}
	if !outcome.Covers(workflow.DestroyTaskKindStorageCluster, "data") {
		t.Fatal("independent successful teardown must retain its own cleanup evidence")
	}
}

func TestDestroyGraphCompletionAllowsEmptyNoOpSkipWithoutClaimingSuccess(t *testing.T) {
	ledger := workflow.RunLedger{Tasks: []workflow.TaskLedgerEntry{{
		ID:            "destroy.provider-services",
		Kind:          workflow.DestroyTaskKindProviderServices,
		Label:         "Provider services",
		Status:        workflow.TaskStatusSkipped,
		SkippedReason: "no remote hosts matched task limit",
	}}}
	outcome, err := destroyGraphCompletion(ledger, resolvedInvocation{verb: invocationDestroy})
	if err != nil {
		t.Fatalf("a task with no selected resource is an empty no-op, not an incomplete destructive selection: %v", err)
	}
	if outcome.Covers(workflow.DestroyTaskKindProviderServices, "") {
		t.Fatal("an empty no-op skip still must not claim successful teardown")
	}
}
