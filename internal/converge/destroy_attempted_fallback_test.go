package converge

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestDestroyKindIncludedOnlyUsesMachineInfraFallbackForUnattemptedWork(t *testing.T) {
	for _, kind := range []string{
		workflow.DestroyTaskKindContainerCluster,
		workflow.DestroyTaskKindStorageNodeAccess,
	} {
		t.Run(kind, func(t *testing.T) {
			failed := workflow.SucceededDestroyTaskKinds(workflow.RunLedger{Tasks: []workflow.TaskLedgerEntry{
				{Kind: kind, ResourceKeys: []string{"cluster-a"}, Status: workflow.TaskStatusFailed},
				{Kind: workflow.DestroyTaskKindMachineInfra, ResourceKeys: []string{"cluster-a"}, Status: workflow.TaskStatusOK},
			}})
			if destroyKindIncluded(failed)(kind, "cluster-a") {
				t.Fatal("attempted failed teardown cannot be covered by machine-infra fallback")
			}

			direct := workflow.SucceededDestroyTaskKinds(workflow.RunLedger{Tasks: []workflow.TaskLedgerEntry{
				{Kind: kind, ResourceKeys: []string{"cluster-a"}, Status: workflow.TaskStatusOK},
			}})
			if !destroyKindIncluded(direct)(kind, "cluster-a") {
				t.Fatal("direct successful teardown must be included")
			}

			unattempted := workflow.SucceededDestroyTaskKinds(workflow.RunLedger{Tasks: []workflow.TaskLedgerEntry{
				{Kind: workflow.DestroyTaskKindMachineInfra, ResourceKeys: []string{"cluster-a"}, Status: workflow.TaskStatusOK},
			}})
			if !destroyKindIncluded(unattempted)(kind, "cluster-a") {
				t.Fatal("unattempted teardown may use successful machine-infra fallback")
			}
		})
	}
}
