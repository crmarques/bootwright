package lifecycle_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// keptHost is the controller one agent installation ran on, as both of its
// adapters see it: the installer's work area, which holds the administrator
// kubeconfig the installation keeps, and the cluster's nodes. Every run is
// recorded as "<block kind>:<operation>", the media observation that hands
// over the kept copy as "media:keep".
type keptHost struct {
	mutex sync.Mutex
	// work is whether the work area exists, and kept the kubeconfig the
	// installation keeps in it.
	work bool
	kept string
	// poweredOff is every node stopped, so nothing answers and no node runs.
	poweredOff bool
	// atRemoval runs when the media removal starts, before it deletes
	// anything; stopRemoval makes it stop there, and failRemoval makes it fail
	// once the work area is gone.
	atRemoval   func()
	stopRemoval func()
	failRemoval bool
	calls       []string
}

func (h *keptHost) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if run.Implementation == agentinstall.MediaImplementation {
		return h.media(run)
	}
	return h.install(run)
}

func (h *keptHost) install(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	call := run.Operation
	if run.Operation == "observe" && len(run.Outputs) == 0 {
		call = "observe-removal"
	}
	h.calls = append(h.calls, "install:"+call)
	switch run.Operation {
	case "apply":
		h.kept = adminAccess
		evidence := agentinstall.InstallEvidence{
			Cluster: installIdentity, Completed: true, Identity: installIdentity, Media: []string{}, Missing: []string{},
			OwnMedia: []string{}, Postcondition: true, Powered: []string{"sno-01"}, Release: installedRelease, Request: run.Digest,
		}
		return lifecycle.RunResult{Outcome: "changed", Evidence: installEvidenceOf(evidence), Produced: adminKubeconfig()}, nil
	case "destroy":
		evidence := agentinstall.InstallEvidence{Absent: true, Media: []string{}, Missing: []string{}, OwnMedia: []string{}, Postcondition: true, Powered: []string{}, Request: run.Digest}
		return lifecycle.RunResult{Outcome: "unchanged", Evidence: installEvidenceOf(evidence)}, nil
	}
	evidence := agentinstall.InstallEvidence{
		Identity: installIdentity, Media: []string{}, Missing: []string{}, OwnMedia: []string{}, Powered: []string{}, Request: run.Digest,
	}
	var produced []lifecycle.Produced
	if !h.poweredOff {
		evidence.Cluster, evidence.Completed, evidence.Postcondition = installIdentity, true, true
		evidence.Powered, evidence.Release = []string{"sno-01"}, installedRelease
		if len(run.Outputs) != 0 {
			produced = adminKubeconfig()
		}
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: installEvidenceOf(evidence), Produced: produced}, nil
}

func (h *keptHost) media(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	published := agentinstall.MediaEvidence{Image: true, Inputs: run.Digest, Installer: installedRelease, Postcondition: true, Request: run.Digest, Work: h.work}
	switch run.Operation {
	case "apply":
		h.calls = append(h.calls, "media:apply")
		h.work, published.Work = true, true
		return lifecycle.RunResult{Outcome: "changed", Evidence: mediaEvidenceOf(published)}, nil
	case "destroy":
		h.calls = append(h.calls, "media:destroy")
		if h.atRemoval != nil {
			h.atRemoval()
		}
		if h.stopRemoval != nil {
			h.stopRemoval()
			return lifecycle.RunResult{}, context.Canceled
		}
		h.work, h.kept = false, ""
		if h.failRemoval {
			return lifecycle.RunResult{}, diagnostics.NewFailureWithRemediation("lifecycle.state", "the published image could not be removed", "", "")
		}
		absent := agentinstall.MediaEvidence{Absent: true, Postcondition: true, Request: run.Digest}
		return lifecycle.RunResult{Outcome: "changed", Evidence: mediaEvidenceOf(absent)}, nil
	}
	if len(run.Outputs) == 0 {
		h.calls = append(h.calls, "media:observe")
		return lifecycle.RunResult{Outcome: "unchanged", Evidence: mediaEvidenceOf(published)}, nil
	}
	h.calls = append(h.calls, "media:keep")
	var produced []lifecycle.Produced
	if h.work && h.kept != "" {
		produced = []lifecycle.Produced{{Name: agentinstall.KubeconfigOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(h.kept)})}}
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: mediaEvidenceOf(published), Produced: produced}, nil
}

func (h *keptHost) took() []string {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	calls := h.calls
	h.calls = nil
	return calls
}

