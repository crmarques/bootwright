package clients

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func routeProof(t *testing.T, proxy string) lifecycle.BlockEvidence {
	t.Helper()
	r := NewRequest(controller.Baseline(), nil)
	r.Machine, r.Egress.HTTPSProxy = "controller", proxy
	if proxy != "" {
		r.Egress.NoProxy = []string{".internal.test"}
	}
	wire, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{{
		ID: BlockID, Description: "prepare controller", Kind: Kind, Object: "example", Stage: reconciliation.StageController,
		Implementation: Implementation, ContentDigest: ContentDigest(), Request: wire,
	}})
	if err != nil {
		t.Fatal(err)
	}
	block := plan.Blocks[0]
	evidence, err := newEvidence(block.RequestDigest, "", false, nil, nil).encode()
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.BlockEvidence{Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Block: block,
		Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence}
}

func TestProvedEgressReadsOnlyTheFrozenCompletedControllerRoute(t *testing.T) {
	for _, proxy := range []string{"", "http://proxy.example.test:3128", "https://proxy.example.test:8443"} {
		proof := routeProof(t, proxy)
		route, err := ProvedEgress("controller", []lifecycle.BlockEvidence{proof})
		if err != nil || route.HTTPSProxy != proxy {
			t.Fatalf("frozen route = %+v (%v)", route, err)
		}
		if proxy != "" {
			route.NoProxy[0] = "changed"
			fresh, err := ProvedEgress("controller", []lifecycle.BlockEvidence{proof})
			if err != nil || !slices.Equal(fresh.NoProxy, []string{".internal.test"}) {
				t.Fatal("route retained a mutable alias")
			}
		}
	}
}

func TestProvedEgressRefusesUnprovedOrContradictoryDependencies(t *testing.T) {
	for _, name := range []string{"absent", "pending", "destroy", "retained", "evidence", "digest", "producer", "identity", "wrong stage", "wrong controller", "duplicate", "conflict", "invalid route"} {
		t.Run(name, func(t *testing.T) {
			proof := routeProof(t, "http://proxy.example.test:3128")
			records, machine := []lifecycle.BlockEvidence{proof}, "controller"
			switch name {
			case "absent":
				records = nil
			case "pending":
				records[0].State = reconciliation.BlockPending
			case "destroy":
				records[0].Verb = reconciliation.Destroy
			case "retained":
				var e Evidence
				if err := json.Unmarshal(proof.Evidence, &e); err != nil {
					t.Fatal(err)
				}
				e.Retained = true
				records[0].Evidence, _ = e.encode()
			case "evidence":
				records[0].Evidence = json.RawMessage(`{}`)
			case "digest":
				records[0].Block.RequestDigest = "other"
			case "producer":
				records[0].Block.ContentDigest = "other"
			case "identity":
				records[0].Object = "other"
			case "wrong stage":
				records[0].Block.Stage = reconciliation.StageMachines
			case "wrong controller":
				machine = "other"
			case "duplicate":
				records = append(records, proof)
			case "conflict":
				other := routeProof(t, "http://another.example.test:3128")
				other.Block.ID = "other-controller"
				records = append(records, other)
			case "invalid route":
				records = []lifecycle.BlockEvidence{routeProof(t, "http://user:password@proxy.example.test:3128")}
			}
			if _, err := ProvedEgress(machine, records); err == nil {
				t.Fatal("unproved route was accepted")
			}
		})
	}
}
