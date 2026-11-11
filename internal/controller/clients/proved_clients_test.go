package clients

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestProvedClientsNameEachReleaseACompletedStageProved(t *testing.T) {
	evidence := Evidence{Area: "area", Packages: []PackageRecord{}, Request: "sha256:request", Tools: []ToolRecord{
		{Kind: "helm", SHA256: "aa", Version: "3.14.0"}, {Kind: "kubectl", SHA256: "bb", Version: "1.37.2"},
	}}
	encoded, err := evidence.encode()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{RequestDigest: "sha256:request"}
	proved := lifecycle.BlockEvidence{State: reconciliation.BlockDone, Evidence: encoded}
	want := []lifecycle.ProvedClient{{Kind: "helm", Version: "3.14.0"}, {Kind: "kubectl", Version: "1.37.2"}}
	if got := (Capability{}).ProvedClients(block, proved); !slices.Equal(got, want) {
		t.Fatalf("proved %+v, want %+v", got, want)
	}
	other := block
	other.RequestDigest = "sha256:other"
	unfinished := proved
	unfinished.State = reconciliation.BlockFailed
	retained := evidence
	retained.Retained = true
	kept, err := retained.encode()
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]lifecycle.ProvedClient{
		"another request":     (Capability{}).ProvedClients(other, proved),
		"an unfinished block": (Capability{}).ProvedClients(block, unfinished),
		"a retaining removal": (Capability{}).ProvedClients(block, lifecycle.BlockEvidence{State: reconciliation.BlockDone, Evidence: kept}),
	} {
		if len(got) != 0 {
			t.Fatalf("%s proved %+v", name, got)
		}
	}
}