// holds reports the work area and the kubeconfig it keeps.
func (h *keptHost) holds() (bool, string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	return h.work, h.kept
}

func (h *keptHost) set(change func(*keptHost)) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	change(h)
}

func mediaEvidenceOf(evidence agentinstall.MediaEvidence) json.RawMessage {
	data, _ := json.Marshal(evidence)
	return data
}

// clusterCapabilities answers for both of one agent installation's blocks,
// each through its own production capability over the one host double.
type clusterCapabilities struct {
	install retainedInstaller
	media   agentinstall.MediaCapability
}

func (c clusterCapabilities) pick(block reconciliation.Block) lifecycle.Capability {
	if block.Implementation == agentinstall.MediaImplementation {
		return c.media
	}
	return c.install
}

func (c clusterCapabilities) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	return c.install.Plan(ctx, input)
}

func (c clusterCapabilities) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	return c.pick(block).Removal(ctx, block)
}

func (c clusterCapabilities) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.pick(execution.Block).Apply(ctx, located(execution))
}

func (c clusterCapabilities) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.pick(execution.Block).Observe(ctx, located(execution))
}

func (c clusterCapabilities) Quiescent(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	return c.pick(probe.Block).Quiescent(ctx, probe)
}

func (c clusterCapabilities) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.pick(execution.Block).Destroy(ctx, located(execution))
}

func (c clusterCapabilities) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.pick(execution.Block).ObserveRemoval(ctx, located(execution))
}

func (c clusterCapabilities) Keeps(block reconciliation.Block) (lifecycle.KeptEntry, bool, error) {
	if block.Implementation != agentinstall.MediaImplementation {
		return lifecycle.KeptEntry{}, false, nil
	}
	return c.media.Keeps(block)
}

func (c clusterCapabilities) Keep(ctx context.Context, execution lifecycle.Execution) ([]lifecycle.Produced, error) {
	return c.media.Keep(ctx, execution)
}

// keptMediaRequest is the frozen boot-media request of the installation
// installedRequest describes, building in the same work area.
func keptMediaRequest() agentinstall.MediaRequest {
	install := installedRequest()
	return agentinstall.MediaRequest{
		AgentConfig: map[string]any{}, Budgets: agentinstall.MediaBudgets{BuildSeconds: 900},
		Identity: agentinstall.Identity{Block: agentinstall.MediaBlockID(installedCluster), Cluster: installedCluster, Context: lifecycle.JourneyContext},
		Image:    install.Image, InstallConfig: map[string]any{}, Placement: install.Placement,
		PullSecretRef: "lab-pull-secret", Release: install.Release, SSHKeyRef: "lab-cluster-key",
		TLSCertificateRef: "lab-artifact-tls", Tool: install.Tool, Version: "cluster-media-agent-v5", WorkRoot: install.WorkRoot,
	}
}

// keptJourney plans one agent installation, its media block and the
// installation that boots from it, and applies both: the adapter completes
// the installation and keeps its administrator kubeconfig in the work area,
// while custody refuses to publish it, so the installation is unknown and
// custody holds nothing. Then every node is powered off.
func keptJourney(t *testing.T) (*lifecycle.CapabilityJourney, *keptHost) {
	t.Helper()
	install, media := installedRequest(), keptMediaRequest()
	installCanonical, err := install.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	mediaCanonical, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	host := &keptHost{}
	capabilities := clusterCapabilities{install: retainedInstaller{agentinstall.NewInstall(host)}, media: agentinstall.NewMedia(host)}
	journey := lifecycle.NewCapabilitiesJourney(t, capabilities, []lifecycle.CapabilityBinding{
		{Kind: agentinstall.Kind, Implementation: agentinstall.MediaImplementation},
		{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation},
	}, lifecycle.CapabilityPlan{
		Definitions: []reconciliation.BlockDefinition{{
			ID: media.Identity.Block, Description: "build the boot media of sno", Stage: reconciliation.StageClusters,
			Kind: agentinstall.Kind, Object: installedCluster, Implementation: agentinstall.MediaImplementation,
			ContentDigest: strings.Repeat("d", 64), Request: mediaCanonical,
		}, {
			ID: install.Identity.Block, Description: "install the cluster sno", Stage: reconciliation.StageClusters,
			Dependencies: []string{media.Identity.Block},
			Kind:         agentinstall.Kind, Object: installedCluster, Implementation: agentinstall.InstallImplementation,
			ContentDigest: strings.Repeat("c", 64), Request: installCanonical,
			Groups: []reconciliation.Group{{ID: "boot-machines", Description: "boot each node", Machines: []string{"sno-01"}}},
		}},
		Secrets: append(install.SecretReferences(), media.SecretReferences()...),
	})
	journey.Holding(media.SSHKeyRef, secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: []byte("ssh-ed25519 AAAA lab")}))
	journey.FailPublication(secretstore.Failure("store.conflict", "secret publication was not committed"))
	_, err = journey.Service.Apply(context.Background(), lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if got := diagnostics.Of(err); len(got) == 0 || got[0].Code != "secret.store.conflict" {
		t.Fatalf("the apply = %+v (%v), want custody's refusal", got, err)
	}
	if calls := host.took(); !slices.Equal(calls, []string{"media:apply", "install:apply"}) || len(journey.Kept()) != 0 {
		t.Fatalf("the apply ran %v and custody holds %v", calls, journey.Kept())
	}
	journey.FailPublication(nil)
	host.set(func(h *keptHost) { h.poweredOff = true })
	return journey, host
}

