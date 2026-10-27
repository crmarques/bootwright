package networking

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects/driver"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type networkRunner struct{ result lifecycle.RunResult }

func (r networkRunner) Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
	return r.result, nil
}
func TestNetworkPartialReceiptsSurviveButUnsafeEvidenceDoesNot(t *testing.T) {
	request, err := renderRequest(clusterFixture(), "controller")
	if err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: BlockID(request.Cluster), Request: data}, RequestDigest: strings.Repeat("a", 64)}
	execution := lifecycle.Execution{Block: block, ApplyOperation: "operation", ApplyRegistered: "2026-01-01T00:00:00Z", LendProduced: func(context.Context, lifecycle.ProducedInput) (secrets.Material, error) {
		return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("private")}), nil
	}, LocateTool: func(context.Context, controller.InstalledTool) (string, error) { return "/qualified/oc", nil }}
	var owned *co.ObjectEntry
	for _, step := range program.Steps {
		if step.Create != nil {
			owned = step.Create
			break
		}
	}
	if owned == nil {
		t.Fatal("no create")
	}
	for _, valid := range []bool{true, false} {
		evidence := driver.Evidence{Version: evidenceVersion, Request: block.RequestDigest, Classification: "partial", Evidence: co.Evidence{Receipts: []co.Receipt{{Ref: owned.Ref, UID: "uid", DesiredDigest: owned.DesiredDigest, Disposition: "created", Basis: co.Created}}, OLM: []co.OLMFacts{}, Dependents: []co.DependentFacts{}, Checks: []co.CheckResult{}, Observations: []co.ObjectObservation{}}}
		if !valid {
			evidence.Receipts[0].UID = "unsafe\nvalue"
		}
		raw, err := co.Canonical(evidence)
		if err != nil {
			t.Fatal(err)
		}
		capability := New(networkRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: raw}}, addons.Registry{})
		applied, err := capability.Apply(context.Background(), execution)
		if err == nil {
			t.Fatal("partial apply reported complete")
		}
		observed, err := capability.Observe(context.Background(), execution)
		if err != nil {
			t.Fatal(err)
		}
		for _, kept := range [][]byte{applied.Evidence, observed.Evidence} {
			if valid && string(kept) != string(raw) {
				t.Fatal("valid partial receipt was discarded")
			}
			if !valid && len(kept) > 0 {
				t.Fatal("unsafe native evidence persisted")
			}
		}
	}
}
