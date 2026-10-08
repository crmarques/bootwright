package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const producedCanary = "apiVersion: v1\nkind: Config\n# kubeconfig-canary-4f2a9c\n"

func producedKubeconfig() []Produced {
	return []Produced{{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(producedCanary)})}}
}

func changedWithKubeconfig() Result {
	return Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`), Produced: producedKubeconfig()}
}

// blockState reads one block's durable state in the context's current
// operation, as a later invocation would.
func blockState(t *testing.T, h *harness, block string) reconciliation.BlockState {
	t.Helper()
	ctx := context.Background()
	store := operationstore.New(h.workspace.area, time.Now)
	index, err := store.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Block(ctx, index.Current, block)
	if err != nil {
		t.Fatal(err)
	}
	return record.State
}

func apply(h *harness) (*OperationResult, error) {
	return h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
}

func destroy(h *harness) (*OperationResult, error) {
	return h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
}

func kept(h *harness) []string { return h.binder.producedEntries() }

func TestProducedMaterialIsInCustodyBeforeTheBlockIsDone(t *testing.T) {
	h := newHarness(t, "install")
	h.capability.outcomes = []Result{changedWithKubeconfig()}
	var atPublication reconciliation.BlockState
	h.binder.kill = func(point string) error {
		if point == "publish produced material" {
			atPublication = blockState(t, h, "install")
		}
		return nil
	}
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	if atPublication != reconciliation.BlockRunning {
		t.Fatalf("the block was %q when its material entered custody, want running", atPublication)
	}
	if state := blockState(t, h, "install"); state != reconciliation.BlockDone || !slices.Equal(kept(h), []string{"install/kubeconfig=" + producedCanary}) {
		t.Fatalf("the block is %s with custody %v", state, kept(h))
	}
}

// A publication that fails leaves a proved effect whose access custody does
// not hold, so the block is neither done nor failed but unknown: the
// continuation observes it again and captures what the observation proves.
func TestAFailedPublicationLeavesTheAttemptUnknown(t *testing.T) {
	h := newHarness(t, "install")
	h.capability.outcomes = []Result{changedWithKubeconfig()}
	refusal := secretstore.Failure("store.conflict", "secret publication was not committed")
	h.binder.produceErr = refusal
	result, err := apply(h)
	if !reports(err, refusal) || result.Receipt.State != string(reconciliation.OperationUnknown) {
		t.Fatalf("apply = %+v, %v", result, err)
	}
	if state := blockState(t, h, "install"); state != reconciliation.BlockUnknown || len(kept(h)) != 0 {
		t.Fatalf("the block is %s with custody %v", state, kept(h))
	}
	h.binder.produceErr = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`), Produced: producedKubeconfig()}}
	if _, err := apply(h); err != nil || blockState(t, h, "install") != reconciliation.BlockDone || !slices.Equal(kept(h), []string{"install/kubeconfig=" + producedCanary}) {
		t.Fatalf("the continuation did not capture: %v, custody %v", err, kept(h))
	}
	if !slices.Equal(h.capability.calls, []string{"apply:install", "observe:install"}) {
		t.Fatalf("calls %v", h.capability.calls)
	}
}