// An agent installation that completed while custody refused its
// administrator kubeconfig keeps the only copy in the installer's work area.
// Its nodes powered off, a destroy's resolution cannot prove the installation
// complete and captures nothing, yet the media block's removal would delete
// that work area. So before it runs, the copy moves into custody marked
// unproved, under the entry a completed installation's capture fills, and only
// then is the work area removed (D124). The entry stays while the removal has
// not completed, and a completed removal withdraws it as it withdraws any.
func TestAKeptKubeconfigMovesIntoCustodyBeforeItsWorkAreaIsRemoved(t *testing.T) {
	entry := agentinstall.InstallBlockID(installedCluster) + "/" + agentinstall.KubeconfigOutput + " (unproved)=" + adminAccess
	removal := []string{"install:observe", "install:observe-removal", "install:destroy", "media:keep", "media:destroy"}
	for _, tc := range []struct {
		name    string
		fails   bool
		state   string
		journal []string
		kept    []string
	}{
		{"the destroy completes", false, "done", []string{"produce unproved " + agentinstall.InstallBlockID(installedCluster), "withdraw"}, []string{}},
		{"the destroy stops once the work area is gone", true, "failed", []string{"produce unproved " + agentinstall.InstallBlockID(installedCluster)}, []string{entry}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			journey, host := keptJourney(t)
			held := []string{}
			host.set(func(h *keptHost) {
				h.failRemoval = tc.fails
				h.atRemoval = func() { held = journey.Kept() }
			})
			result, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
			if result == nil || result.Receipt.State != tc.state || (err == nil) == tc.fails {
				t.Fatalf("the removal = %+v, %+v (%v), want %s", result, diagnostics.Of(err), err, tc.state)
			}
			if calls := host.took(); !slices.Equal(calls, removal) {
				t.Fatalf("the removal ran %v, want %v", calls, removal)
			}
			if !slices.Equal(held, []string{entry}) {
				t.Fatalf("custody held %v when the work area's removal began, want %v", held, entry)
			}
			if work, kept := host.holds(); work || kept != "" {
				t.Fatalf("the work area is %t and keeps %q, want it removed", work, kept)
			}
			if journal, kept := journey.CustodyJournal(), journey.Kept(); !slices.Equal(journal, tc.journal) || !slices.Equal(kept, tc.kept) {
				t.Fatalf("custody committed %v and keeps %v, want %v and %v", journal, kept, tc.journal, tc.kept)
			}
			detail := agentinstall.InstallBlockID(installedCluster) + "/" + agentinstall.KubeconfigOutput
			if got := journey.BlockLog(t, agentinstall.MediaBlockID(installedCluster), "kept-unproved"); len(got) != 1 || got[0].Detail != detail {
				t.Fatalf("the media block logged %+v, want one kept copy of %s", got, detail)
			}
		})
	}
}

// A work area that keeps no kubeconfig hands over nothing: custody stays empty
// and the removal logs no copy kept, because none was.
func TestAWorkAreaKeepingNothingLogsNoKeptCopy(t *testing.T) {
	journey, host := keptJourney(t)
	host.set(func(h *keptHost) { h.kept = "" })
	result, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("the removal = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if calls := host.took(); !slices.Contains(calls, "media:keep") || !slices.Contains(calls, "media:destroy") {
		t.Fatalf("the removal ran %v, want the keep offered and the removal run", calls)
	}
	if journal := journey.CustodyJournal(); slices.ContainsFunc(journal, func(entry string) bool { return strings.HasPrefix(entry, "produce") }) {
		t.Fatalf("custody committed %v, want no publication", journal)
	}
	if got := journey.BlockLog(t, agentinstall.MediaBlockID(installedCluster), "kept-unproved"); len(got) != 0 {
		t.Fatalf("the media block logged %+v, want no kept copy", got)
	}
}

