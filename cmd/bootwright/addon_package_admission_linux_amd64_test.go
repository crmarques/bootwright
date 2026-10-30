package main

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func withBinding(t *testing.T, example, binding string) desiredstate.Sources {
	t.Helper()
	sources := exampleDirectory(t, example)
	path := filepath.Join(sources.Roots[0], "add-ons", "binding.yaml")
	index := slices.IndexFunc(sources.Files, func(f desiredstate.SourceFile) bool { return f.Path() == path })
	if index < 0 {
		t.Fatalf("the example %s has no add-ons/binding.yaml", example)
	}
	sources.Files[index] = desiredstate.NewSourceFile(path, []byte(binding))
	return sources
}

func planAddOns(t *testing.T, state *compilation.State, name string) []reconciliation.BlockDefinition {
	t.Helper()
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: state, Controller: "controller", Context: lifecycle.ContextIdentity{Name: name}}
	var definitions []reconciliation.BlockDefinition
	for _, binding := range resolver.Bindings() {
		if binding.Implementation != instance.Implementation {
			continue
		}
		capability, ok := resolver.Resolve(binding.Kind, binding.Implementation)
		if !ok {
			t.Fatal("unresolved production capability", binding)
		}
		contribution, err := capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("a configuration that validated does not plan: %+v", diagnostics.Of(err))
		}
		definitions = append(definitions, contribution.Definitions...)
	}
	return definitions
}

const nmstateBinding = "apiVersion: bootwright.io/v1alpha1\nkind: ClusterAddonBinding\nmetadata:\n  name: nmstate\n\nspec:\n  clusterRef: sno\n\n  packages:\n    nmstate:\n      timeout: %s\n\n      subscription:\n        channel: example-channel\n        startingCSV: kubernetes-nmstate-operator.v0.0.0\n"

func TestPackageTimeoutAdmission(t *testing.T) {
	for _, tc := range []struct {
		timeout string
		seconds int64
		refused string
	}{
		{timeout: "500ms", refused: "a whole number of seconds"},
		{timeout: "1500ms", refused: "a whole number of seconds"},
		{timeout: "1s", seconds: 1},
		{timeout: "1.5m", seconds: 90},
		{timeout: "5h50m", seconds: 21000},
		{timeout: "5h50m1s", refused: "at most 5h50m"},
	} {
		t.Run(tc.timeout, func(t *testing.T) {
			sources := withBinding(t, "lab-sno-nmstate", strings.Replace(nmstateBinding, "%s", tc.timeout, 1))
			state, _, err := wireCompiler().Compile(context.Background(), sources)
			if tc.refused != "" {
				found := false
				for _, d := range diagnostics.Of(err) {
					found = found || d.Code == "api.value" && d.Field == "$.spec.packages.nmstate.timeout" && strings.Contains(d.Message, tc.refused) && d.Remediation != ""
				}
				if !found {
					t.Fatalf("timeout %s was not refused at the field: %+v", tc.timeout, diagnostics.Of(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("timeout %s is refused: %+v", tc.timeout, diagnostics.Of(err))
			}
			definitions := planAddOns(t, state, "lab-sno-nmstate")
			if len(definitions) != 1 {
				t.Fatalf("got %d add-on blocks", len(definitions))
			}
			request, err := instance.DecodeRequest(definitions[0].Request)
			if err != nil || request.BudgetSeconds != tc.seconds {
				t.Fatalf("budget = %d (%v), want %d", request.BudgetSeconds, err, tc.seconds)
			}
		})
	}
}
