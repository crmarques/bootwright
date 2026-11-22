package diskdiscovery

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type preparedOperations struct {
	operation *lifecycle.OperationResult
	err       error
	prepared  []lifecycle.PrepareRequest
	read      []string
	records   []lifecycle.BlockEvidence
}

func (o *preparedOperations) Prepare(_ context.Context, request lifecycle.PrepareRequest) (*lifecycle.OperationResult, error) {
	o.prepared = append(o.prepared, request)
	return o.operation, o.err
}

func (o *preparedOperations) EvidenceForObjects(_ context.Context, _, _ string, objects []string) (map[string][]lifecycle.BlockEvidence, error) {
	o.read = append(o.read, objects...)
	records := make(map[string][]lifecycle.BlockEvidence, len(objects))
	for _, record := range o.records {
		records[record.Object] = append(records[record.Object], record)
	}
	return records, nil
}

func TestInventoryReadsCurrentRecordsWithoutPreparing(t *testing.T) {
	t.Parallel()
	operations := &preparedOperations{records: []lifecycle.BlockEvidence{{
		Object: "node-a", Implementation: DiscoveryImplementation, Verb: reconciliation.Destroy, State: reconciliation.BlockDone,
	}}}
	result, err := NewDiscoveryService(operations).Inventory(context.Background(), InventoryRequest{ContextName: "lab", Names: []string{"node-a", "node-b"}})
	if err != nil || result == nil || len(result.Inventories) != 2 {
		t.Fatalf("inventory = %+v, err = %v", result, err)
	}
	for _, inventory := range result.Inventories {
		if inventory.Source != "" || len(inventory.Disks) != 0 {
			t.Fatalf("reported removed inventory: %+v", inventory)
		}
	}
	if len(operations.prepared) != 0 || len(operations.read) != 2 {
		t.Fatalf("inspection prepared or failed to read: %v %v", operations.prepared, operations.read)
	}
}

func (o *preparedOperations) PlanPreparation(_ context.Context, request lifecycle.PrepareRequest) (*lifecycle.PlanResult, error) {
	o.prepared = append(o.prepared, request)
	return &lifecycle.PlanResult{}, nil
}

func TestDiscoveryReadsInventoriesOnlyAfterTheOperationIsDone(t *testing.T) {
	t.Parallel()
	request := DiscoverRequest{ContextName: "lab", Names: []string{"a", "b"}, PreparePrerequisites: true, SkipConfirmation: true}
	for name, test := range map[string]struct {
		operations *preparedOperations
		wantResult bool
		wantReads  []string
		wantErr    bool
	}{
		"no operation":    {&preparedOperations{err: errors.New("refused")}, false, nil, true},
		"not done":        {&preparedOperations{operation: &lifecycle.OperationResult{Receipt: lifecycle.Receipt{State: string(reconciliation.OperationFailed)}}}, true, nil, false},
		"done, no inputs": {&preparedOperations{operation: &lifecycle.OperationResult{Receipt: lifecycle.Receipt{State: string(reconciliation.OperationDone)}}}, true, []string{"a", "b"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := NewDiscoveryService(test.operations).Discover(context.Background(), request)
			if (err != nil) != test.wantErr || (result != nil) != test.wantResult || len(test.operations.read) != len(test.wantReads) {
				t.Fatalf("result = %+v, err = %v, evidence read for %v", result, err, test.operations.read)
			}
			if got := test.operations.prepared[0]; got.Kind != DiscoveryPreparation || got.ContextName != "lab" || !got.PreparePrerequisites || !got.SkipConfirmation {
				t.Fatalf("prepared %+v", got)
			}
		})
	}
}

func TestPlanningDiscoveryNeverSkipsConfirmation(t *testing.T) {
	t.Parallel()
	operations := &preparedOperations{}
	if _, err := NewDiscoveryService(operations).PlanDiscovery(context.Background(), DiscoverRequest{ContextName: "lab", Names: []string{"a"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if got := operations.prepared[0]; got.Kind != DiscoveryPreparation || got.SkipConfirmation {
		t.Fatalf("planned %+v", got)
	}
}
