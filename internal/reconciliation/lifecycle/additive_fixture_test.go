package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

const addonSecret = "addon-secret"

type planningCapability struct {
	*lendingCapability
	reserves string
}

func (c *planningCapability) Plan(ctx context.Context, input PlanInput) (CapabilityPlan, error) {
	plan, err := c.testCapability.Plan(ctx, input)
	if err != nil {
		return plan, err
	}
	definitions := slices.Clone(plan.Definitions)
	var members []string
	for _, object := range input.State.Effective().Objects() {
		if object.Kind() != api.ClusterAddonBinding {
			continue
		}
		for _, member := range object.Spec().Get("packages").Fields() {
			members = append(members, member.Name)
		}
	}
	for _, member := range members {
		block := stagedDefinition("addon-"+member, reconciliation.StageAddOns)
		if member != "last" && slices.Contains(members, "last") {
			block.Dependencies = []string{"addon-last"}
		}
		definitions = append(definitions, block)
	}
	plan.Definitions = definitions
	if slices.Contains(members, c.reserves) {
		plan.Reservations = append(plan.Reservations, prerequisites.HostReservation{Context: input.Context.Name, Kind: "listener", Service: c.reserves, Keys: []string{"tcp:" + c.reserves}})
	}
	return plan, nil
}

func (c *planningCapability) Claimed(catalog api.Catalog) []string {
	claimed := c.testCapability.Claimed(catalog)
	for _, object := range catalog.OfKind(api.ClusterAddonBinding) {
		claimed = append(claimed, object.Identity())
	}
	return claimed
}

func (c *planningCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	if strings.HasPrefix(block.ID, "addon-") {
		return []string{addonSecret}, nil
	}
	return nil, nil
}

type addonCapability struct{ *planningCapability }

func (c *addonCapability) AdmitAdded(lineage reconciliation.Plan, added reconciliation.Block) error {
	if !strings.HasPrefix(added.ID, "addon-") {
		return failure("lifecycle.state", "the test capability admits only add-on instances", "")
	}
	return nil
}

func (c *addonCapability) ReadsMaterialOnlyInApply(block reconciliation.Block) bool {
	return strings.HasPrefix(block.ID, "addon-")
}

func (c *addonCapability) UnresolvedAdded(reconciliation.Verb, reconciliation.Block, json.RawMessage, []OwnedBlock) (Unresolved, bool) {
	return Unresolved{Reason: "an object is foreign", Remedy: "remove the object"}, true
}

func (c *addonCapability) Unresolved(reconciliation.Verb, reconciliation.Block, json.RawMessage) (Unresolved, bool) {
	return Unresolved{Reason: "an object is foreign", Remedy: "destroy and apply again"}, true
}

type additiveFixture struct {
	*harness
	compiler   sourcesCompiler
	capability *addonCapability
}

func addonBinding(cluster string, members ...string) api.Object {
	spec := api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue(cluster)})
	if len(members) != 0 {
		packages := make([]api.FieldValue, 0, len(members))
		for _, member := range members {
			packages = append(packages, api.FieldValue{Name: member, Value: api.MapValue()})
		}
		spec = spec.With("packages", api.MapValue(packages...))
	}
	return api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, spec)
}

func configuredBinding(member, channel string) api.Object {
	spec := api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}).With("packages", api.MapValue(
		api.FieldValue{Name: member, Value: api.MapValue(api.FieldValue{Name: "channel", Value: api.StringValue(channel)})}))
	return api.NewObject(api.ClusterAddonBinding, "binding", api.Value{}, spec)
}

func newAdditiveFixture(t *testing.T, initial ...api.Object) *additiveFixture {
	t.Helper()
	h := newHarness(t, "producer-a")
	h.capability.definitions[0].Object = "producer-a"
	h.capability.outcomeFor = map[string]Result{"producer-a": {
		Outcome: reconciliation.OutcomeChanged, Evidence: []byte(`{"ok":true}`),
		Produced: []Produced{{Name: "access", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("kubeconfig")})}},
	}}
	return installAddons(h, "producer-a", initial...)
}

func installAddons(h *harness, producer string, initial ...api.Object) *additiveFixture {
	references := map[string][]ProducedInput{}
	if producer != "" {
		for _, member := range []string{"nmstate", "metallb", "kubevirt", "last"} {
			references["addon-"+member] = []ProducedInput{{Kind: "ArtifactServer", Object: producer, Output: "access"}}
		}
	}
	planning := &planningCapability{lendingCapability: &lendingCapability{testCapability: h.capability, references: references}}
	c := &addonCapability{planning}
	h.service.capabilities = testResolver{capability: c}
	h.binder.material[addonSecret] = secrets.NewMaterial(nil)
	compiler := sourcesCompiler{states: map[string]*compilation.State{}, errs: map[string]error{}}
	compiler.states[digestOf(h.workspace.inputs)] = diffState(initial...)
	h.service.compiler = compiler
	return &additiveFixture{harness: h, compiler: compiler, capability: c}
}

func (f *additiveFixture) publish(revision string, objects ...api.Object) {
	sources := diffSources("kind: Environment\n# " + revision + "\n")
	f.compiler.states[digestOf(sources)] = diffState(objects...)
	f.update(revision, sources)
}

func (f *additiveFixture) root(t *testing.T) operationstore.Operation {
	t.Helper()
	if _, err := apply(f.harness); err != nil {
		t.Fatal(err)
	}
	operation, _ := durableOperation(t, f.harness)
	return operation
}

func (f *additiveFixture) refusedWith(t *testing.T, fragments ...string) {
	t.Helper()
	before, _ := durableOperation(t, f.harness)
	_, err := apply(f.harness)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" {
		t.Fatalf("the apply reported %+v (%v), want one lifecycle.state refusal", reported, err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(reported[0].Message, fragment) {
			t.Errorf("the refusal %q does not name %q", reported[0].Message, fragment)
		}
	}
	if after, _ := durableOperation(t, f.harness); after.ID != before.ID || after.State != before.State {
		t.Errorf("a refused apply moved the current operation from %s to %s", before.ID, after.ID)
	}
}

func headPlan(t *testing.T, h *harness) (operationstore.Operation, reconciliation.Plan) {
	t.Helper()
	operation, _ := durableOperation(t, h)
	plan, err := lineageReader(h).ReadPlan(context.Background(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return operation, plan
}
