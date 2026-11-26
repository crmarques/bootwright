package operationstore

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// B1862: a removal whose step is taken back on kept hosts records that
// effect's own contract beside its plan's, and only such a removal may.
func TestARemovalOnKeptHostsRecordsItsKeptHostContract(t *testing.T) {
	definition := testPlan(t, "alpha").Blocks[0].BlockDefinition
	definition.Impacts, definition.Inverse = []string{"tear down alpha on Machine/node-1"}, &reconciliation.Inverse{KeptHosts: []string{"node-1"}}
	keeping, err := reconciliation.NewPlan(reconciliation.Destroy, []reconciliation.BlockDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	definition.Impacts, definition.Inverse = nil, nil
	whole, err := reconciliation.NewPlan(reconciliation.Destroy, []reconciliation.BlockDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	step := strings.Repeat("4", 64)
	kept := map[string]string{"artifact-server-nginx-v1": step, "storage-ceph-kept-teardown-v1": strings.Repeat("5", 64)}
	for name, test := range map[string]struct {
		plan      reconciliation.Plan
		contracts map[string]string
		admitted  bool
	}{
		"its kept-host contract":      {keeping, kept, true},
		"no kept-host contract":       {keeping, map[string]string{"artifact-server-nginx-v1": step}, true},
		"one keeping no host":         {whole, kept, false},
		"another extra contract":      {keeping, map[string]string{"artifact-server-nginx-v1": step, "storage-other-v1": step}, false},
		"an empty kept-host contract": {keeping, map[string]string{"artifact-server-nginx-v1": step, "storage-ceph-kept-teardown-v1": ""}, false},
		"no step contract":            {keeping, map[string]string{"storage-ceph-kept-teardown-v1": step, "storage-ceph-host-kept-release-v1": step}, false},
	} {
		t.Run(name, func(t *testing.T) {
			operation := testOperation(t, test.plan)
			operation.Contracts = test.contracts
			if err := checkContracts(operation, test.plan, map[string]KeptNeed{"storage-ceph-kept-teardown-v1": KeepsHosts, "storage-ceph-host-kept-release-v1": KeepsHosts}); (err == nil) != test.admitted {
				t.Fatalf("the removal's contracts %v read %v", test.contracts, err)
			}
		})
	}
}
