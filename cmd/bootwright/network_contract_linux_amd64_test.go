package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/containercluster/networking"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func contractNetworkRow() contractRow {
	return contractRow{kind: networking.Kind, implementation: networking.Implementation, example: "contract-secondary-network", presence: contractNetworkPresence, retains: true, retirementOnly: true, removalObservesNothing: "secondary networks retire with their cluster without touching live objects", noEffectUnreported: "cluster objects require positive per-object absence evidence", drift: map[string]any{"observations": []any{}}}
}
func contractNetworkPresence(t *testing.T, execution lifecycle.Execution) map[string]any {
	t.Helper()
	request, err := networking.DecodeRequest(execution.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	evidence := co.Evidence{Receipts: []co.Receipt{}, OLM: []co.OLMFacts{}, Dependents: []co.DependentFacts{}, Checks: []co.CheckResult{}, Observations: []co.ObjectObservation{}}
	checks := len(program.Checks)
	for _, step := range program.Steps {
		checks += len(step.Wait)
		if step.Create != nil {
			evidence.Receipts = append(evidence.Receipts, co.Receipt{Ref: step.Create.Ref, UID: "uid-" + step.Create.Name, DesiredDigest: step.Create.DesiredDigest, Disposition: "created"})
			evidence.Observations = append(evidence.Observations, co.ObjectObservation{Ref: step.Create.Ref, Ownership: co.Own, Equal: true})
		}
	}
	for i := 0; i < checks; i++ {
		evidence.Checks = append(evidence.Checks, co.CheckResult{Index: i, Satisfied: true, LastObservation: "ready"})
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["version"] = "cluster-network-evidence-v2"
	fields["postcondition"] = true
	fields["classification"] = "completed"
	return fields
}
func contractNetworkExample(t *testing.T, resolver capabilityResolver) contractExample {
	t.Helper()
	sources := exampleDirectory(t, "lab-sno")
	clusterName := ""
	for i, file := range sources.Files {
		var doc map[string]any
		if err := yaml.Unmarshal(file.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc["kind"] != "ContainerCluster" {
			continue
		}
		metadata := doc["metadata"].(map[string]any)
		clusterName = metadata["name"].(string)
		spec := doc["spec"].(map[string]any)
		network, _ := spec["networking"].(map[string]any)
		if network == nil {
			network = map[string]any{}
		}
		network["secondaryNetworks"] = []any{map[string]any{"name": "tenant", "topology": "Localnet", "uplink": map[string]any{"bridge": "br-ex"}, "namespaceSelector": map[string]any{"matchLabels": map[string]any{"tenant": "yes"}}}}
		spec["networking"] = network
		data, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		sources.Files[i] = desiredstate.NewSourceFile(file.Path(), data)
	}
	if clusterName == "" {
		t.Fatal("no cluster")
	}
	binding := map[string]any{"apiVersion": "bootwright.io/v1alpha1", "kind": "ClusterAddonBinding", "metadata": map[string]any{"name": "network-operators"}, "spec": map[string]any{"clusterRef": clusterName, "packages": map[string]any{"nmstate": map[string]any{"subscription": map[string]any{"channel": "stable", "startingCSV": "kubernetes-nmstate-operator.4.21.0"}}}}}
	data, err := yaml.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], "network-operators.yaml"), data))
	state, _ := compileAcceptance(t, sources)
	identity := lifecycle.ContextIdentity{Name: "contract-secondary-network"}
	example := contractExample{state: state, apply: lifecycle.PlanInput{Verb: reconciliation.Apply, Context: identity, State: state, Controller: "controller"}, destroy: lifecycle.PlanInput{Verb: reconciliation.Destroy, Context: identity, State: state, Controller: "controller"}, plans: map[lifecycle.CapabilityBinding]lifecycle.CapabilityPlan{}}
	definitions := []reconciliation.BlockDefinition{}
	for _, binding := range resolver.Bindings() {
		capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
		contribution, err := capability.Plan(context.Background(), example.apply)
		if err != nil {
			continue
		}
		example.plans[binding] = contribution
		definitions = append(definitions, contribution.Definitions...)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	example.plan = plan
	return example
}
