//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func contractMutationOperations(row contractRow) []string {
	if row.retirementOnly {
		return []string{"apply"}
	}
	return []string{"apply", "destroy"}
}
func contractSourceName(name string) string {
	if name == "addon-fixture" {
		return "lab-sno"
	}
	return name
}
func contractAddonRow() contractRow {
	refs := []co.Ref{{APIVersion: "v1", Kind: "Namespace", Name: "fixture"}, {APIVersion: "operators.coreos.com/v1", Kind: "OperatorGroup", Namespace: "fixture", Name: "fixture", Namespaced: true}, {APIVersion: "operators.coreos.com/v1alpha1", Kind: "Subscription", Namespace: "fixture", Name: "fixture", Namespaced: true}}
	objects := []co.ObjectObservation{}
	for _, ref := range refs {
		objects = append(objects, co.ObjectObservation{Ref: ref, Ownership: co.Absent})
	}
	return contractRow{kind: instance.Kind, implementation: instance.Implementation, example: "addon-fixture", retirementOnly: true, retains: true, removalObservesNothing: "specs/add-ons.md Retirement: removal retires from the frozen request without contacting the cluster", presence: contractAddonPresence, absence: map[string]any{"version": "addon-instance-evidence-v3", "retired": true}, noEffect: map[string]any{"version": "addon-instance-evidence-v3", "observations": objects, "checks": []co.CheckResult{{Index: 0}}, "olm": []co.OLMFacts{{Namespace: "fixture", Subscription: "fixture"}}}, drift: map[string]any{"olm": []co.OLMFacts{{Namespace: "fixture", Subscription: "fixture", InstalledCSV: "unexpected.v2.0.0"}}}}
}
func contractAddonState(state *compilation.State) *compilation.State {
	catalog := state.Effective()
	cluster := catalog.OfKind(api.OpenShiftCluster)[0].Name()
	sub := api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue("stable")}, api.FieldValue{Name: "source", Value: api.StringValue("catalog")}, api.FieldValue{Name: "sourceNamespace", Value: api.StringValue("openshift-marketplace")}, api.FieldValue{Name: "startingCSV", Value: api.StringValue("fixture.v1.0.0")})
	binding := api.NewObject(api.ClusterAddonBinding, "fixture-binding", api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue(cluster)}, api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: "fixture", Value: api.MapValue(api.FieldValue{Name: "subscription", Value: sub})})}))
	objects := append(catalog.Objects(), binding)
	catalog = api.NewCatalog(objects)
	return compilation.NewState(catalog, catalog, nil)
}

type contractAddonCapability struct {
	lifecycle.Capability
	planner instance.Capability
}

func (c contractAddonCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	return c.planner.Plan(ctx, input)
}
func (c contractAddonCapability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	return c.planner.Unsupported(state)
}
func contractInstallFixtureRegistry(t *testing.T, resolver capabilityResolver, runner *contractRunner) {
	t.Helper()
	registry, err := addons.NewRegistry(append(packages.Builtin().Packages(), addons.Package{Name: "fixture", Summary: "fixture package", Budget: time.Minute, OLM: &addons.OLM{Namespace: "fixture", OperatorGroup: addons.OwnNamespace, GroupName: "fixture", Subscription: "fixture", Package: "fixture", DefaultSource: "catalog", Approval: addons.Exact}, Render: func(addons.Instance) (addons.Rendered, error) { return addons.Rendered{}, nil }}))
	if err != nil {
		t.Fatal(err)
	}
	for i := range resolver {
		if resolver[i].implementation == instance.Implementation {
			resolver[i].capability = contractAddonCapability{Capability: instance.New(runner, registry, openshift.AddonFacts), planner: instance.New(nil, registry, openshift.AddonFacts)}
		}
	}
}
func contractAddonPresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := instance.DecodeRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	evidence := instance.Evidence{Version: "addon-instance-evidence-v3", Postcondition: true, Evidence: co.Evidence{Observations: []co.ObjectObservation{}, Receipts: []co.Receipt{}, Checks: []co.CheckResult{}, OLM: []co.OLMFacts{}, Dependents: []co.DependentFacts{}}}
	checks := len(program.Checks)
	for _, step := range program.Steps {
		checks += len(step.Wait)
		object := step.Create
		if step.Subscribe != nil {
			object = &step.Subscribe.Object
			evidence.OLM = append(evidence.OLM, co.OLMFacts{Namespace: object.Namespace, Subscription: object.Name, InstalledCSV: step.Subscribe.CSV})
		}
		if object != nil {
			evidence.Observations = append(evidence.Observations, co.ObjectObservation{Ref: object.Ref, Ownership: co.Own, Equal: true})
			evidence.Receipts = append(evidence.Receipts, co.Receipt{Ref: object.Ref, UID: "uid-" + object.Name, DesiredDigest: object.DesiredDigest, Disposition: "created", Basis: co.Created})
		}
	}
	for i := range checks {
		evidence.Checks = append(evidence.Checks, co.CheckResult{Index: i, Satisfied: true})
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
func TestRetirementOnlyCapabilitiesRefuseMalformedFrozenRequest(t *testing.T) {
	resolver := buildCapabilities(systemClock{}, controllerDependencies{}, nil)
	for _, row := range contractRows() {
		if !row.retirementOnly {
			continue
		}
		capability, _ := resolver.Resolve(row.kind, row.implementation)
		execution := lifecycle.Execution{Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: json.RawMessage(`{}`)}}}
		if _, err := capability.Destroy(context.Background(), execution); err == nil {
			t.Errorf("%s accepted malformed retirement", row.implementation)
		}
	}
}

func (c contractAddonCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	reporter, ok := c.Capability.(lifecycle.UnresolvedReporter)
	if !ok {
		return lifecycle.Unresolved{}, false
	}
	return reporter.Unresolved(verb, block, evidence)
}
