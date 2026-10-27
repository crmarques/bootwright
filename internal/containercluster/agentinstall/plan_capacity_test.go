package agentinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

// This tests the state envelope independently of the provider's run ceiling:
// forty-eight physical nodes need more rounds than today's call budgets allow.
func TestFortyEightPhysicalNodesFitThePlanStateEnvelope(t *testing.T) {
	objects := append(base(), metalProvider())
	nodes := make([]api.Value, 0, 48)
	for index := range 48 {
		name := fmt.Sprintf("metal-%02d", index)
		objects = append(objects, server(name, fmt.Sprintf("198.51.100.%d/24", 40+index), fmt.Sprintf("02:00:00:00:00:%02x", index)))
		role := "worker"
		if index < 3 {
			role = "controlPlane"
		}
		nodes = append(nodes, node(name, role, name, name+".metal.lab.example.test"))
	}
	declared, _ := physicalCatalog().Find(api.OpenShiftCluster, "metal")
	declared = declared.WithSpec(declared.Spec().With("nodes", api.ListValue(nodes...)))
	objects = append(objects, declared)
	catalog := api.NewCatalog(objects)
	state := compilation.NewState(catalog, catalog, nil)
	machines, err := baremetal.NewMachine(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext}})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := nodeProjections(catalog, declared, testContext, "controller", &Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	media, install, _ := onlyRequests(t, physicalCatalog())
	media.AgentConfig, err = agentConfig(declared, projected, nil)
	if err != nil {
		t.Fatal(err)
	}
	media.InstallConfig, err = installConfig(catalog, declared, projected)
	if err != nil {
		t.Fatal(err)
	}
	install.Nodes = frozenNodes(projected)
	mediaBytes, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	installBytes, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	definitions := machines.Definitions
	for _, block := range []struct {
		id      string
		request []byte
		impacts []string
		groups  []reconciliation.Group
	}{
		{"cluster-media-metal", mediaBytes, mediaImpacts(reconciliation.Apply, media), mediaGroups(reconciliation.Apply, media)},
		{"cluster-install-metal", installBytes, installImpacts(reconciliation.Apply, install), installGroups(reconciliation.Apply, install)},
		{"cluster-network-metal", []byte(`{"objects":[]}`), []string{"create-cluster-network"}, []reconciliation.Group{{ID: "apply-network", Description: "apply the cluster network", Machines: install.Machines()}}},
	} {
		definitions = append(definitions, reconciliation.BlockDefinition{ID: block.id, Description: "realize the cluster", Kind: "OpenShiftCluster", Object: "metal", Implementation: InstallImplementation, ContentDigest: InstallContentDigest(), Stage: reconciliation.StageClusters, Request: block.request, Impacts: block.impacts, Groups: block.groups})
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blocks) != 51 {
		t.Fatalf("planned %d blocks", len(plan.Blocks))
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []reconciliation.Plan{plan, inverse} {
		encoded, err := json.Marshal(candidate)
		if err != nil || len(encoded) > operationstore.MaxPlanBytes {
			t.Fatalf("stored plan %d bytes: %v", len(encoded), err)
		}
	}
}