// A removal stopped after the move and before the work area's deletion leaves
// the copy in both places; the destroy that continues finds custody holding
// it, so it keeps nothing more and removes the work area.
func TestAKeptKubeconfigStoppedBetweenTheMoveAndTheRemovalIsInBothPlaces(t *testing.T) {
	journey, host := keptJourney(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host.set(func(h *keptHost) { h.stopRemoval = cancel })
	if _, err := journey.Service.Destroy(ctx, lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}); err == nil {
		t.Fatal("the stopped removal reported no failure")
	}
	host.took()
	entry := agentinstall.InstallBlockID(installedCluster) + "/" + agentinstall.KubeconfigOutput + " (unproved)=" + adminAccess
	if work, kept := host.holds(); !work || kept != adminAccess || !slices.Equal(journey.Kept(), []string{entry}) {
		t.Fatalf("the work area is %t keeping %q and custody keeps %v, want the copy in both", work, kept, journey.Kept())
	}
	host.set(func(h *keptHost) { h.stopRemoval = nil })
	resolved, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err == nil || resolved.Receipt.State != "failed" {
		t.Fatalf("the resolving removal = %+v, %+v (%v), want the stopped removal resolved as not performed", resolved, diagnostics.Of(err), err)
	}
	result, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("the continued removal = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if calls := host.took(); !slices.Equal(calls, []string{"media:observe", "media:destroy"}) {
		t.Fatalf("the continued removal ran %v, want the resolution and the removal and no second keep", calls)
	}
	if work, _ := host.holds(); work {
		t.Fatal("the continued removal left the work area")
	}
	journal := []string{"produce unproved " + agentinstall.InstallBlockID(installedCluster), "withdraw"}
	if got := journey.CustodyJournal(); !slices.Equal(got, journal) {
		t.Fatalf("custody committed %v, want %v", got, journal)
	}
}

// Custody that already holds the installation's proved kubeconfig keeps it:
// the media removal reads nothing for custody and deletes the work area.
func TestAProvedKubeconfigIsNotKeptAgainBeforeTheWorkAreaIsRemoved(t *testing.T) {
	journey, host := keptJourney(t)
	host.set(func(h *keptHost) { h.poweredOff = false })
	result, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("the removal = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	if calls := host.took(); !slices.Equal(calls, []string{"install:observe", "install:destroy", "media:destroy"}) {
		t.Fatalf("the removal ran %v", calls)
	}
	journal := []string{"produce " + agentinstall.InstallBlockID(installedCluster), "withdraw"}
	if got := journey.CustodyJournal(); !slices.Equal(got, journal) {
		t.Fatalf("custody committed %v, want %v", got, journal)
	}
}

// Custody that refuses the kept copy stops the media removal before it
// deletes anything: the block records a failed removal and the copy stays in
// the work area, never in neither place.
func TestARefusedKeepLeavesTheWorkAreaInPlace(t *testing.T) {
	journey, host := keptJourney(t)
	journey.FailPublication(secretstore.Failure("store.conflict", "secret publication was not committed"))
	result, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true})
	if got := diagnostics.Of(err); result == nil || result.Receipt.State != "failed" || len(got) == 0 || got[0].Code != "secret.store.conflict" {
		t.Fatalf("the removal = %+v, %+v (%v), want custody's refusal", result, got, err)
	}
	if calls := host.took(); !slices.Equal(calls, []string{"install:observe", "install:observe-removal", "install:destroy", "media:keep"}) {
		t.Fatalf("the removal ran %v, want no deletion after the refused keep", calls)
	}
	if work, kept := host.holds(); !work || kept != adminAccess || len(journey.Kept()) != 0 {
		t.Fatalf("the work area is %t keeping %q and custody keeps %v", work, kept, journey.Kept())
	}
	media := agentinstall.MediaBlockID(installedCluster)
	if !slices.ContainsFunc(result.Blocks, func(block lifecycle.BlockResult) bool {
		return block.ID == media && block.State == string(reconciliation.BlockFailed)
	}) {
		t.Fatalf("blocks = %+v, want %s failed", result.Blocks, media)
	}
}
