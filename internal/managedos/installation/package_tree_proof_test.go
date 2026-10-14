package installation

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestPackageTreeProofRequiresOriginalFrozenCompletedPublication(t *testing.T) {
	execution, request := treeFixture(t)
	digest, err := reconciliation.RequestDigest(execution.Block.BlockDefinition)
	if err != nil {
		t.Fatal(err)
	}
	execution.Block.RequestDigest = digest
	evidence := TreeEvidence{Version: TreeEvidenceVersion, Request: digest, Content: true, Tree: true, Identity: request.Media.SHA256, Postcondition: true}
	data, err := reconciliation.Freeze(evidence, "tree evidence")
	if err != nil {
		t.Fatal(err)
	}
	proof := lifecycle.BlockEvidence{Block: execution.Block, Kind: TreeKind, Object: request.Identity.Profile, Implementation: TreeImplementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: data}
	got, err := ProvedPackageTree([]lifecycle.BlockEvidence{proof}, request.Identity.Profile)
	if err != nil || got.Media != request.Media || got.Publication != request.Publication {
		t.Fatalf("lost immutable DVD source: %+v %v", got, err)
	}
	for _, change := range []func(*lifecycle.BlockEvidence){
		func(p *lifecycle.BlockEvidence) { p.Verb = reconciliation.Destroy },
		func(p *lifecycle.BlockEvidence) { p.State = reconciliation.BlockPending },
		func(p *lifecycle.BlockEvidence) { p.Block.RequestDigest = "changed" },
		func(p *lifecycle.BlockEvidence) { p.Block.Object = "another-profile" },
		func(p *lifecycle.BlockEvidence) { p.Evidence = []byte(`{}`) },
	} {
		bad := proof
		change(&bad)
		if _, err := ProvedPackageTree([]lifecycle.BlockEvidence{bad}, request.Identity.Profile); err == nil {
			t.Fatal("accepted ambiguous, incomplete or changed DVD identity")
		}
	}
	for _, proofs := range [][]lifecycle.BlockEvidence{nil, {proof, proof}} {
		if _, err := ProvedPackageTree(proofs, request.Identity.Profile); err == nil {
			t.Fatal("accepted missing or duplicated DVD proof")
		}
	}
}
