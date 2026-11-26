package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// presentedSecret records, per step, the version of one Secret each attempt
// or removal was lent.
func presentedSecret(h *harness, secret string) func() map[string][]string {
	var mutex sync.Mutex
	seen := map[string][]string{}
	h.capability.presenting = func(execution Execution) bool {
		value, _ := execution.Material[secret].Part(secrets.PasswordPart)
		mutex.Lock()
		seen[execution.Block.ID] = append(seen[execution.Block.ID], string(value))
		mutex.Unlock()
		return true
	}
	return func() map[string][]string {
		mutex.Lock()
		defer mutex.Unlock()
		taken := seen
		seen = map[string][]string{}
		return taken
	}
}

// presentedVersions records, per step, the version of one more Secret each
// attempt or removal was lent, beside what presentedSecret records.
func presentedVersions(h *harness, secret string) func() map[string][]string {
	var mutex sync.Mutex
	seen := map[string][]string{}
	earlier := h.capability.presenting
	h.capability.presenting = func(execution Execution) bool {
		value, _ := execution.Material[secret].Part(secrets.PasswordPart)
		mutex.Lock()
		seen[execution.Block.ID] = append(seen[execution.Block.ID], string(value))
		mutex.Unlock()
		return earlier == nil || earlier(execution)
	}
	return func() map[string][]string {
		mutex.Lock()
		defer mutex.Unlock()
		return seen
	}
}

// restoreBindingHarness plans a kept base step and a cluster step a staged
// destroy at clusters takes back. Both name the BMC credential; the base
// step also names the effect Secret keep and the cluster step pull.
func restoreBindingHarness(t *testing.T) *harness {
	t.Helper()
	base, cluster := stagedDefinition("base", reconciliation.StageInfrastructureServices), stagedDefinition("cluster", reconciliation.StageClusters, "base")
	base.Request = json.RawMessage(`{"credentialsRef":"bmc","keepRef":"keep","name":"base"}`)
	cluster.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"cluster","secretRef":"pull"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{base, cluster})
	h.capability.secrets = []string{"bmc", "keep", "pull"}
	h.capability.access = map[string][]string{"base": {"bmc"}, "cluster": {"bmc"}}
	h.binder.material = map[string]secrets.Material{"bmc": password("first"), "keep": password("keep"), "pull": password("pull")}
	h.binder.versions = map[string]string{"bmc": "ver-1", "keep": "keep-1", "pull": "pull-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second"), "keep-1": password("keep"), "pull-1": password("pull"), "pull-2": password("pull")}
	return h
}

func holdsSecret(holds custody.Holds, secret string) bool {
	return slices.ContainsFunc(holds.Bindings, func(hold custody.Hold) bool { return hold.All || slices.Contains(hold.Secrets, secret) })
}

// B1868: while trimmed, the holds keep only what a kept step's binding pins,
// so pull, which only the taken-back step used, renews; the restore then
// binds the current versions of what its added step names in a binding of its
// own, so the re-created step is presented the rotated credential while the
// kept step keeps the version its apply bound; and a whole destroy after the
// restore presents each step the binding of the apply that created it and
// releases both.
func TestARestoreBindsTheCurrentVersionsOfWhatItsAddedStepsName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := restoreBindingHarness(t)
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	holds, err := h.service.SecretHolds(ctx, testContextName)
	if err != nil || holdsSecret(holds, "pull") || !holdsSecret(holds, "keep") {
		t.Fatalf("the trimmed context holds %+v (%v), want keep held and pull renewable", holds, err)
	}
	h.binder.versions["bmc"], h.binder.versions["pull"] = "ver-2", "pull-2"
	seen := presentedSecret(h, "bmc")
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	if len(restoring.Bindings) != 1 || slices.Contains(applied.Bindings, restoring.Bindings[0]) {
		t.Fatalf("the restore recorded %v beside the apply's %v, want a binding of its own", restoring.Bindings, applied.Bindings)
	}
	if pinned := h.binder.pins[restoring.Bindings[0]]; pinned["bmc"] != "ver-2" || pinned["pull"] != "pull-2" || pinned["keep"] != "" {
		t.Fatalf("the restore's binding pins %v, want the current bmc and pull and nothing of the kept step", pinned)
	}
	if got := seen(); !slices.Equal(got["cluster"], []string{"second"}) || len(got["base"]) != 0 {
		t.Fatalf("the restore presented %v, want the rotated version to the re-created step alone", got)
	}
	if holds, err := h.service.SecretHolds(ctx, testContextName); err != nil || !holdsSecret(holds, "pull") || !holdsSecret(holds, "keep") {
		t.Fatalf("the restored context holds %+v (%v), want pull and keep held", holds, err)
	}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy over the restore: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["cluster"], []string{"second"}) || !slices.Equal(got["base"], []string{"first"}) {
		t.Fatalf("the whole destroy presented %v, want each step its creator's version", got)
	}
	for _, binding := range append(slices.Clone(applied.Bindings), restoring.Bindings...) {
		if !slices.Contains(h.binder.released, binding) {
			t.Fatalf("the whole destroy released %v, want every member's binding", h.binder.released)
		}
	}
}

