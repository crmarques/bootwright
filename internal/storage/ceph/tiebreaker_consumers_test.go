package ceph

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

var stretchInstalled = []string{"node-1", "node-2", "node-3", "node-4"}

// stretchConsumer plans a CephConsumer of the stretch cluster whose
// tiebreaker is a provided Machine.
func stretchConsumer(t *testing.T) (ConsumerRequest, lifecycle.Execution) {
	t.Helper()
	catalog, _ := tiebreakerCatalog(func(m, c api.Value) (api.Value, api.Value) {
		c = withMDS(c)
		return m, c.WithPath(api.ListValue(append(c.Get("services").Items(), requestMap("service_type", "prometheus", "placement", requestMap("count", 1)))...), "services")
	})
	objects := append(catalog.Objects(), cephfsPool("fs-meta", "cephfsMetadata"), cephfsPool("fs-data", "cephfsData"), cephfsPool("block", "rbd"),
		filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "csi"))),
		childOf(api.CephConsumer, "consumer", "dataFoundation", requestMap("rbdPoolRef", "block", "filesystemRef", "files", "cephxKeyType", "aes")))
	catalog = api.NewCatalog(objects)
	plan, err := NewConsumer(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, Controller: "controller", State: compilation.NewState(catalog, catalog, nil)})
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("stretch consumer plan: %+v %+v", plan, diagnostics.Of(err))
	}
	r, err := DecodeConsumerRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	return r, lifecycle.Execution{Block: reconciliation.Block{BlockDefinition: plan.Definitions[0]}, ApplyOperation: "apply-original", Operation: "destroy-later", RetiredWithHosts: true}
}

// stretchReplication is a planned replication whose primary endpoint is the
// stretch cluster with a provided tiebreaker.
func stretchReplication(t *testing.T) (ReplicationRequest, lifecycle.Execution) {
	t.Helper()
	r, e := replicationExecution(t)
	stretch := providedStretchRequest(t)
	r.Primary.Machines, r.Primary.Provided = requestMachines(stretch), providedMachines(stretch)
	e.Block.Request = mustCanonical(t, r)
	e.Operation, e.RetiredWithHosts = "destroy-later", true
	return r, e
}

