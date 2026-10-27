package lifecycle_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type addonAdapter struct {
	calls                    []string
	lost, drift, unreachable bool
	partial                  bool
	expectedReceipts         int
}

func (a *addonAdapter) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	a.calls = append(a.calls, run.Operation)
	if run.Operation == "apply" {
		password, ok := run.Material["addon-password"].Part(secrets.PasswordPart)
		username, userOK := run.Material["addon-password"].Part(secrets.UsernamePart)
		if !ok || !userOK || string(password) != "private-addon-password\n" || string(username) != "fixture-user" || len(run.Materials) != 3 {
			return lifecycle.RunResult{}, errors.New("declared usernamePassword material was not lent exactly")
		}
	} else if len(run.Materials) != 1 || len(run.Material) != 1 {
		return lifecycle.RunResult{}, errors.New("observation lent placed Secret bytes")
	}
	if a.unreachable {
		return lifecycle.RunResult{}, errors.New("cluster API unreachable")
	}
	if a.lost && run.Operation == "apply" {
		a.lost = false
		return lifecycle.RunResult{}, errors.New("outcome lost after object creation")
	}
	if a.expectedReceipts > 0 {
		var previous co.Evidence
		if err := json.Unmarshal([]byte(run.MaterialValues["evidence"]), &previous); err != nil || len(previous.Receipts) != a.expectedReceipts || len(previous.OLM) != 0 {
			return lifecycle.RunResult{}, errors.New("partial UID receipts were not preserved")
		}
	}
	request, err := instance.DecodeRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	program, err := request.Objects()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	evidence := instance.Evidence{Version: "addon-instance-evidence-v3", Request: run.Digest, Postcondition: true, Evidence: co.Evidence{Receipts: []co.Receipt{}, Observations: []co.ObjectObservation{}, Checks: []co.CheckResult{}, OLM: []co.OLMFacts{}, Dependents: []co.DependentFacts{}}}
	checks := len(program.Checks)
	for _, step := range program.Steps {
		checks += len(step.Wait)
		object := step.Create
		if step.Subscribe != nil {
			object = &step.Subscribe.Object
			csv := step.Subscribe.CSV
			if a.drift {
				csv = "unexpected.v2.0.0"
			}
			evidence.OLM = append(evidence.OLM, co.OLMFacts{Namespace: object.Namespace, Subscription: object.Name, InstalledCSV: csv})
		}
		if step.Place != nil {
			object = &co.ObjectEntry{Ref: co.Ref{APIVersion: "v1", Kind: "Secret", Namespace: step.Place.Namespace, Name: step.Place.Name, Namespaced: true}, DesiredDigest: step.Place.DesiredDigest}
		}
		if object != nil {
			evidence.Receipts = append(evidence.Receipts, co.Receipt{Ref: object.Ref, UID: "uid-" + object.Name, DesiredDigest: object.DesiredDigest, Disposition: "unchanged", Basis: co.Created})
			evidence.Observations = append(evidence.Observations, co.ObjectObservation{Ref: object.Ref, Ownership: co.Own, Equal: true})
		}
	}
	for i := range checks {
		evidence.Checks = append(evidence.Checks, co.CheckResult{Index: i, Satisfied: true})
	}
	if a.partial && run.Operation == "apply" {
		a.partial = false
		a.expectedReceipts = len(evidence.Receipts)
		evidence.Checks = nil
		evidence.Postcondition = false
		data, err := co.Canonical(evidence)
		return lifecycle.RunResult{Outcome: "changed", Evidence: data}, err
	}
	data, err := co.Canonical(evidence)
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: data}, err
}

type locatedAddon struct{ instance.Capability }

func (c locatedAddon) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return c.Capability.Apply(ctx, located(e))
}
func (c locatedAddon) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	return c.Capability.Observe(ctx, located(e))
}

