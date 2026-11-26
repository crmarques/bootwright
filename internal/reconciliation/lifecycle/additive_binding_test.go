package lifecycle

import (
	"context"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// additiveBindingFixture applies the root and then an additive apply whose
// added instance names addonSecret, tracking each Secret's versions.
func additiveBindingFixture(t *testing.T, initial []api.Object, next ...api.Object) *additiveFixture {
	t.Helper()
	f := newAdditiveFixture(t, initial...)
	f.binder.versions = map[string]string{addonSecret: "addon-1"}
	f.binder.versionMaterial = map[string]secrets.Material{"addon-1": password("first"), "addon-2": password("second")}
	return f
}

func heldUnder(holds custody.Holds, binding, secret string) bool {
	return slices.ContainsFunc(holds.Bindings, func(hold custody.Hold) bool {
		return hold.Binding == binding && (hold.All || slices.Contains(hold.Secrets, secret))
	})
}

func addedHead(t *testing.T, f *additiveFixture) (root, head string) {
	t.Helper()
	if result, err := apply(f.harness); err != nil || result.Receipt.State != "done" {
		t.Fatalf("the additive apply: %+v (%v)", result, diagnostics.Of(err))
	}
	operation, _ := durableOperation(t, f.harness)
	predecessor, err := lineageReader(f.harness).ReadOperation(context.Background(), operation.Predecessor)
	if operation.Predecessor == "" || len(operation.Bindings) != 1 || err != nil {
		t.Fatalf("the apply was no addition with a binding of its own: %+v (%v)", operation, err)
	}
	return firstBinding(predecessor.Bindings), operation.Bindings[0]
}

// B1870: a whole destroy presents the added instance the binding of the
// additive apply that created it, which alone pins the Secret the root never
// bound, and the holds keep that Secret under it.
func TestAWholeDestroyPresentsAnAddedStepTheSecretOnlyItsAdditiveApplyBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := additiveBindingFixture(t, nil)
	f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"), secretObject(addonSecret, "file:one"))
	root, head := addedHead(t, f)
	if _, pinned := f.binder.pins[root][addonSecret]; pinned || f.binder.pins[head][addonSecret] != "addon-1" {
		t.Fatalf("the root's binding pins %v and the addition's %v", f.binder.pins[root], f.binder.pins[head])
	}
	holds, err := f.service.SecretHolds(ctx, testContextName)
	if err != nil || !heldUnder(holds, head, addonSecret) || heldUnder(holds, root, addonSecret) {
		t.Fatalf("the holds are %+v (%v), want the added Secret held under the addition's binding alone", holds, err)
	}
	seen := presentedSecret(f.harness, addonSecret)
	if err := destroyWith(f.harness); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["addon-nmstate"], []string{"first"}) || !slices.Equal(got["producer-a"], []string{""}) {
		t.Fatalf("the whole destroy presented %v, want the added step its addition's Secret and the root's step none", got)
	}
	if !slices.Contains(f.binder.released, head) || !slices.Contains(f.binder.released, root) {
		t.Fatalf("the whole destroy released %v", f.binder.released)
	}
}