// A destroy that follows a failed publication resolves the installation by
// the apply's own observation (D123) and captures its access before any
// inverse runs, so the
// media block's inverse, which deletes the installer's copy, never runs while
// custody holds none, and a removal that then stops keeps the only access.
func TestADestroyAfterAFailedPublicationCapturesBeforeAnyInverse(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("cluster-media-sno", reconciliation.StageClusters),
		stagedDefinition("machine-sno-01", reconciliation.StageMachines),
		stagedDefinition("cluster-install-sno", reconciliation.StageClusters, "cluster-media-sno", "machine-sno-01"),
	})
	h.service.options.Concurrency = 1
	changed := Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`)}
	h.capability.outcomeFor = map[string]Result{"cluster-media-sno": changed, "machine-sno-01": changed, "cluster-install-sno": changedWithKubeconfig()}
	refusal := secretstore.Failure("store.conflict", "secret publication was not committed")
	h.binder.produceErr = refusal
	if _, err := apply(h); !reports(err, refusal) || len(kept(h)) != 0 {
		t.Fatalf("apply = %v with custody %v", err, kept(h))
	}
	h.binder.produceErr = nil
	h.capability.outcomeFor = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`), Produced: producedKubeconfig()}}
	h.capability.outcomes = []Result{changed, changed, {Outcome: reconciliation.OutcomeFailed}}
	access := []string{"cluster-install-sno/kubeconfig=" + producedCanary}
	heldAtInverse := map[string][]string{}
	h.capability.destroyHold = func(block string) { heldAtInverse[block] = kept(h) }
	result, err := destroy(h)
	if err == nil || result.Receipt.State != string(reconciliation.OperationFailed) || !slices.Equal(h.capability.destroys, []string{"cluster-install-sno", "cluster-media-sno", "machine-sno-01"}) {
		t.Fatalf("destroy = %+v, %v after removing %v", result, err, h.capability.destroys)
	}
	if !slices.Equal(h.capability.observes, []string{"cluster-install-sno"}) || !slices.Equal(heldAtInverse["cluster-install-sno"], access) || !slices.Equal(heldAtInverse["cluster-media-sno"], access) {
		t.Fatalf("observed %v; custody at each inverse %v", h.capability.observes, heldAtInverse)
	}
	if !slices.Equal(kept(h), access) || !slices.Equal(h.binder.journal, []string{"produce cluster-install-sno"}) {
		t.Fatalf("the stopped removal kept %v after %v", kept(h), h.binder.journal)
	}
}

func TestACancelledPublicationLeavesItUnknown(t *testing.T) {
	h := newHarness(t, "install")
	h.capability.outcomes = []Result{changedWithKubeconfig()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.binder.kill = func(point string) error {
		if point == "publish produced material" {
			cancel()
			return ctx.Err()
		}
		return nil
	}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted capture completed")
	}
	if state := blockState(t, h, "install"); state != reconciliation.BlockUnknown || len(kept(h)) != 0 {
		t.Fatalf("the block is %s with custody %v", state, kept(h))
	}
}

// A resolution that proves an apply's block complete captures what its
// observation offers before it records the block done; one whose publication
// fails leaves the block unknown, so the next invocation observes it again.
// The observation ran, so the failure is custody's: the resolution records no
// observation failure, status never says the observation could not run, and
// the refusal is custody's own, diagnosed or not.
func TestAResolutionProvingCompletionPublishesAndAFailedPublicationStaysUnknown(t *testing.T) {
	diagnosed := secretstore.Failure("store.conflict", "secret publication was not committed")
	undiagnosed := errors.New("secret publication was lost")
	for _, refusal := range []error{nil, diagnosed, undiagnosed} {
		h := newHarness(t, "install")
		h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
		if _, err := apply(h); err == nil {
			t.Fatal("an unknown outcome completed")
		}
		h.binder.produceErr = refusal
		h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`), Produced: producedKubeconfig()}}
		result, err := apply(h)
		if refusal == nil {
			if err != nil || blockState(t, h, "install") != reconciliation.BlockDone || len(kept(h)) != 1 {
				t.Fatalf("resolution = %+v, %v with custody %v", result, err, kept(h))
			}
			continue
		}
		// An undiagnosed custody failure escapes as the internal failure it
		// was, never as the unresolved diagnosis an observation would give.
		refused := reports(err, diagnosed)
		if refusal == undiagnosed {
			got := diagnostics.Of(err)
			refused = len(got) > 0 && got[0].Code == "runtime.internal"
		}
		if !refused || blockState(t, h, "install") != reconciliation.BlockUnknown || len(kept(h)) != 0 || result.Receipt.State != string(reconciliation.OperationUnknown) {
			t.Fatalf("a failed publication resolved %+v, %+v with custody %v", result, diagnostics.Of(err), kept(h))
		}
		if recorded := settlingResolution(t, h, "install").Failure; recorded != nil {
			t.Fatalf("a failed publication recorded the observation failure %+v", recorded)
		}
		status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
		if err != nil || status.Lifecycle == nil {
			t.Fatalf("status = %+v (%v)", status, err)
		}
		for _, reported := range status.Lifecycle.Blocks {
			if reported.Unresolved != nil && strings.HasPrefix(reported.Unresolved.Reason, "its observation could not run") {
				t.Fatalf("status says %+v after a failed publication", reported.Unresolved)
			}
		}
	}
}

// A removal never captures: neither what its attempt offers nor what its
// removal observation offers, so only the apply's publication ever reaches
// custody.
func TestADestroyNeverCaptures(t *testing.T) {
	removalOffer := func() []Produced {
		return []Produced{{Name: "kubeconfig", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("offered by the removal")})}}
	}
	for _, resolved := range []bool{false, true} {
		h := newHarness(t, "install")
		h.capability.outcomes = []Result{changedWithKubeconfig(), {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`), Produced: removalOffer()}}
		if resolved {
			h.capability.outcomes[1] = Result{Outcome: reconciliation.OutcomeUnknown, Produced: removalOffer()}
		}
		if _, err := apply(h); err != nil {
			t.Fatal(err)
		}
		var withdrawn []string
		h.binder.kill = func(point string) error {
			if point == "withdraw produced material" {
				withdrawn = kept(h)
			}
			return nil
		}
		_, err := destroy(h)
		if resolved {
			if err == nil {
				t.Fatal("an unknown removal completed")
			}
			h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`), Produced: removalOffer()}}
			_, err = destroy(h)
		}
		if err != nil {
			t.Fatal(err)
		}
		if resolved && !slices.Contains(h.capability.calls, "observe-removal:install") {
			t.Fatalf("calls %v", h.capability.calls)
		}
		if !slices.Equal(h.binder.journal, []string{"produce install", "withdraw"}) || !slices.Equal(withdrawn, []string{"install/kubeconfig=" + producedCanary}) || len(kept(h)) != 0 {
			t.Fatalf("custody journal %v, withdrew %v, kept %v", h.binder.journal, withdrawn, kept(h))
		}
	}
}

// A fresh destroy over an apply that never proved its block resolves that
// block first by the apply's observation (D123), so a completion it proves is
// captured like the apply's own before the removal withdraws it.
func TestAFreshDestroyCapturesWhatItsResolutionOfTheApplyProves(t *testing.T) {
	h := newHarness(t, "install")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := apply(h); err == nil {
		t.Fatal("an unknown outcome completed")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`), Produced: producedKubeconfig()}}
	if _, err := destroy(h); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(h.capability.calls, "observe:install") || !slices.Equal(h.binder.journal, []string{"produce install", "withdraw"}) || len(kept(h)) != 0 {
		t.Fatalf("calls %v, custody journal %v, kept %v", h.capability.calls, h.binder.journal, kept(h))
	}
}

