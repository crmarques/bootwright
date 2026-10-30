package lifecycle_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestLastInstanceWaitsForEverySameClusterStageConsumer(t *testing.T) {
	for _, last := range []bool{false, true} {
		pkg := addons.Package{Name: "fixture", Summary: "fixture package", Last: last, Budget: time.Minute, Render: func(addons.Instance) (addons.Rendered, error) { return addons.Rendered{}, nil }}
		registry, err := addons.NewRegistry([]addons.Package{pkg})
		if err != nil {
			t.Fatal(err)
		}
		request, requires, err := instance.Render(addons.SelectedInstance{Package: pkg, Instance: addons.Instance{Cluster: "cluster", Binding: "binding", Config: api.MapValue()}}, addons.ClusterFacts{Name: "cluster", Release: "4.21.15"}, "controller")
		if err != nil {
			t.Fatal(err)
		}
		data, err := request.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		capability := instance.New(nil, registry, nil)
		bound := []lifecycle.BoundCapability{{Binding: lifecycle.CapabilityBinding{Kind: instance.Kind, Implementation: instance.Implementation}, Capability: capability}}
		for _, reverse := range []bool{false, true} {
			storage := make([]string, 8)
			storage[0] = "existing"
			storage[1] = "untouched"
			definitions := []reconciliation.BlockDefinition{
				{ID: "tail", Stage: reconciliation.StageAddOns, Kind: instance.Kind, Implementation: instance.Implementation, Request: data, Requires: requires, Dependencies: storage[:1]},
				{ID: "synthetic-peer", Stage: reconciliation.StageAddOns, Kind: "Synthetic", Requires: requires},
				{ID: "another-cluster", Stage: reconciliation.StageAddOns, Kind: "Synthetic", Requires: []reconciliation.ObjectRef{{Kind: "OpenShiftCluster", Object: "another"}}},
				{ID: "another-stage", Stage: reconciliation.StageClusters, Kind: "Synthetic", Requires: requires},
			}
			if reverse {
				slices.Reverse(definitions)
			}
			before := make([]reconciliation.BlockDefinition, len(definitions))
			for i, definition := range definitions {
				before[i] = definition
				before[i].Dependencies = slices.Clone(definition.Dependencies)
				before[i].Requires = slices.Clone(definition.Requires)
				before[i].Request = slices.Clone(definition.Request)
			}
			planned, err := lifecycle.StageTailDependenciesForTest(bound, definitions)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(definitions, before) || storage[1] != "untouched" {
				t.Fatal("stage tail mutated its input")
			}
			for _, definition := range planned {
				if definition.ID != "tail" {
					if len(definition.Dependencies) != 0 {
						t.Fatal("changed another consumer", definition.ID)
					}
					continue
				}
				expected := []string{"existing"}
				if last {
					expected = append(expected, "synthetic-peer")
				}
				if !slices.Equal(definition.Dependencies, expected) {
					t.Fatalf("last=%v reverse=%v dependencies=%v", last, reverse, definition.Dependencies)
				}
			}
		}
	}
}
