package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// reboundAdminHarness applies a monitoring step a staged destroy at clusters
// keeps, which binds Grafana's password and names the BMC credential, and a
// step of the same cluster it takes back, which names the BMC credential too.
func reboundAdminHarness(t *testing.T) *harness {
	t.Helper()
	monitoring := stagedDefinition("edge-monitoring", reconciliation.StageInfrastructureServices)
	monitoring.Kind, monitoring.Object = string(api.CephCluster), "edge"
	monitoring.Request = json.RawMessage(`{"credentialsRef":"bmc","grafanaRef":"` + grafanaSecret + `","name":"edge-monitoring"}`)
	cluster := stagedDefinition("cluster", reconciliation.StageClusters)
	cluster.Kind, cluster.Object = string(api.CephCluster), "edge"
	cluster.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"cluster"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{monitoring, cluster})
	h.capability.secrets = []string{"bmc", grafanaSecret}
	h.capability.access = map[string][]string{"edge-monitoring": {"bmc"}, "cluster": {"bmc"}}
	h.service.options.PasswordMatches = func(password, hash []byte) bool { return true }
	old := h.service.compiler.(testCompiler).state.Effective()
	catalog := api.NewCatalog(append(old.Objects(), api.NewObject(api.CephCluster, "edge", api.Value{}, api.MapValue()), grafanaDeclaration))
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
	capability := &credentialCapability{testCapability: h.capability, logins: map[string][]AdminLogin{"edge-monitoring": {grafanaLogin}, "cluster": nil}, failing: map[string]bool{}}
	h.service.capabilities = testResolver{capability: capability, bindings: []CapabilityBinding{{Kind: monitoring.Kind, Implementation: monitoring.Implementation}}}
	h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	h.binder.versions = map[string]string{"bmc": "ver-1", grafanaSecret: grafanaSecret + "-v1"}
	h.binder.versionMaterial = map[string]secrets.Material{
		"ver-1": password("first"), "ver-2": password("second"),
		grafanaSecret + "-v1": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("grafana-bound-password")}),
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("the apply: %+v", diagnostics.Of(err))
	}
	return h
}

// reboundAndRestored rotates the BMC credential, takes the cluster back with a
// staged destroy that rebinds it, and restores the cluster. It returns the
// root apply's binding and the staged destroy.
func reboundAndRestored(t *testing.T, h *harness) (string, string) {
	t.Helper()
	applied, _ := durableOperation(t, h)
	h.binder.versions["bmc"] = "ver-2"
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound staged destroy: %+v", diagnostics.Of(err))
	}
	removal, _ := currentRecord(t, h)
	if removal.Rebound[applied.Bindings[0]] == "" {
		t.Fatalf("the staged destroy recorded %v", removal.Rebound)
	}
	restoreWith(t, h)
	return applied.Bindings[0], removal.ID
}

// presentedLogin records, per step, the BMC password and the Grafana password
// each removal was lent.
func presentedLogin(h *harness) func() map[string][]string {
	var mutex sync.Mutex
	seen := map[string][]string{}
	h.capability.presenting = func(execution Execution) bool {
		bmc, _ := execution.Material["bmc"].Part(secrets.PasswordPart)
		grafana, _ := execution.Material[grafanaSecret].Part(secrets.ValuePart)
		mutex.Lock()
		seen[execution.Block.ID] = append(seen[execution.Block.ID], string(bmc)+"/"+string(grafana))
		mutex.Unlock()
		return true
	}
	return func() map[string][]string {
		mutex.Lock()
		defer mutex.Unlock()
		return seen
	}
}

// B1871: after a staged destroy rebound the BMC credential and a restore, a
// Grafana update reads the binding that presents its kept login step, the
// staged destroy's rebind, and replaces it in that destroy's record, so the
// holds keep the new version under the new binding and a whole destroy
// presents the kept step both the rebound credential and the updated password.
func TestAGrafanaUpdateAfterAStagedRebindAndARestoreRebindsThePresentingBinding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := reboundAdminHarness(t)
	root, staged := reboundAndRestored(t, h)
	removal, err := lineageReader(h).ReadOperation(ctx, staged)
	if err != nil {
		t.Fatal(err)
	}
	presented := removal.Rebound[root]
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("the Grafana update: %+v", diagnostics.Of(err))
	}
	if last := h.binder.rebinds[len(h.binder.rebinds)-1]; last.BindingID != presented {
		t.Fatalf("the update rebound %s, want the staged destroy's %s that presents the login", last.BindingID, presented)
	}
	updated, err := lineageReader(h).ReadOperation(ctx, staged)
	if err != nil {
		t.Fatal(err)
	}
	issued := updated.Rebound[root]
	version := h.binder.versions[grafanaSecret]
	if issued == presented || !slices.Contains(updated.Bindings, issued) || slices.Contains(updated.Bindings, presented) ||
		h.binder.pins[issued][grafanaSecret] != version || h.binder.pins[issued]["bmc"] != "ver-2" || !slices.Contains(h.binder.released, presented) {
		t.Fatalf("the staged destroy records %v and %v; %s pins %v; released %v", updated.Bindings, updated.Rebound, issued, h.binder.pins[issued], h.binder.released)
	}
	if applied, _ := lineageReader(h).ReadOperation(ctx, removal.Source); !slices.Contains(applied.Bindings, root) {
		t.Fatalf("the update rewrote the root apply's bindings %v", applied.Bindings)
	}
	holds, err := h.service.SecretHolds(ctx, testContextName)
	if err != nil || !heldUnder(holds, issued, grafanaSecret) {
		t.Fatalf("the holds are %+v (%v), want the updated Grafana password held under %s", holds, err, issued)
	}
	stored, _ := h.binder.versionMaterial[version].Part(secrets.ValuePart)
	seen := presentedLogin(h)
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["edge-monitoring"], []string{"second/" + string(stored)}) || !slices.Equal(got["cluster"], []string{"second/"}) {
		t.Fatalf("the whole destroy presented %v, want the kept step the rebound credential and the updated password", got)
	}
	if !slices.Contains(h.binder.released, issued) {
		t.Fatalf("the whole destroy released %v, want %s", h.binder.released, issued)
	}
}

