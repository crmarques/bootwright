package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type liveRetirementMachine struct {
	*probedMachines
	refuse string
}

func (liveRetirementMachine) ProbesQuiescence() bool { return false }

func (m *liveRetirementMachine) StartPower(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	if m.refuse != "" && probe.Block.Object == m.refuse {
		return lifecycle.Quiescence{}, errors.New("the second host's power could not be proved")
	}
	return m.probedMachines.StartPower(ctx, probe)
}

func (m liveRetirementMachine) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	stopped, err := m.StartPower(ctx, lifecycle.Probe{})
	if err != nil || !stopped.Settled() {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, errors.New("the retirement host was not stopped before machine removal")
	}
	return m.fleetMachines.Destroy(ctx, e)
}

type liveRetirementCluster struct {
	*fleetMachines
	calls []string
}

func (*liveRetirementCluster) HostMachines(reconciliation.Block) ([]string, error) {
	return []string{"example-node-a", "example-node-b"}, nil
}

func (c *liveRetirementCluster) OnlineRetirementHosts(block reconciliation.Block) ([]string, error) {
	return c.HostMachines(block)
}

func (c *liveRetirementCluster) Quiescent(context.Context, lifecycle.Probe) (lifecycle.Quiescence, error) {
	return lifecycle.Quiescence{State: lifecycle.Quiescent}, nil
}

func (c *liveRetirementCluster) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	c.calls = append(c.calls, "retire")
	state, err := c.fleetMachines.Quiescent(ctx, lifecycle.Probe{})
	if err != nil || state.State != lifecycle.Live || !e.RetiredWithHosts {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, errors.New("cannot reach the retirement host")
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"retired":true}`)}, nil
}

func (c *liveRetirementCluster) ObserveRemoval(ctx context.Context, _ lifecycle.Execution) (lifecycle.Observation, error) {
	c.calls = append(c.calls, "observe")
	state, err := c.fleetMachines.Quiescent(ctx, lifecycle.Probe{})
	if err != nil || state.State != lifecycle.Live {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, errors.New("cannot observe the retirement host")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectNoEffect}, nil
}

type liveRetirementPreStop struct {
	*fleetMachines
	cluster *liveRetirementCluster
	fail    bool
}

func (*liveRetirementPreStop) RequiresPreStopRemoval(reconciliation.Block) bool { return true }

func (p *liveRetirementPreStop) Destroy(_ context.Context, _ lifecycle.Execution) (lifecycle.Result, error) {
	p.cluster.calls = append(p.cluster.calls, "pre-stop")
	if p.fail {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, errors.New("the pre-stop inverse lost its reply")
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"released":true}`)}, nil
}

func (*liveRetirementPreStop) ObserveRemoval(context.Context, lifecycle.Execution) (lifecycle.Observation, error) {
	return lifecycle.Observation{Effect: reconciliation.EffectNoEffect}, nil
}

func TestResumedOnlineRetirementProvesCleanupBeforeStoppingItsHost(t *testing.T) {
	for _, test := range []struct {
		name      string
		preStop   bool
		refuse    string
		wantCalls []string
	}{
		{name: "retirement retry", wantCalls: []string{"retire", "observe", "retire", "stop:example-node-a", "stop:example-node-b"}},
		{name: "early pre-stop inverse", preStop: true, wantCalls: []string{"pre-stop", "pre-stop", "retire", "stop:example-node-b", "stop:example-node-a"}},
		{name: "whole stop preflight", refuse: "example-node-b", wantCalls: []string{"retire", "observe", "retire"}},
	} {
		t.Run(test.name, func(t *testing.T) { testResumedOnlineRetirement(t, test.preStop, test.refuse, test.wantCalls) })
	}
}

func testResumedOnlineRetirement(t *testing.T, withPreStop bool, refuse string, wantCalls []string) {
	t.Helper()
	ctx := context.Background()
	fleet := &fleetMachines{}
	machine := &liveRetirementMachine{probedMachines: &probedMachines{fleetMachines: fleet}}
	cluster := &liveRetirementCluster{fleetMachines: fleet}
	bindings := []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.Machine), Implementation: fleetMachineImplementation, Family: reconciliation.ManagedMachineFamily}, Capability: machine},
		{Binding: lifecycle.CapabilityBinding{Kind: string(api.CephCluster), Implementation: "fixture-cluster"}, Capability: cluster},
	}
	var definitions []reconciliation.BlockDefinition
	hosts, _ := cluster.HostMachines(reconciliation.Block{})
	var upstream []string
	for _, name := range hosts {
		upstream = append(upstream, "machine-"+name)
		definitions = append(definitions, reconciliation.BlockDefinition{ID: "machine-" + name, Description: "create the Machine", Stage: reconciliation.StageMachines,
			Kind: string(api.Machine), Object: name, Implementation: fleetMachineImplementation, ContentDigest: strings.Repeat("c", 64), Request: json.RawMessage(`{"name":"` + name + `"}`)})
	}
	preStop := &liveRetirementPreStop{fleetMachines: fleet, cluster: cluster, fail: true}
	if withPreStop {
		bindings = append(bindings, lifecycle.BoundCapability{Binding: lifecycle.CapabilityBinding{Kind: "FixtureInstall", Implementation: "fixture-install"}, Capability: preStop})
		definitions = append(definitions, reconciliation.BlockDefinition{ID: "install-example-node-a", Description: "install the Machine", Stage: reconciliation.StageMachines,
			Kind: "FixtureInstall", Object: hosts[0], Implementation: "fixture-install", ContentDigest: strings.Repeat("d", 64), Dependencies: []string{upstream[0]}, Request: json.RawMessage(`{}`)})
		upstream = append(upstream, "install-example-node-a")
	}
	definitions = append(definitions, reconciliation.BlockDefinition{ID: "cluster-example", Description: "create the cluster", Stage: reconciliation.StageClusters, Kind: string(api.CephCluster), Object: "example",
		Implementation: "fixture-cluster", ContentDigest: strings.Repeat("e", 64), Dependencies: upstream, Request: json.RawMessage(`{"name":"example"}`)})
	journey := lifecycle.NewBoundJourney(t, bindings, lifecycle.CapabilityPlan{Definitions: definitions})
	journey.WithMachineDriver(func(_ context.Context, _ lifecycle.Execution, proof lifecycle.BlockEvidence, start bool) error {
		if start {
			t.Error("the resumed destroy unexpectedly started a machine")
		}
		cluster.calls = append(cluster.calls, "stop:"+proof.Object)
		setRunning(fleet, start)
		return nil
	})
	apply, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || apply.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("apply = %+v (%v)", apply, err)
	}
	destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	setRunning(fleet, false)
	first, err := journey.Service.Destroy(ctx, destroy)
	if err == nil || first.Receipt.State != string(reconciliation.OperationUnknown) {
		t.Fatalf("first destroy = %+v (%v), want an unproved retirement", first, err)
	}
	setRunning(fleet, true)
	preStop.fail, machine.refuse = false, refuse
	next, err := journey.Service.Destroy(ctx, destroy)
	if refuse != "" {
		if err == nil || !slices.Equal(cluster.calls, wantCalls) || len(fleet.removed) != 0 {
			t.Fatalf("continuation = %+v (%v), actions %v, removed %v", next, err, cluster.calls, fleet.removed)
		}
		return
	}
	if err != nil || next.Receipt.State != string(reconciliation.OperationDone) ||
		!slices.Equal(cluster.calls, wantCalls) {
		t.Fatalf("continuation = %+v (%v), actions %v", next, err, cluster.calls)
	}
}
