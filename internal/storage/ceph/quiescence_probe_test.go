package ceph

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func corruptPolicyProof(cluster string) lifecycle.BlockEvidence {
	return retirementRecord(reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{
		Kind: "CephCluster", Object: cluster, Implementation: PhysicalRetirementImplementation, Request: []byte(`{}`),
	}}, reconciliation.Apply, []byte(`{}`))
}

func TestConsumerQuiescenceKeepsCancellationAndProbeFailureApart(t *testing.T) {
	consumer, e := stretchConsumer(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	q, err := NewConsumer(nil).Quiescent(cancelled, lifecycle.Probe{Block: e.Block, ApplyOperation: e.ApplyOperation, RetiredWithHosts: true})
	if !errors.Is(err, context.Canceled) || q.Reason == survivingReason {
		t.Fatalf("cancelled consumer probe = %+v (%v)", q, err)
	}
	failing := lifecycle.Probe{Block: e.Block, ApplyOperation: e.ApplyOperation, RetiredWithHosts: true, ApplyProved: []lifecycle.BlockEvidence{corruptPolicyProof(consumer.Cluster)}}
	q, err = NewConsumer(nil).Quiescent(context.Background(), failing)
	if err == nil || q.Reason == survivingReason {
		t.Fatalf("failing consumer probe = %+v (%v)", q, err)
	}
	surviving := lifecycle.Probe{Block: e.Block, ApplyOperation: e.ApplyOperation}
	q, err = NewConsumer(nil).Quiescent(context.Background(), surviving)
	if err != nil || q.State != lifecycle.Unproved || q.Reason != survivingReason || q.Remedy != survivingRemedy {
		t.Fatalf("surviving consumer probe = %+v (%v)", q, err)
	}
}

func TestSMBQuiescenceReportsAFailedProbeWithItsCause(t *testing.T) {
	r, e := retiredSMB(t, requestMap("label", "mon", "count", 2))
	probe := lifecycle.Probe{Block: e.Block, ApplyOperation: e.ApplyOperation, RetiredWithHosts: true, ApplyProved: []lifecycle.BlockEvidence{corruptPolicyProof(r.Cluster)}}
	q, err := NewSMB(nil).Quiescent(context.Background(), probe)
	if err == nil || q.Reason == survivingReason {
		t.Fatalf("failing SMB probe = %+v (%v)", q, err)
	}
	probe.ApplyProved = nil
	probe.Block.Request = []byte(`{}`)
	q, err = NewSMB(nil).Quiescent(context.Background(), probe)
	if err == nil || q.Reason == survivingReason {
		t.Fatalf("undecodable SMB probe = %+v (%v)", q, err)
	}
}

func TestReplicationQuiescenceReportsAFailedProbeWithItsCause(t *testing.T) {
	r, e := stretchReplication(t)
	probe := lifecycle.Probe{Block: e.Block, ApplyOperation: e.ApplyOperation, RetiredWithHosts: true, ApplyProved: []lifecycle.BlockEvidence{corruptPolicyProof(r.Primary.Cluster)}}
	q, err := NewReplication(nil).Quiescent(context.Background(), probe)
	if err == nil || q.Reason == survivingReason {
		t.Fatalf("failing replication probe = %+v (%v)", q, err)
	}
	probe.ApplyProved = nil
	probe.Block.Request = []byte(`{}`)
	q, err = NewReplication(nil).Quiescent(context.Background(), probe)
	if err == nil || q.Reason == survivingReason {
		t.Fatalf("undecodable replication probe = %+v (%v)", q, err)
	}
}