// B1871: an update over a staged rebind interrupted between its rebind and
// the staged destroy's record is finished by repeating it, which rebinds the
// binding that still presents the login.
func TestAGrafanaUpdateOverAStagedRebindRecoversAfterRebindBeforeRecord(t *testing.T) {
	t.Parallel()
	h := reboundAdminHarness(t)
	root, staged := reboundAndRestored(t, h)
	record := staged + "/operation.json"
	h.workspace.area.landing = func(_, target string, _ map[string][]byte) error {
		if target == record {
			return errors.New("killed before the record")
		}
		return nil
	}
	if _, err := updateLogins(h, "grafana"); err == nil {
		t.Fatal("an interrupted record was reported as an update")
	}
	removal, err := lineageReader(h).ReadOperation(context.Background(), staged)
	presented := removal.Rebound[root]
	if err != nil || h.binder.rebinds[len(h.binder.rebinds)-1].BindingID != presented || slices.Contains(h.binder.released, presented) {
		t.Fatalf("the interruption did not land between the rebind and the record: %v (%v)", removal.Rebound, err)
	}
	h.workspace.area.landing = nil
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("the repeated update: %+v", diagnostics.Of(err))
	}
	updated, err := lineageReader(h).ReadOperation(context.Background(), staged)
	if issued := updated.Rebound[root]; err != nil || issued == presented || h.binder.pins[issued][grafanaSecret] != h.binder.versions[grafanaSecret] || len(h.binder.versionWrites()) != 1 {
		t.Fatalf("the repeat recorded %v (%v), writes %v", updated.Rebound, err, h.binder.versionWrites())
	}
}

// grafanaPassword is the value of the Secret's current version.
func grafanaPassword(h *harness) string {
	value, _ := h.binder.versionMaterial[h.binder.versions[grafanaSecret]].Part(secrets.ValuePart)
	return string(value)
}

// B1871: a second update over a staged destroy's rebind reads the binding the
// first issued and replaces it in the same record, and a whole destroy
// presents the kept step the last password with the rebound credential.
func TestTwoGrafanaUpdatesOverAStagedRebindEachReplaceThePresentingBinding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := reboundAdminHarness(t)
	root, staged := reboundAndRestored(t, h)
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("the first update: %+v", diagnostics.Of(err))
	}
	first, _ := lineageReader(h).ReadOperation(ctx, staged)
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("the second update: %+v", diagnostics.Of(err))
	}
	second, _ := lineageReader(h).ReadOperation(ctx, staged)
	if last := h.binder.rebinds[len(h.binder.rebinds)-1]; last.BindingID != first.Rebound[root] || second.Rebound[root] == first.Rebound[root] ||
		!slices.Contains(h.binder.released, first.Rebound[root]) || len(h.binder.versionWrites()) != 2 {
		t.Fatalf("the second update rebound %s over %v, recording %v; writes %v", last.BindingID, first.Rebound, second.Rebound, h.binder.versionWrites())
	}
	seen := presentedLogin(h)
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["edge-monitoring"], []string{"second/" + grafanaPassword(h)}) {
		t.Fatalf("the whole destroy presented %v", got)
	}
}

// B1871: over two staged destroys and restores, the first of which rebound
// the BMC credential, an update replaces that first rebind in its record and
// a whole destroy presents the kept step the updated password and the
// rebound credential.
func TestAGrafanaUpdateAfterTwoStagedDestroysAndRestoresReplacesTheFirstRebind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := reboundAdminHarness(t)
	root, staged := reboundAndRestored(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	again, _ := currentRecord(t, h)
	restoreWith(t, h)
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("the update: %+v", diagnostics.Of(err))
	}
	first, _ := lineageReader(h).ReadOperation(ctx, staged)
	later, _ := lineageReader(h).ReadOperation(ctx, again.ID)
	if issued := first.Rebound[root]; h.binder.pins[issued][grafanaSecret] != h.binder.versions[grafanaSecret] || h.binder.pins[issued]["bmc"] != "ver-2" || len(later.Rebound) != 0 {
		t.Fatalf("the first staged destroy records %v, the second %v", first.Rebound, later.Rebound)
	}
	seen := presentedLogin(h)
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["edge-monitoring"], []string{"second/" + grafanaPassword(h)}) {
		t.Fatalf("the whole destroy presented %v", got)
	}
}

// B1871: while a staged destroy is the current operation, an update refuses
// engine.credential-unapplied before anything is written.
func TestAGrafanaUpdateRefusesWhileAStagedDestroyIsCurrent(t *testing.T) {
	t.Parallel()
	h := reboundAdminHarness(t)
	stagedDestroy(t, h, reconciliation.StageClusters)
	_, err := updateLogins(h, "grafana")
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != engineCredentialUnapplied.Code || len(h.binder.versionWrites()) != 0 || len(h.binder.rebinds) != 0 {
		t.Fatalf("an update over a staged destroy returned %+v", reported)
	}
}
