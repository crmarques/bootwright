package instance

import (
	"context"
	"strings"
	"testing"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func clusterBlock(kind, object string) reconciliation.Block {
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: "install-" + object, Kind: kind, Object: object}}
}

func TestAnAdditionIsAdmittedOnlyOnAClusterTheLineageInstalled(t *testing.T) {
	block := fixtureBlock(t, fixtureRequest(t))
	capability := New(nil, fixtureRegistry(t), nil)
	installed := reconciliation.Plan{Blocks: []reconciliation.Block{clusterBlock("OpenShiftCluster", "cluster")}}
	if err := capability.AdmitAdded(installed, block); err != nil {
		t.Fatalf("an instance on an installed cluster: %v", err)
	}
	for name, lineage := range map[string]reconciliation.Plan{
		"another cluster":  {Blocks: []reconciliation.Block{clusterBlock("OpenShiftCluster", "other")}},
		"another kind":     {Blocks: []reconciliation.Block{clusterBlock("Machine", "cluster")}},
		"an empty lineage": {},
	} {
		err := capability.AdmitAdded(lineage, block)
		if err == nil || !strings.Contains(diagnostics.Of(err)[0].Message, "did not install the cluster cluster") {
			t.Errorf("%s: admitted or refused otherwise: %v", name, err)
		}
	}
	bad := block
	bad.Request = []byte(`{}`)
	if err := capability.AdmitAdded(installed, bad); err == nil {
		t.Error("an undecodable request was admitted")
	}
}

func TestAnInstanceReadsBoundSecretsOnlyInItsOwnApplyAttempts(t *testing.T) {
	request := fixtureRequest(t)
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := co.NewPlacementEntry(co.SecretPlacement{Secret: "credentials", Namespace: "fixture-operator", Name: "credentials", Type: "Opaque", Keys: []co.KeyPart{{Key: "password", Part: "password"}}})
	if err != nil {
		t.Fatal(err)
	}
	program.Steps = append(program.Steps, co.Step{Kind: "place", Place: &entry})
	if request.Program, err = co.Freeze(program, co.Options{Namespace: "fixture-operator"}); err != nil {
		t.Fatal(err)
	}
	request.Secrets = []string{"credentials"}
	execution := fixtureExecution(t, request)
	execution.Material = map[string]secrets.Material{"credentials": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte("password")})}
	defer execution.Material["credentials"].Clear()
	runner := &scriptedRunner{}
	capability := New(runner, fixtureRegistry(t), nil)
	if !capability.ReadsMaterialOnlyInApply(execution.Block) {
		t.Fatal("the capability does not state the premise an addition requires")
	}
	ctx := context.Background()
	execution.ReadOnly = true
	_, _ = capability.Observe(ctx, execution)
	_, _ = capability.Destroy(ctx, execution)
	_, _ = capability.ObserveRemoval(ctx, execution)
	_, _ = capability.Quiescent(ctx, lifecycle.Probe{Block: execution.Block, Material: execution.Material})
	if len(runner.calls) != 1 {
		t.Fatalf("the read paths made %d adapter calls, want only the observation", len(runner.calls))
	}
	if _, lent := runner.calls[0].Material["credentials"]; lent || len(runner.calls[0].Material) != 1 {
		t.Fatalf("the observation was lent %v", runner.calls[0].Material)
	}
	execution.ReadOnly = false
	_, _ = capability.Apply(ctx, execution)
	if _, lent := runner.calls[1].Material["credentials"]; !lent {
		t.Fatal("the apply attempt was not lent the placed Secret")
	}
}

func TestAnUnresolvedAddedInstanceNamesTheContinuationNotADestroy(t *testing.T) {
	request := fixtureRequest(t)
	block := fixtureBlock(t, request)
	evidence := fixtureEvidence(t, request, block.RequestDigest)
	evidence.Observations[0].Ownership = co.Foreign
	capability := New(nil, fixtureRegistry(t), nil)
	plain, ok := capability.Unresolved(reconciliation.Apply, block, encoded(t, evidence))
	if !ok || plain.Remedy != "destroy and apply again" {
		t.Fatalf("a plain apply's instance is explained %+v", plain)
	}
	added, ok := capability.UnresolvedAdded(reconciliation.Apply, block, encoded(t, evidence))
	if !ok || added.Reason != plain.Reason || strings.Contains(added.Remedy, "destroy") || added.Remedy == "" {
		t.Fatalf("an added instance is explained %+v beside %+v", added, plain)
	}
	if _, ok := capability.UnresolvedAdded(reconciliation.Destroy, block, encoded(t, evidence)); ok {
		t.Fatal("a removal was explained as an addition")
	}
}