func addonJourney(t *testing.T, lost bool) (*lifecycle.CapabilityJourney, *addonAdapter, string) {
	t.Helper()
	producerRequest := installedRequest()
	producerData, err := producerRequest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	producer := reconciliation.BlockDefinition{ID: producerRequest.Identity.Block, Description: "install cluster", Stage: reconciliation.StageClusters, Kind: agentinstall.Kind, Object: installedCluster, Implementation: agentinstall.InstallImplementation, ContentDigest: agentinstall.InstallContentDigest(), Request: producerData}
	pkg := addons.Package{Name: "fixture", Budget: time.Minute, OLM: &addons.OLM{Namespace: "fixture", OperatorGroup: addons.OwnNamespace, GroupName: "fixture", Subscription: "fixture", Package: "fixture", DefaultSource: "catalog", Approval: addons.Exact}, Render: func(addons.Instance) (addons.Rendered, error) {
		return addons.Rendered{After: []co.Phase{{Secrets: []co.SecretPlacement{{Secret: "addon-password", Namespace: "fixture", Name: "credentials", Type: "Opaque", Keys: []co.KeyPart{{Key: "username", Part: "username"}, {Key: "password", Part: "password"}}}}}}}, nil
	}}
	sub := api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("stable")}, api.FieldValue{Name: "source", Value: api.StringValue("catalog")}, api.FieldValue{Name: "sourceNamespace", Value: api.StringValue("openshift-marketplace")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue("fixture.v1.0.0")})
	request, requires, err := instance.Render(addons.SelectedInstance{Package: pkg, Instance: addons.Instance{Cluster: installedCluster, Binding: "fixture-binding", Config: api.MapValue(api.FieldValue{Name: "subscription", Value: sub})}}, addons.ClusterFacts{Name: installedCluster, Release: installedRelease}, "controller")
	if err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	definition := reconciliation.BlockDefinition{ID: instance.BlockID("fixture", installedCluster), Description: "install add-on fixture", Stage: reconciliation.StageAddOns, Kind: instance.Kind, Object: instance.Object(installedCluster, "fixture"), Implementation: instance.Implementation, ContentDigest: instance.ContentDigest(), Request: data, Requires: requires}
	adapter := &addonAdapter{lost: lost}
	cluster := &installer{completed: true}
	journey := lifecycle.NewBoundJourney(t, []lifecycle.BoundCapability{{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation}, Capability: retainedInstaller{agentinstall.NewInstall(cluster)}}, {Binding: lifecycle.CapabilityBinding{Kind: instance.Kind, Implementation: instance.Implementation}, Capability: locatedAddon{instance.New(adapter, addons.Registry{}, nil)}}}, lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{producer, definition}, Secrets: append(producerRequest.SecretReferences(), "addon-password")})
	journey.Holding("addon-password", secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("fixture-user"), secrets.PasswordPart: []byte("private-addon-password\n")}))
	return journey, adapter, definition.ID
}
func TestAddonInstanceEngineContinuesLostOutcomeAndSettledApplyContactsNothing(t *testing.T) {
	journey, adapter, block := addonJourney(t, true)
	ctx := context.Background()
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Apply(ctx, apply); err == nil {
		t.Fatal("lost outcome succeeded")
	}
	if _, err := journey.Service.Apply(ctx, apply); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !slices.Equal(adapter.calls, []string{"apply", "observe"}) {
		t.Fatal(adapter.calls)
	}
	records := journey.Records(t, block)
	if len(records) != 2 || records[1].Effect != reconciliation.EffectCompleted {
		t.Fatal(records)
	}
	if _, err := journey.Service.Apply(ctx, apply); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(adapter.calls) != 2 {
		t.Fatal("settled apply contacted cluster")
	}
	for _, record := range records {
		if strings.Contains(string(record.Evidence), "private-addon-password") {
			t.Fatal("Secret leaked to evidence")
		}
	}
	adapter.unreachable = true
	if _, err := journey.Service.Destroy(ctx, lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(adapter.calls) != 2 {
		t.Fatal("retirement contacted cluster")
	}
}
func TestAddonInstanceUnknownRetiresWithoutReachingUnreachableAPI(t *testing.T) {
	journey, adapter, _ := addonJourney(t, true)
	ctx := context.Background()
	if _, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil {
		t.Fatal("lost outcome succeeded")
	}
	adapter.unreachable = true
	if _, err := journey.Service.Destroy(ctx, lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !slices.Equal(adapter.calls, []string{"apply"}) {
		t.Fatal("unknown retirement observed API", adapter.calls)
	}
}
func TestAddonInstanceDiffObservesAnotherInstalledCSV(t *testing.T) {
	journey, adapter, _ := addonJourney(t, false)
	ctx := context.Background()
	if _, err := journey.Service.Apply(ctx, lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	adapter.drift = true
	result, err := journey.Service.Diff(ctx, lifecycle.DiffRequest{ContextName: lifecycle.JourneyContext, Observe: true})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "unexpected.v2.0.0") || !slices.Equal(adapter.calls, []string{"apply", "observe"}) {
		t.Fatalf("drift not reported: %s calls %v", data, adapter.calls)
	}
}

func TestAddonInstanceContinuesWithReceiptsFromPartialFailure(t *testing.T) {
	journey, adapter, _ := addonJourney(t, false)
	adapter.partial = true
	request := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Apply(context.Background(), request); err == nil {
		t.Fatal("partial apply succeeded")
	}
	if _, err := journey.Service.Apply(context.Background(), request); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !slices.Equal(adapter.calls, []string{"apply", "observe"}) {
		t.Fatal(adapter.calls)
	}
}
