package agentinstall

import (
	"context"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/material"
)

func TestTwoClusterPlanSerializesInstallationsOnlyGolden(t *testing.T) {
	objects := singleNodeCatalog().Objects()
	objects = append(objects, guest("other-01", "198.51.100.22/24"), cluster("other",
		installSelection(endpoints("198.51.100.22", "198.51.100.22", "198.51.100.22", "node")),
		node("master-0", "controlPlane", "other-01", "master-0.other.lab.example.test")))
	catalog := api.NewCatalog(objects)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext}}
	type blockView struct {
		Exclusive []string `json:"exclusive"`
		ID        string   `json:"id"`
	}
	var blocks []blockView
	for _, capability := range []interface {
		Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error)
	}{
		NewMedia(nil, material.AuthorizedKey), NewInstall(nil),
	} {
		plan, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		for _, block := range plan.Definitions {
			if block.Implementation == InstallImplementation && !slices.Equal(block.Exclusive, []string{"container-cluster-install"}) {
				t.Fatal("installation does not hold context key", block)
			}
			if block.Implementation == MediaImplementation && len(block.Exclusive) != 0 {
				t.Fatal("media build is serialized", block)
			}
			blocks = append(blocks, blockView{block.Exclusive, block.ID})
		}
	}
	if len(blocks) != 4 {
		t.Fatal("two clusters did not contribute four blocks", blocks)
	}
	canonical, err := reconciliation.Freeze(struct {
		Blocks []blockView `json:"blocks"`
	}{blocks}, "cluster schedule")
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "plan-two-clusters", canonical)
}