func mustCanonical(t *testing.T, value interface{ Canonical() ([]byte, error) }) json.RawMessage {
	t.Helper()
	data, err := value.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTheConsumerAndReplicationBindingsLeaveOutOnlyTheProvidedTiebreaker(t *testing.T) {
	consumer, e := stretchConsumer(t)
	if !slices.Equal(consumer.Machines, append(slices.Clone(stretchInstalled), "tie")) || !slices.Equal(consumer.Provided, []string{"tie"}) {
		t.Fatalf("the consumer froze machines %v and provided %v", consumer.Machines, consumer.Provided)
	}
	if machines, err := NewConsumer(nil).HostMachines(e.Block); err != nil || !slices.Equal(machines, stretchInstalled) {
		t.Fatalf("consumer host machines %v (%v)", machines, err)
	}
	replication, e := stretchReplication(t)
	machines, err := NewReplication(nil).HostMachines(e.Block)
	if err != nil || !slices.Equal(machines, append(slices.Clone(stretchInstalled), replication.Secondary.Machines...)) {
		t.Fatalf("replication host machines %v (%v)", machines, err)
	}
	unprovided := managedExportPlan(t, false)
	if r, _ := DecodeConsumerRequest(unprovided.Request); len(r.Provided) != 0 {
		t.Fatalf("an installed cluster's consumer froze provided %v", r.Provided)
	}
	if machines, _ := NewConsumer(nil).HostMachines(reconciliation.Block{BlockDefinition: unprovided}); !slices.Equal(machines, []string{"node-1", "node-2", "node-3"}) {
		t.Fatalf("an installed host left the consumer binding: %v", machines)
	}
	_, plain := replicationExecution(t)
	if machines, _ := NewReplication(nil).HostMachines(plain.Block); len(machines) != 6 {
		t.Fatalf("an installed host left the replication binding: %v", machines)
	}
}

func TestAFrozenProvidedMachineMustBeOneOfTheBlocksMachinesAndLeaveAnInstalledOne(t *testing.T) {
	consumer, _ := stretchConsumer(t)
	replication, _ := stretchReplication(t)
	for name, provided := range map[string][]string{"foreign": {"other"}, "every host": consumer.Machines, "unsorted": {"tie", "node-1"}, "repeated": {"tie", "tie"}} {
		changed := consumer
		changed.Provided = provided
		if _, err := DecodeConsumerRequest(mustCanonical(t, changed)); err == nil {
			t.Errorf("consumer with %s provided machines decoded", name)
		}
		end := replication
		end.Primary.Provided = provided
		if _, err := DecodeReplicationRequest(mustCanonical(t, end)); err == nil {
			t.Errorf("replication with %s provided machines decoded", name)
		}
	}
}

func TestTheConsumerAndReplicationRetireWithoutReachingTheTiebreaker(t *testing.T) {
	consumer, e := stretchConsumer(t)
	runner := &tiebreakerRunner{}
	result, err := NewConsumer(runner).Destroy(context.Background(), e)
	var recorded Retirement
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(runner.runs) != 0 || json.Unmarshal(result.Evidence, &recorded) != nil ||
		recorded.Action != "export" || !slices.Equal(recorded.RetiredWith, consumer.Machines) || recorded.Tiebreaker != nil {
		t.Fatalf("consumer retirement %s %+v after %d runs", result.Evidence, diagnostics.Of(err), len(runner.runs))
	}
	q, err := NewConsumer(runner).Quiescent(context.Background(), lifecycle.Probe{Block: e.Block, ApplyOperation: e.ApplyOperation, RetiredWithHosts: true})
	if err != nil || q.State != lifecycle.Quiescent {
		t.Fatalf("consumer quiescence %+v (%v)", q, err)
	}
	replication, e := stretchReplication(t)
	result, err = NewReplication(runner).Destroy(context.Background(), e)
	var record struct {
		RetiredWith []string `json:"retiredWith"`
	}
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(runner.runs) != 0 || json.Unmarshal(result.Evidence, &record) != nil ||
		!slices.Equal(record.RetiredWith, replicationMachines(replication)) || !slices.Contains(record.RetiredWith, "tie") {
		t.Fatalf("replication retirement %s %+v after %d runs", result.Evidence, diagnostics.Of(err), len(runner.runs))
	}
	e.RetiredWithHosts = false
	_, err = NewReplication(runner).Destroy(context.Background(), e)
	refusedWith(t, err, "lifecycle.unsupported", survivingReason, survivingRemedy)
}

func TestAReplicationEndpointWhoseProvidedMachinesDifferFromItsClusterProofRefuses(t *testing.T) {
	r, e := replicationExecution(t)
	r.Primary.Provided = []string{r.Primary.Machines[2]}
	e.Block.Request = mustCanonical(t, r)
	_, _, err := NewReplication(nil).prepare(context.Background(), e)
	refusedWith(t, err, "lifecycle.state", "replication machines do not match original cluster creation evidence", "")
}

func TestAConsumerWhoseProvidedMachinesDifferFromItsClusterProofRefuses(t *testing.T) {
	block := managedExportPlan(t, false)
	catalog := filesystemCatalog(withMDS)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	base, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	base.Action, base.Controller = "cluster", "controller"
	proofs := []lifecycle.BlockEvidence{coreClusterProof(t, base, nil), imageProof(t, "example", base.Image, resolvedDigest)}
	r, err := DecodeConsumerRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	for name, provided := range map[string][]string{"as proved": nil, "another provided host": {r.Machines[2]}} {
		t.Run(name, func(t *testing.T) {
			changed := r
			changed.Provided = provided
			frozen := block
			frozen.Request = mustCanonical(t, changed)
			e := lifecycle.Execution{Block: reconciliation.Block{BlockDefinition: frozen}, ApplyOperation: "apply-original", Operation: "apply-retry", ApplyProved: proofs}
			_, _, err := NewConsumer(nil).runExport(context.Background(), e, "observe")
			mismatch := len(diagnostics.Of(err)) == 1 && diagnostics.Of(err)[0].Message == "Ceph consumer host set differs from its proved cluster"
			if mismatch != (provided != nil) {
				t.Fatalf("run %+v", diagnostics.Of(err))
			}
		})
	}
}
