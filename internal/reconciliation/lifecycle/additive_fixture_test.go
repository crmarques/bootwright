package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	addonSecret  = "addon-secret"
	signOnSecret = "sign-on-secret"
)

type planningCapability struct {
	*lendingCapability
	reserves string
	// admitFault fails the admission of an add-on block with a plain error.
	admitFault error
	planned    func()
}

func (c *planningCapability) Plan(ctx context.Context, input PlanInput) (CapabilityPlan, error) {
	if c.planned != nil {
		c.planned()
	}
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
	for _, object := range input.State.Effective().OfKind(api.CephSignOn) {
		definitions = append(definitions, stagedDefinition("signon-"+object.Name(), reconciliation.StageAddOns, "producer-a"))
	}
	plan.Definitions = definitions
	if slices.Contains(members, c.reserves) {
		plan.Reservations = append(plan.Reservations, infrastructureservices.HostReservation{Context: input.Context.Name, Kind: "listener", Service: c.reserves, Keys: []string{"tcp:" + c.reserves}})
	}
	return plan, nil
}

func (c *planningCapability) Claimed(catalog api.Catalog) []string {
	claimed := c.testCapability.Claimed(catalog)
	for _, kind := range []api.Kind{api.ClusterAddonBinding, api.CephSignOn, api.OIDCProvider} {
		for _, object := range catalog.OfKind(kind) {
			claimed = append(claimed, object.Identity())
		}
	}
	return claimed
}

func (c *planningCapability) SecretReferences(block reconciliation.Block) ([]string, error) {
	if strings.HasPrefix(block.ID, "addon-") {
		return []string{addonSecret}, nil
	}
	if strings.HasPrefix(block.ID, "signon-") {
		return []string{signOnSecret}, nil
	}
	return c.testCapability.SecretReferences(block)
}

type addonCapability struct{ *planningCapability }

func (c *addonCapability) AdmitAdded(lineage reconciliation.Plan, added reconciliation.Block) error {
	if c.admitFault != nil {
		return c.admitFault
	}
	if !strings.HasPrefix(added.ID, "addon-") && !strings.HasPrefix(added.ID, "signon-") {
		return failure("lifecycle.state", "the test capability admits only add-on instances and sign-ons", "")
	}
	return nil
}

func (c *addonCapability) AdmitsAddedObjects(after api.Catalog, added []string) []string {
	var admitted, providers []string
	for _, identity := range added {
		if kind, name, _ := strings.Cut(identity, "/"); kind == string(api.CephSignOn) {
			object, _ := after.Find(api.CephSignOn, name)
			admitted = append(admitted, identity)
			providers = append(providers, string(api.OIDCProvider)+"/"+object.Spec().Get("oidcProviderRef").Text())
		}
	}
	for _, identity := range added {
		if slices.Contains(providers, identity) {
			admitted = append(admitted, identity)
		}
	}
	return admitted
}

func (c *addonCapability) ReadsMaterialOnlyInApply(block reconciliation.Block) bool {
	return strings.HasPrefix(block.ID, "addon-") || strings.HasPrefix(block.ID, "signon-")
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

func signOnOf(name, provider string) api.Object {
	return api.NewObject(api.CephSignOn, name, api.Value{}, api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")},
		api.FieldValue{Name: "oidcProviderRef", Value: api.StringValue(provider)}))
}

func providerOf(name, issuer string) api.Object {
	return api.NewObject(api.OIDCProvider, name, api.Value{}, api.MapValue(api.FieldValue{Name: "issuerURL", Value: api.StringValue(issuer)}))
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
	h.binder.material[signOnSecret] = secrets.NewMaterial(nil)
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
	if len(reported) != 1 || !refusedBeforeEffect(reported[0].Code) {
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

// refusedBeforeEffect is whether a code is a refusal that made no effect: a
// declared stop of certainty none or owed.
func refusedBeforeEffect(code string) bool {
	for _, stop := range slices.Concat(Stops(), operationstore.Stops(), []diagnostics.Stop{testRefusal}) {
		if stop.Code == code {
			return stop.Certainty == diagnostics.CertaintyNone || stop.Certainty == diagnostics.CertaintyOwed
		}
	}
	return false
}

const anyUnproved = "<unproved>"

var testUnproved = diagnostics.Stop{Code: "test.unproved", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyUnproved, Cause: diagnostics.CauseUnclassified,
	Recovery: diagnostics.RecoveryRepeat, Meaning: "a test adapter lost its outcome", Remedy: "repeat the operation"}

var testPossible = diagnostics.Stop{Code: "test.possible", Level: diagnostics.LevelAdapterRun, Certainty: diagnostics.CertaintyPossible, Cause: diagnostics.CauseTarget,
	Recovery: diagnostics.RecoveryRepeat, Meaning: "a test adapter refused within its boundary", Remedy: "repeat the operation"}

var testRefusal = diagnostics.Stop{Code: "test.refusal", Level: diagnostics.LevelOperation, Certainty: diagnostics.CertaintyNone, Cause: diagnostics.CauseInput,
	Recovery: diagnostics.RecoveryChangeInput, Meaning: "a test double refused before any effect", Remedy: "change the input"}

func codeIs(got, want string) bool {
	return got == want || want == anyUnproved && unprovedCode(got)
}

// unprovedCode is whether a code leaves an outcome unproved: a declared stop of
// certainty unproved.
func unprovedCode(code string) bool {
	for _, stop := range Stops() {
		if stop.Code == code {
			return stop.Certainty == diagnostics.CertaintyUnproved
		}
	}
	return false
}

// authorizationRefusal is whether a code is a declared authorize stop.
func authorizationRefusal(code string) bool {
	return strings.HasPrefix(code, "authorize.")
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
