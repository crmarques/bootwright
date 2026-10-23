package lifecycle_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects/driver"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/containercluster/networking"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type networkAdapter struct {
	calls                               []string
	lost, unreachable, unready, foreign bool
}

func (a *networkAdapter) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	a.calls = append(a.calls, run.Operation)
	if len(run.Materials) != 1 || len(run.Material) != 1 {
		return lifecycle.RunResult{}, errors.New("network borrowed unexpected material")
	}
	if a.unreachable {
		return lifecycle.RunResult{}, errors.New("cluster API unreachable")
	}
	if a.lost && run.Operation == "apply" {
		a.lost = false
		return lifecycle.RunResult{}, errors.New("network create response lost")
	}
	request, err := networking.DecodeRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	program, err := request.Objects()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	evidence := driver.Evidence{Version: "cluster-network-evidence-v2", Request: run.Digest, Postcondition: !a.unready && !a.foreign}
	checks := len(program.Checks)
	for _, step := range program.Steps {
		checks += len(step.Wait)
		if step.Create == nil {
			continue
		}
		object := step.Create
		ownership := co.Own
		if a.foreign && object.Kind == "NodeNetworkConfigurationPolicy" {
			ownership = co.Foreign
		}
		evidence.Observations = append(evidence.Observations, co.ObjectObservation{Ref: object.Ref, Ownership: ownership, Equal: ownership == co.Own})
		if ownership == co.Own {
			evidence.Receipts = append(evidence.Receipts, co.Receipt{Ref: object.Ref, UID: "uid-" + object.Name, DesiredDigest: object.DesiredDigest, Disposition: "created"})
		}
	}
	for i := range checks {
		evidence.Checks = append(evidence.Checks, co.CheckResult{Index: i, Satisfied: !a.unready, LastObservation: "Available"})
	}
	data, err := co.Canonical(evidence)
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: data}, err
}

type locatedNetwork struct{ networking.Capability }

func (c locatedNetwork) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return c.Capability.Apply(ctx, located(e))
}
func (c locatedNetwork) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	return c.Capability.Observe(ctx, located(e))
}

func networkJourney(t *testing.T) (*lifecycle.CapabilityJourney, *networkAdapter, string) {
	t.Helper()
	producerRequest := installedRequest()
	producerData, err := producerRequest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	producer := reconciliation.BlockDefinition{ID: producerRequest.Identity.Block, Description: "install cluster", Stage: reconciliation.StageClusters, Kind: agentinstall.Kind, Object: installedCluster, Implementation: agentinstall.InstallImplementation, ContentDigest: agentinstall.InstallContentDigest(), Request: producerData}
	field := func(name string, value api.Value) api.FieldValue { return api.FieldValue{Name: name, Value: value} }
	cluster := api.NewObject(api.ContainerCluster, installedCluster, api.Value{}, api.MapValue(
		field("nodes", api.ListValue(api.MapValue(field("name", api.StringValue("sno-01"))))),
		field("networking", api.MapValue(field("secondaryNetworks", api.ListValue(api.MapValue(
			field("name", api.StringValue("tenant")), field("uplink", api.MapValue(field("bridge", api.StringValue("br-ex")))), field("namespaceSelector", api.MapValue(field("matchLabels", api.MapValue(field("tenant", api.StringValue("yes"))))))))))),
	))
	program, err := networking.Render(cluster)
	if err != nil {
		t.Fatal(err)
	}
	programData, err := co.Freeze(program, co.Options{})
	if err != nil {
		t.Fatal(err)
	}
	request := networking.Request{Version: "cluster-network-request-v1", BudgetSeconds: 1800, Cluster: installedCluster, Placement: producerRequest.Placement, ProducedInputs: []lifecycle.ProducedInput{{Kind: "ContainerCluster", Object: installedCluster, Output: "kubeconfig"}}, Program: programData, Tool: networking.Tool{Kind: "openshift-clients", Compatibility: "openshift", Version: installedRelease}}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{ID: networking.BlockID(installedCluster), Description: "configure secondary networks", Stage: reconciliation.StageAddOns, Kind: networking.Kind, Object: installedCluster, Implementation: networking.Implementation, ContentDigest: networking.ContentDigest(), Request: data, Requires: []reconciliation.ObjectRef{{Kind: "ContainerCluster", Object: installedCluster}}}
	adapter := &networkAdapter{}
	journey := lifecycle.NewBoundJourney(t, []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation}, Capability: retainedInstaller{agentinstall.NewInstall(&installer{completed: true})}},
		{Binding: lifecycle.CapabilityBinding{Kind: networking.Kind, Implementation: networking.Implementation}, Capability: locatedNetwork{networking.New(adapter, addons.Registry{})}},
	}, lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{producer, definition}, Secrets: producerRequest.SecretReferences()})
	return journey, adapter, definition.ID
}

func TestClusterNetworkContinuesLostOutcomeAndSettledApplyIsOffline(t *testing.T) {
	journey, adapter, block := networkJourney(t)
	adapter.lost = true
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Apply(context.Background(), apply); err == nil {
		t.Fatal("lost response succeeded")
	}
	if _, err := journey.Service.Apply(context.Background(), apply); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	records := journey.Records(t, block)
	if len(records) != 2 || records[1].Effect != reconciliation.EffectCompleted {
		t.Fatal(records)
	}
	if _, err := journey.Service.Apply(context.Background(), apply); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !slices.Equal(adapter.calls, []string{"apply", "observe"}) {
		t.Fatal(adapter.calls)
	}
	adapter.unreachable = true
	if _, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(adapter.calls) != 2 {
		t.Fatal("retirement contacted cluster", adapter.calls)
	}
}
func TestClusterNetworkUnknownRetiresWithoutAPI(t *testing.T) {
	journey, adapter, _ := networkJourney(t)
	adapter.lost = true
	if _, err := journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil {
		t.Fatal("lost response succeeded")
	}
	adapter.unreachable = true
	if _, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !slices.Equal(adapter.calls, []string{"apply"}) {
		t.Fatal(adapter.calls)
	}
}
func TestClusterNetworkReadinessAndForeignOwnershipStayUnresolved(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "not ready", true: "foreign"}[foreign], func(t *testing.T) {
			journey, adapter, block := networkJourney(t)
			adapter.unready = !foreign
			adapter.foreign = foreign
			apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
			if _, err := journey.Service.Apply(context.Background(), apply); err == nil {
				t.Fatal("unproved network succeeded")
			}
			if _, err := journey.Service.Apply(context.Background(), apply); err == nil {
				t.Fatal("unproved observation succeeded")
			}
			records := journey.Records(t, block)
			want := reconciliation.EffectPartial
			if foreign {
				want = reconciliation.EffectUnknown
			}
			if len(records) < 2 || records[1].Effect != want {
				t.Fatalf("records=%d, expected resolution %s", len(records), want)
			}
			adapter.unready = false
			adapter.foreign = false
			if _, err := journey.Service.Apply(context.Background(), apply); err != nil {
				t.Fatal(diagnostics.Of(err))
			}
		})
	}
}
