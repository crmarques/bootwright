//go:build linux && amd64

package main

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

// TestEveryExampleObjectIsClaimedOncePerFamily plans every example through
// this build's composition and holds it to the engine's claim rule: every
// binding names the capability family it realizes, and no two implementations
// of one family plan blocks for one object, so the engine's double-claim
// refusal never fires on a supported shape (specs/architecture.md,
// Implementation selection). The arms whose machines a bound substrate
// implementation realizes across the examples are exactly the arms
// substrate.Realized() names, so the substrate refusal table and the bindings
// cannot drift apart. Each implementation claims every effect-bearing object
// it plans a block for, and every effect-bearing object of an example is
// claimed by some bound implementation, so the engine's per-object unclaimed
// refusal never fires: an example's object is claimed, or refused by its
// domain's own table.
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
		catalog := state.Effective()
		covered := map[string]bool{}
		for _, identity := range lifecycle.Identities(productionUnsupported(t, state)) {
			covered[identity] = true
		}
		claimed := map[lifecycle.CapabilityBinding][]string{}
		familyHolder := map[string]string{}
		for _, binding := range resolver.Bindings() {
			capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
			claimed[binding] = capability.Claimed(catalog)
			for _, identity := range claimed[binding] {
				covered[identity] = true
				key := binding.Family + " " + identity
				if holder, held := familyHolder[key]; held && holder != binding.Implementation {
					t.Errorf("%s: %s is claimed by %s and %s", name, key, holder, binding.Implementation)
				}
				familyHolder[key] = binding.Implementation
			}
		}
		for _, object := range catalog.Objects() {
			if _, bears := effectBearing(object); bears && !covered[object.Identity()] {
				t.Errorf("%s: no bound implementation claims or refuses %s", name, object.Identity())
			}
		}
		planned := map[lifecycle.CapabilityBinding]map[string]bool{}
		for _, binding := range resolver.Bindings() {
			capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
			contribution, _ := capability.Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: name}, State: state, Controller: "controller"})
			planned[binding] = map[string]bool{}
			for _, definition := range contribution.Definitions {
				planned[binding][definition.Kind+"/"+definition.Object] = true
			}
		}
		for binding, identities := range claimed {
			for _, identity := range identities {
				kind, objectName, _ := strings.Cut(identity, "/")
				object, _ := catalog.Find(api.Kind(kind), objectName)
				if _, bears := effectBearing(object); !bears || planned[binding][identity] || noBlockClaims[binding.Implementation+" "+kind] != "" {
					continue
				}
				t.Errorf("%s: %s claims %s but plans no block for it and names no no-block rule", name, binding.Implementation, identity)
			}
		}
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
					if object, found := catalog.Find(api.Kind(definition.Kind), definition.Object); found {
						if _, bears := effectBearing(object); bears && !slices.Contains(claimed[binding], object.Identity()) {
							t.Errorf("%s %s: %s plans a block for %s, which it does not claim", name, verb, binding.Implementation, object.Identity())
						}
					}
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

// noBlockClaims is each implementation and kind whose claimed objects plan no
// block keyed by the object, and why; it only shrinks.
var noBlockClaims = map[string]string{
	baremetal.Implementation + " InfraProvider":      "a bare-metal provider is realized only through its Machines",
	instance.Implementation + " ClusterAddonBinding": "add-on blocks are keyed by package and cluster",
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
