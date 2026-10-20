package lifecycle_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type compactInstaller struct {
	calls   []string
	first   bool
	foreign bool
}

func (i *compactInstaller) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	i.calls = append(i.calls, run.Operation)
	state := agentinstall.InstallEvidence{Identity: installIdentity, Request: run.Digest,
		Missing: []string{"master-0", "master-1", "master-2"}, Media: []string{"compact-01", "compact-02"},
		OwnMedia: []string{"compact-01", "compact-02"}, Powered: []string{"compact-01"}}
	if i.foreign {
		state.OwnMedia = []string{"compact-01"}
		state.Powered = []string{"compact-01", "compact-02"}
	}
	if run.Operation == "apply" && !i.first {
		i.first = true
		return lifecycle.RunResult{Outcome: "changed", Evidence: installEvidenceOf(state)}, nil
	}
	if run.Operation == "observe" {
		return lifecycle.RunResult{Outcome: "unchanged", Evidence: installEvidenceOf(state)}, nil
	}
	state.Cluster, state.Completed, state.Postcondition, state.Release = installIdentity, true, true, installedRelease
	state.Missing, state.Media, state.OwnMedia = []string{}, []string{}, []string{}
	state.Powered = []string{"compact-01", "compact-02", "compact-03"}
	return lifecycle.RunResult{Outcome: "changed", Evidence: installEvidenceOf(state), Produced: adminKubeconfig()}, nil
}

func compactJourney(t *testing.T) (*lifecycle.CapabilityJourney, *compactInstaller, string) {
	t.Helper()
	request := installedRequest()
	request.Nodes = []agentinstall.Node{}
	for n, address := range []string{"198.51.100.21", "198.51.100.22", "198.51.100.23"} {
		node := installedRequest().Nodes[0]
		node.Name, node.Machine, node.Address = "master-"+string(rune('0'+n)), "compact-0"+string(rune('1'+n)), address
		request.Nodes = append(request.Nodes, node)
	}
	request.Rendezvous = request.Nodes[0].Address
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	adapter := &compactInstaller{}
	definition := reconciliation.BlockDefinition{ID: request.Identity.Block, Description: "install the compact cluster", Stage: reconciliation.StageClusters,
		Kind: agentinstall.Kind, Object: request.Identity.Cluster, Implementation: agentinstall.InstallImplementation,
		ContentDigest: strings.Repeat("c", 64), Request: canonical, Exclusive: []string{"container-cluster-install"}}
	journey := lifecycle.NewCapabilityJourney(t, retainedInstaller{agentinstall.NewInstall(adapter)},
		lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation},
		lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{definition}, Secrets: request.SecretReferences()})
	return journey, adapter, definition.ID
}

func TestCompactClusterContinuesAnInterruptedBootAfterResolvingMixedNodes(t *testing.T) {
	journey, adapter, block := compactJourney(t)
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Apply(context.Background(), apply); err == nil {
		t.Fatal("incomplete boot was marked done")
	}
	if _, err := journey.Service.Apply(context.Background(), apply); err != nil {
		t.Fatal("mixed-node continuation failed", diagnostics.Of(err))
	}
	if !slices.Equal(adapter.calls, []string{"apply", "observe", "apply"}) {
		t.Fatal("unknown boot was not observed before retry", adapter.calls)
	}
	records := journey.Records(t, block)
	if len(records) != 3 || records[1].Effect != reconciliation.EffectPartial {
		t.Fatal("mixed nodes were not resolved partial", records)
	}
	var evidence agentinstall.InstallEvidence
	if err := json.Unmarshal(records[1].Evidence, &evidence); err != nil || !slices.Equal(evidence.Powered, []string{"compact-01"}) || len(evidence.Media) != 2 {
		t.Fatal("resolution lost per-node boot state", evidence, err)
	}
}

func TestCompactClusterUnknownWithForeignMediaRemainsUnresolved(t *testing.T) {
	journey, adapter, _ := compactJourney(t)
	apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
	if _, err := journey.Service.Apply(context.Background(), apply); err == nil {
		t.Fatal("incomplete boot was marked done")
	}
	adapter.foreign = true
	if _, err := journey.Service.Apply(context.Background(), apply); err == nil {
		t.Fatal("foreign running media was retried")
	}
	if !slices.Equal(adapter.calls, []string{"apply", "observe"}) {
		t.Fatal("unproved mixed nodes were mutated", adapter.calls)
	}
}