func TestACompletedRemovalWithdrawsEveryEntryBeforeReleasingReservations(t *testing.T) {
	h := newHarness(t, "install", "media")
	h.capability.reservations = []prerequisites.HostReservation{{Context: testContextName, Kind: "artifact-server", Service: "install", Keys: []string{"socket:192.0.2.1:8443"}}}
	h.capability.outcomes = []Result{changedWithKubeconfig(), {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`), Produced: []Produced{{Name: "password", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("secret")})}}}}
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	if len(kept(h)) != 2 || len(h.workspace.reservations) == 0 {
		t.Fatalf("the apply kept %v and reserved %v", kept(h), h.workspace.reservations)
	}
	var reservedAtWithdrawal int
	h.binder.kill = func(point string) error {
		if point == "withdraw produced material" {
			reservedAtWithdrawal = len(h.workspace.reservations)
		}
		return nil
	}
	if _, err := destroy(h); err != nil {
		t.Fatal(err)
	}
	if reservedAtWithdrawal == 0 || len(kept(h)) != 0 || len(h.workspace.reservations) != 0 || h.binder.journal[len(h.binder.journal)-1] != "withdraw" {
		t.Fatalf("reserved at withdrawal %d, kept %v, reserved %v, journal %v", reservedAtWithdrawal, kept(h), h.workspace.reservations, h.binder.journal)
	}
	if !bytes.Equal(h.workspace.evidence, pristineEvidence(t)) {
		t.Fatalf("the completed removal left evidence %s", h.workspace.evidence)
	}
}

// Removal runs dependents first, so the installation's inverse is done before
// the block its work area belongs to. A removal that stops there has not
// completed, and the access it would leave behind is the only one: custody
// keeps it.
func TestAStoppedDestroyKeepsTheAccess(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("media", reconciliation.StageClusters),
		stagedDefinition("install", reconciliation.StageClusters, "media"),
	})
	h.capability.outcomeFor = map[string]Result{"media": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`)}, "install": changedWithKubeconfig()}
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomeFor = nil
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`)}, {Outcome: reconciliation.OutcomeFailed}}
	result, err := destroy(h)
	if err == nil || result.Receipt.State != string(reconciliation.OperationFailed) || !slices.Equal(h.capability.destroys, []string{"install", "media"}) {
		t.Fatalf("destroy = %+v, %v after removing %v", result, err, h.capability.destroys)
	}
	if blockState(t, h, "install") != reconciliation.BlockDone || !slices.Equal(kept(h), []string{"install/kubeconfig=" + producedCanary}) || slices.Contains(h.binder.journal, "withdraw") {
		t.Fatalf("the stopped removal kept %v after %v", kept(h), h.binder.journal)
	}
}

// A removal whose record reads done but whose withdrawal failed has not
// finished: its evidence stays short of pristine, and the next invocation of
// either verb finalizes it, withdrawing what it left.
func TestAFinalizationWithdrawsWhatAnInterruptedRemovalLeft(t *testing.T) {
	for _, verb := range []reconciliation.Verb{reconciliation.Destroy, reconciliation.Apply} {
		t.Run(string(verb), func(t *testing.T) {
			h := newHarness(t, "install")
			h.capability.outcomes = []Result{changedWithKubeconfig()}
			if _, err := apply(h); err != nil {
				t.Fatal(err)
			}
			refusal := secretstore.Failure("store.conflict", "secret publication was not committed")
			h.binder.withdrawErr = refusal
			if _, err := destroy(h); !reports(err, refusal) {
				t.Fatalf("the interrupted removal returned %v", err)
			}
			if len(kept(h)) != 1 || bytes.Equal(h.workspace.evidence, pristineEvidence(t)) || len(h.workspace.reservations) == 0 && len(h.capability.reservations) != 0 {
				t.Fatalf("the interrupted removal left custody %v and evidence %s", kept(h), h.workspace.evidence)
			}
			h.binder.withdrawErr = nil
			var err error
			if verb == reconciliation.Destroy {
				_, err = destroy(h)
			} else {
				_, err = apply(h)
			}
			if err != nil && verb == reconciliation.Destroy {
				t.Fatal(err)
			}
			if len(kept(h)) != 0 || !slices.Equal(h.binder.journal, []string{"produce install", "withdraw"}) {
				t.Fatalf("the %s finalization left custody %v after %v (%v)", verb, kept(h), h.binder.journal, err)
			}
		})
	}
}

// Produced material is confidential: no operation record, log, evidence,
// adapter output or diagnostic carries its bytes, whether it entered custody
// or its publication failed.
func TestProducedMaterialNeverReachesRecordsLogsOrAdapterOutput(t *testing.T) {
	for _, failing := range []bool{false, true} {
		h := newHarness(t, "install")
		h.capability.outcomes = []Result{changedWithKubeconfig()}
		if failing {
			h.binder.produceErr = secretstore.Failure("store.conflict", "secret publication was not committed")
		}
		progress := &testProgress{}
		h.service.options.Progress = progress
		_, err := apply(h)
		canary := []byte("kubeconfig-canary-4f2a9c")
		for _, area := range []*memoryArea{h.workspace.area, h.workspace.runArea} {
			for name, data := range area.clone().files {
				if bytes.Contains(data, canary) {
					t.Fatalf("%s carries produced material", name)
				}
			}
		}
		if bytes.Contains(h.workspace.evidence, canary) || strings.Contains(strings.Join(progress.reported(), "\n"), string(canary)) {
			t.Fatal("the evidence or the progress carries produced material")
		}
		for _, reported := range diagnostics.Of(err) {
			if strings.Contains(reported.Message+reported.Remediation, string(canary)) {
				t.Fatal("a diagnostic carries produced material")
			}
		}
		if failing && !logHas(h, "custody-failed") {
			t.Fatal("a failed publication logged no diagnosis")
		}
	}
}

// reports tells whether err carries the diagnosis refusal carries.
func reports(err, refusal error) bool {
	want := diagnostics.Of(refusal)[0]
	return slices.ContainsFunc(diagnostics.Of(err), func(found diagnostics.Diagnostic) bool {
		return found.Code == want.Code && found.Message == want.Message
	})
}

func logHas(h *harness, event string) bool {
	for name, data := range h.workspace.area.clone().files {
		if strings.Contains(name, "/logs/") && bytes.Contains(data, []byte(`"event":"`+event+`"`)) {
			return true
		}
	}
	return false
}

func pristineEvidence(t *testing.T) []byte {
	t.Helper()
	data, err := reconciliation.PristineEvidence().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}