// B1870: a Secret rotated after the root apply is bound at its new version by
// the additive apply whose added step names it, so a whole destroy presents
// the added step that version and the root's step the one the root bound, and
// the holds keep the new version under the addition's binding while the root's
// access credential is not held.
func TestAWholeDestroyPresentsAnAddedStepTheVersionItsAdditiveApplyBound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := additiveBindingFixture(t, []api.Object{secretObject(addonSecret, "file:one")})
	f.capability.secrets = append(f.capability.secrets, addonSecret)
	f.capability.access = map[string][]string{"producer-a": {addonSecret}}
	f.root(t)
	f.binder.versions[addonSecret] = "addon-2"
	f.publish("rev-2", addonBinding("cluster", "nmstate"), secretObject(addonSecret, "file:one"))
	root, head := addedHead(t, f)
	if f.binder.pins[root][addonSecret] != "addon-1" || f.binder.pins[head][addonSecret] != "addon-2" {
		t.Fatalf("the root's binding pins %v and the addition's %v", f.binder.pins[root], f.binder.pins[head])
	}
	holds, err := f.service.SecretHolds(ctx, testContextName)
	if err != nil || !heldUnder(holds, head, addonSecret) || heldUnder(holds, root, addonSecret) {
		t.Fatalf("the holds are %+v (%v), want the rotated Secret held under the addition's binding alone", holds, err)
	}
	seen := presentedSecret(f.harness, addonSecret)
	if err := destroyWith(f.harness); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["addon-nmstate"], []string{"second"}) || !slices.Equal(got["producer-a"], []string{"first"}) {
		t.Fatalf("the whole destroy presented %v, want each step the version its apply bound", got)
	}
	if removal, _ := durableOperation(t, f.harness); removal.Verb != reconciliation.Destroy || slices.Contains(removal.Bindings, head) || !slices.Contains(f.binder.released, head) {
		t.Fatalf("the removal recorded %+v and released %v, want the addition's binding released but not frozen, since its step reads no material at removal", removal, f.binder.released)
	}
}

// B1870: an added instance reads bound material only in its own apply
// attempts, so a lost additive binding blocks neither status nor the whole
// removal: the step is presented nothing, the holds still name the binding,
// and the removal completes and releases every binding.
func TestALostAdditiveBindingLeavesAStepThatReadsNoMaterialAtRemovalRemovable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := additiveBindingFixture(t, nil)
	f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"), secretObject(addonSecret, "file:one"))
	root, head := addedHead(t, f)
	f.binder.lost = []string{head}
	if holds, err := f.service.SecretHolds(ctx, testContextName); err != nil || !heldUnder(holds, head, addonSecret) {
		t.Fatalf("the holds are %+v (%v), want the added Secret still held under the lost binding", holds, err)
	}
	status, err := f.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if err != nil || slices.ContainsFunc(status.Contradictions, func(line string) bool { return strings.Contains(line, head) }) {
		t.Fatalf("status reported %+v (%v), want no lost binding", status.Contradictions, err)
	}
	seen := presentedSecret(f.harness, addonSecret)
	if err := destroyWith(f.harness); err != nil {
		t.Fatalf("the whole destroy over the lost additive binding: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["addon-nmstate"], []string{""}) {
		t.Fatalf("the whole destroy presented %v, want the added step nothing", got)
	}
	if !slices.Contains(f.binder.released, root) {
		t.Fatalf("the whole destroy released %v", f.binder.released)
	}
	removal, _ := durableOperation(t, f.harness)
	log := string(f.workspace.logArea.files[operationstore.OperationLogPath(removal.ID)])
	if !strings.Contains(log, `"event":"binding-unreopened"`) || !strings.Contains(log, "Secret binding "+head+" could not be reopened") {
		t.Fatalf("the removal's log %q does not warn that the additive binding %s could not be reopened", log, head)
	}
	if strings.Contains(log, "Secret binding "+root+" could not be reopened") {
		t.Fatalf("the removal's log %q warns about the root's binding %s, which reopened", log, root)
	}
}

// B1870: an added step that may read bound material at removal keeps the
// lost-binding refusal, naming the additive apply's binding and only that step.
func TestALostAdditiveBindingRefusesTheRemovalOfAStepThatReadsMaterialAtRemoval(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "producer-a")
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	f.complete(t)
	h.binder.lost = []string{f.head.Bindings[0]}
	reported := diagnostics.Of(destroyWith(h))
	if len(reported) == 0 || reported[0].Code != engineBindingLost.Code || !strings.Contains(reported[0].Message, "binding "+f.head.Bindings[0]+",") ||
		!strings.HasSuffix(reported[0].Message, "owns: ArtifactServer/consumer") {
		t.Fatalf("a destroy over the addition's lost binding returned %+v", reported)
	}
}