// B1868: a Secret no binding of the lineage pins, which a changed input's
// added step names, is bound at its current version by the restore itself.
func TestARestoreBindsASecretNoLineageBindingPins(t *testing.T) {
	t.Parallel()
	base, cluster := stagedDefinition("base", reconciliation.StageInfrastructureServices), stagedDefinition("cluster", reconciliation.StageClusters, "base")
	cluster.Request = json.RawMessage(`{"name":"cluster","secretRef":"pull"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{base, cluster})
	h.capability.secrets = []string{"pull"}
	h.binder.material = map[string]secrets.Material{"pull": password("pull"), "token": password("token")}
	h.binder.versions = map[string]string{"pull": "pull-1", "token": "token-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"pull-1": password("pull"), "token-1": password("token")}
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.capability.definitions[1].Request = json.RawMessage(`{"name":"cluster","secretRef":"pull","tokenRef":"token"}`)
	h.capability.secrets = []string{"pull", "token"}
	seen := presentedSecret(h, "token")
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	if pinned := h.binder.pins[firstBinding(restoring.Bindings)]; pinned["token"] != "token-1" {
		t.Fatalf("the restore's binding pins %v, want token", pinned)
	}
	if got := seen(); !slices.Equal(got["cluster"], []string{"token"}) {
		t.Fatalf("the restore presented %v", got)
	}
}

// B1868: a rebind during a whole destroy over a restored lineage replaces the
// binding of the member whose step presents the rotated credential, the
// restore's alone, records the replacement, and presents the new version to
// that step while the kept step keeps its own.
func TestARebindOverARestoredLineageReplacesThePresentingMembersBinding(t *testing.T) {
	t.Parallel()
	h := restoreBindingHarness(t)
	h.capability.definitions[0].Request = json.RawMessage(`{"keepRef":"keep","name":"base"}`)
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	h.binder.versions["bmc"] = "ver-2"
	seen := presentedSecret(h, "keep")
	rebound := presentedVersions(h, "bmc")
	if err := destroyWith(h, RebindAuthorization+"bmc"); err != nil {
		t.Fatalf("the rebound whole destroy: %+v", diagnostics.Of(err))
	}
	if len(h.binder.rebinds) != 1 || h.binder.rebinds[0].BindingID != restoring.Bindings[0] {
		t.Fatalf("the destroy rebound %+v, want the restore's binding %v alone", h.binder.rebinds, restoring.Bindings)
	}
	removal, _ := currentRecord(t, h)
	issued := removal.Rebound[restoring.Bindings[0]]
	if issued == "" || len(removal.Rebound) != 1 || !slices.Contains(removal.Bindings, issued) || !slices.Contains(removal.Bindings, applied.Bindings[0]) {
		t.Fatalf("the removal recorded %v and %v", removal.Bindings, removal.Rebound)
	}
	if got := seen(); !slices.Equal(got["base"], []string{"keep"}) {
		t.Fatalf("the removal presented %v, want the kept step its apply's keep", got)
	}
	if got := rebound(); !slices.Equal(got["cluster"], []string{"second"}) {
		t.Fatalf("the removal presented bmc %v, want the cluster step the rebound version", got)
	}
	if pinned := h.binder.pins[issued]; pinned["bmc"] != "ver-2" {
		t.Fatalf("the rebound binding pins %v", pinned)
	}
}

// B1868: a whole destroy over a restored lineage whose restore binding is
// lost refuses engine.binding-lost naming that binding and only the objects
// its steps hold; status names the same.
func TestALostRestoreBindingNamesItselfAndWhatItPresents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := restoreBindingHarness(t)
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	h.binder.lost = []string{restoring.Bindings[0]}
	err := destroyWith(h)
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != engineBindingLost.Code || !strings.Contains(reported[0].Message, "binding "+restoring.Bindings[0]+",") ||
		strings.Contains(reported[0].Message, applied.Bindings[0]) || !strings.HasSuffix(reported[0].Message, "owns: ArtifactServer/cluster") {
		t.Fatalf("the destroy over a lost restore binding returned %+v", reported)
	}
	status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if err != nil || !slices.ContainsFunc(status.Contradictions, func(line string) bool { return strings.Contains(line, restoring.Bindings[0]) }) {
		t.Fatalf("status reported %+v (%v)", status.Contradictions, err)
	}
}

// B1868: a rebind whose presenting step a trimmed context took back is
// refused by a fresh restore, which rebinds nothing because it binds every
// Secret its added steps name at its current version.
func TestARestoreRefusesARebind(t *testing.T) {
	t.Parallel()
	h := restoreBindingHarness(t)
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.binder.versions["bmc"] = "ver-2"
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}})
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != authorizeRebindInvalid.Code || !strings.Contains(reported[0].Message, "binds the current version") {
		t.Fatalf("a restore with a rebind returned %+v", reported)
	}
}
