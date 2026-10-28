package lifecycle

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type objectClaimer struct {
	*testCapability
	claimed []string
}

func (c objectClaimer) Claimed(api.Catalog) []string { return c.claimed }

// The engine asks every bound implementation about each selected object rather
// than trusting the kinds it binds: an object an implementation claims with no
// block of its own is admitted, one a capability refuses carries only that
// refusal, and an effect-bearing object of a bound kind that nothing claims or
// refuses is refused by identity instead of being left out of the plan.
func TestAnEffectBearingObjectNoImplementationClaimsIsRefused(t *testing.T) {
	provider := func(name string) api.Object {
		return api.NewObject(api.InfraProvider, name, api.Value{}, api.MapValue())
	}
	rack, lab, cloud := provider("rack"), provider("lab"), provider("cloud")
	bare := api.NewObject(api.Machine, "bare", api.Value{}, api.MapValue(
		api.FieldValue{Name: "os", Value: api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(false)})}))
	catalog := api.NewCatalog([]api.Object{rack, lab, cloud, bare})
	refused := RefusalOf(lab, "its arm is outside the supported shape", "remove "+lab.Identity())
	capability := objectClaimer{testCapability: &testCapability{unsupported: []Refusal{refused}}, claimed: []string{rack.Identity()}}
	resolver := testResolver{capability: capability, bindings: []CapabilityBinding{{Kind: string(api.InfraProvider), Implementation: "provider-v1"}}}
	want := SortRefusals([]Refusal{
		refused,
		RefusalOf(cloud, "no implementation of this executable claims InfraProvider/cloud", unclaimedRemediation(cloud.Identity())),
		RefusalOf(bare, "no capability of this executable realizes a Machine whose operating system is not provided", unclaimedRemediation(bare.Identity())),
	})
	if got := Unsupported(resolver, compilation.NewState(catalog, catalog, nil), domainEffects); !reflect.DeepEqual(got, want) {
		t.Fatalf("refusals = %+v, want %+v", got, want)
	}
	capability.claimed = append(capability.claimed, cloud.Identity())
	resolver.capability = capability
	if got := Unsupported(resolver, compilation.NewState(catalog, catalog, nil), domainEffects); len(got) != 2 || slices.ContainsFunc(got, func(refusal Refusal) bool { return refusal.Name == cloud.Name() }) {
		t.Fatalf("a claimed object is still refused: %+v", got)
	}
}

type countingClaimer struct {
	*testCapability
	calls *int
}

func (c countingClaimer) Claimed(catalog api.Catalog) []string {
	*c.calls++
	var claimed []string
	for _, object := range catalog.OfKind(api.InfraProvider) {
		claimed = append(claimed, object.Identity())
	}
	return claimed
}

// The unclaimed check asks each implementation once per selected graph and
// each object's domain once, so its cost grows with the input, not its square.
func TestTheUnclaimedCheckAsksEachImplementationOncePerGraph(t *testing.T) {
	const count = 4000
	objects := make([]api.Object, 0, count)
	for index := range count {
		objects = append(objects, api.NewObject(api.InfraProvider, fmt.Sprintf("p%04d", index), api.Value{}, api.MapValue()))
	}
	catalog := api.NewCatalog(objects)
	claims, bearings := 0, 0
	bearing := func(object api.Object) (Bearing, bool) {
		bearings++
		return domainEffects(object)
	}
	resolver := testResolver{capability: countingClaimer{testCapability: &testCapability{}, calls: &claims}, bindings: []CapabilityBinding{
		{Kind: string(api.InfraProvider), Implementation: "provider-v1"}}}
	if refused := Unsupported(resolver, compilation.NewState(catalog, catalog, nil), bearing); len(refused) != 0 {
		t.Fatalf("claimed providers refused: %d", len(refused))
	}
	if claims != 1 || bearings != count {
		t.Fatalf("the check asked implementations %d times and domains %d times over %d objects", claims, bearings, count)
	}
}

// Two implementations of one family that claim one object refuse it before
// registration, even when neither plans a block for it.
func TestAnObjectTwoImplementationsOfAFamilyClaimIsRefused(t *testing.T) {
	rack := api.NewObject(api.InfraProvider, "rack", api.Value{}, api.MapValue())
	catalog := api.NewCatalog([]api.Object{rack})
	resolver := testResolver{capability: objectClaimer{testCapability: &testCapability{}, claimed: []string{rack.Identity()}}, bindings: []CapabilityBinding{
		{Kind: string(api.InfraProvider), Implementation: "provider-v1"}, {Kind: string(api.InfraProvider), Implementation: "provider-v2"}}}
	got := Unsupported(resolver, compilation.NewState(catalog, catalog, nil), domainEffects)
	if len(got) != 1 || got[0].Reason != "two implementations of this executable claim InfraProvider/rack: provider-v1 and provider-v2" {
		t.Fatalf("refusals = %+v", got)
	}
}