// X769: a rebind that names a Secret only an apply-only step's binding pins is
// refused saying no step of the removal reads it at removal, not by listing
// the bindings the removal reopens, which the Secret was never in.
func TestARebindOfASecretOnlyAnApplyOnlyStepsBindingPinsSaysNoStepReadsItAtRemoval(t *testing.T) {
	t.Parallel()
	f := additiveBindingFixture(t, nil)
	f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"), secretObject(addonSecret, "file:one"))
	root, head := addedHead(t, f)
	f.binder.versions[addonSecret] = "addon-2"
	before := slices.Clone(f.capability.destroys)
	reported := diagnostics.Of(destroyWith(f.harness, RebindAuthorization+addonSecret))
	if len(reported) != 1 || reported[0].Code != authorizeRebindInvalid.Code || !strings.Contains(reported[0].Message, "no step of this destroy reads Secret "+addonSecret+" at removal") ||
		strings.Contains(reported[0].Message, root) || strings.Contains(reported[0].Message, head) {
		t.Fatalf("the rebind returned %+v, want no step of the removal reads the Secret at removal", reported)
	}
	if !slices.Equal(f.capability.destroys, before) {
		t.Fatalf("a refused rebind destroyed %v", f.capability.destroys)
	}
}

// materialGatedAddon answers a removal's observation of an add-on instance the
// way an adapter that cannot read its apply's Secret must: completed when the
// removal lends it the Secret, unknown when it does not.
type materialGatedAddon struct {
	*addonCapability
	mutex sync.Mutex
	lent  map[string][]bool
}

func (c *materialGatedAddon) ObserveRemoval(ctx context.Context, execution Execution) (Observation, error) {
	_, held := execution.Material[addonSecret]
	c.mutex.Lock()
	c.lent[execution.Block.ID] = append(c.lent[execution.Block.ID], held)
	c.mutex.Unlock()
	if strings.HasPrefix(execution.Block.ID, "addon-") && !held {
		return Observation{Effect: reconciliation.EffectUnknown}, nil
	}
	return Observation{Effect: reconciliation.EffectCompleted}, nil
}

// X769: a whole destroy resolving an add-on instance its additive apply left
// unproved observes it through ObserveRemoval, lent the additive binding's
// Secret. When that binding is lost the observation is lent nothing and the
// instance stays unproved, so the removal refuses before any effect instead of
// reading completed over missing material.
func TestADestroyResolvingAnUnprovedApplyOnlyStepOverALostBindingResolvesFailSafe(t *testing.T) {
	t.Parallel()
	for name, lost := range map[string]bool{"the binding stands": false, "the binding is lost": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := additiveBindingFixture(t, nil)
			f.root(t)
			f.publish("rev-2", addonBinding("cluster", "nmstate"), secretObject(addonSecret, "file:one"))
			_, head := addedHead(t, f)
			gated := &materialGatedAddon{addonCapability: f.capability, lent: map[string][]bool{}}
			f.service.capabilities = testResolver{capability: gated}
			operation, _ := durableOperation(t, f.harness)
			rewriteState(t, f.harness, path.Join(operation.ID, "blocks", "addon-nmstate", "state.json"), string(reconciliation.BlockUnknown))
			if lost {
				f.binder.lost = []string{head}
			}
			err := destroyWith(f.harness)
			reads := gated.lent["addon-nmstate"]
			if !lost {
				if err != nil || !slices.Equal(reads, []bool{true}) {
					t.Fatalf("the destroy over a standing binding returned %v and observed the instance lent %v", diagnostics.Of(err), reads)
				}
				return
			}
			reported := diagnostics.Of(err)
			if len(reported) == 0 || !unprovedCode(reported[0].Code) || !slices.Equal(reads, []bool{false}) {
				t.Fatalf("the destroy over a lost binding returned %+v and observed the instance lent %v", reported, reads)
			}
			if len(f.capability.destroys) != 0 || len(f.binder.released) != 0 {
				t.Fatalf("a refused removal destroyed %v and released %v", f.capability.destroys, f.binder.released)
			}
			if current, _ := durableOperation(t, f.harness); current.ID != operation.ID || current.Verb != reconciliation.Apply {
				t.Fatalf("the context holds %+v, want the additive apply untouched", current)
			}
		})
	}
}
