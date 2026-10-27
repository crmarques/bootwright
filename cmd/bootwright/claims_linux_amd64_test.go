//go:build linux && amd64

package main

import (
	"context"
	"maps"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// TestEveryExampleObjectIsClaimedOncePerFamily plans every example through
// this build's composition and holds it to the engine's claim rule: every
// binding names the capability family it realizes, and no two implementations
// of one family plan blocks for one object, so the engine's double-claim
// refusal never fires on a supported shape (specs/architecture.md,
// Implementation selection). The arms whose machines a bound substrate
// implementation realizes across the examples are exactly the arms
// substrate.Realized() names, so the substrate refusal table and the bindings
// cannot drift apart.
func TestEveryExampleObjectIsClaimedOncePerFamily(t *testing.T) {
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	bound := map[string]bool{}
	for _, binding := range resolver.Bindings() {
		if binding.Family == "" {
			t.Errorf("%s/%s names no capability family", binding.Kind, binding.Implementation)
		}
	}
	names := []string{}
	for name := range exampleDirectories(t) {
		if name != refusedExample {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		state, _ := compileAcceptance(t, exampleDirectory(t, name))
		for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
			input := lifecycle.PlanInput{Verb: verb, Context: lifecycle.ContextIdentity{Name: name}, State: state, Controller: "controller"}
			holders := map[string]string{}
			for _, binding := range resolver.Bindings() {
				capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
				contribution, err := capability.Plan(context.Background(), input)
				if err != nil {
					continue
				}
				for _, definition := range contribution.Definitions {
					key := binding.Family + " " + definition.Kind + "/" + definition.Object
					if holder, held := holders[key]; held && holder != binding.Implementation {
						t.Errorf("%s %s: %s is claimed by %s and %s", name, verb, key, holder, binding.Implementation)
					}
					holders[key] = binding.Implementation
					if binding.Family == substrate.MachineFamily && definition.Kind == string(api.Machine) {
						bound[substrateArm(t, state.Effective(), definition.Object)] = true
					}
				}
			}
		}
	}
	if arms := slices.Sorted(maps.Keys(bound)); !slices.Equal(arms, slices.Sorted(slices.Values(substrate.Realized()))) {
		t.Errorf("substrate.Realized() names %v, but the examples' machines are realized on %v", substrate.Realized(), arms)
	}
}

// substrateArm is the arm of the provider one Machine is placed on.
func substrateArm(t *testing.T, catalog api.Catalog, name string) string {
	t.Helper()
	object, found := catalog.Find(api.Machine, name)
	if !found {
		t.Fatalf("a substrate machine block names Machine/%s, which the example does not declare", name)
	}
	provider, found := machine.Provider(object, catalog)
	if !found {
		t.Fatalf("Machine/%s names no InfraProvider", name)
	}
	return substrate.Variant(provider)
}
