package lifecycle

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type powerHostedAddon struct{ Capability }

func (powerHostedAddon) HostObject(reconciliation.Block) (reconciliation.ObjectRef, error) {
	return reconciliation.ObjectRef{Kind: "OpenShiftCluster", Object: "cluster"}, nil
}

type powerClusterNodes struct{ Capability }

func (powerClusterNodes) NodeMachines(reconciliation.Block) ([]string, error) {
	return []string{"node-b", "node-a"}, nil
}

func TestAddedAddonStartsOnlyItsInstalledClusterNodes(t *testing.T) {
	const kind, implementation = "OpenShiftCluster", "installed-cluster"
	service := Service{capabilities: claimResolver{
		CapabilityBinding{Kind: kind, Implementation: implementation}: powerClusterNodes{},
	}}
	var started []string
	execution := Execution{ApplyProved: []BlockEvidence{
		{Kind: kind, Object: "another", Implementation: implementation},
		{Kind: kind, Object: "cluster", Implementation: implementation},
	}, PowerMachine: func(_ context.Context, name string, on bool) error {
		if !on {
			t.Fatal("add-on stopped a Machine")
		}
		started = append(started, name)
		return nil
	}}
	if err := service.startClusterNodes(context.Background(), powerHostedAddon{}, execution); err != nil || !slices.Equal(started, []string{"node-a", "node-b"}) {
		t.Fatalf("starts %v, error %v", started, err)
	}
	failure := errors.New("management controller unreachable")
	execution.PowerMachine = func(context.Context, string, bool) error { return failure }
	if err := service.startClusterNodes(context.Background(), powerHostedAddon{}, execution); !errors.Is(err, failure) {
		t.Fatalf("power failure lost: %v", err)
	}
	execution.ApplyProved = execution.ApplyProved[:1]
	if err := service.startClusterNodes(context.Background(), powerHostedAddon{}, execution); err == nil {
		t.Fatal("an unrelated cluster was accepted")
	}
}
