package artifactserver

import (
	"context"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func frozenBlock(t *testing.T, objects ...api.Object) reconciliation.Block {
	t.Helper()
	plan, err := New(&fakeRunner{}, fixedClock{}).Plan(context.Background(), planInput(t, reconciliation.Apply, objects...))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	return frozen.Blocks[0]
}

func TestAContextStoreServingCertificateIsProvedWhenItsBindingIsAcquired(t *testing.T) {
	capability := New(&fakeRunner{}, fixedClock{})
	block := frozenBlock(t, controller(), artifactServer())
	uncovered, expired, client := validOptions(), validOptions(), validOptions()
	uncovered.ips = []string{"203.0.113.9"}
	expired.notAfter = testMoment.Add(-time.Hour)
	client.clientOnly = true
	for name, options := range map[string]certificateOptions{"uncovered": uncovered, "expired": expired, "client only": client} {
		t.Run(name, func(t *testing.T) {
			material, _ := issue(t, options)
			err := capability.ProveBinding(context.Background(), testContext, block, map[string]secrets.Material{testSecret: material})
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "secret.part" || reported[0].Object == nil ||
				reported[0].Object.Kind != "Secret" || reported[0].Object.Name != testSecret {
				t.Fatalf("refusal = %#v, want one secret.part on Secret/%s", reported, testSecret)
			}
			remedy := reported[0].Remediation
			if !strings.Contains(remedy, "--context "+testContext) || !strings.HasSuffix(remedy, "then apply again with bootwright apply --context "+testContext) || strings.Contains(remedy, "destroy") {
				t.Fatalf("remediation = %q, want one that applies again in this context and destroys nothing", remedy)
			}
		})
	}
	material, _ := issue(t, validOptions())
	if err := capability.ProveBinding(context.Background(), testContext, block, map[string]secrets.Material{testSecret: material}); err != nil {
		t.Fatalf("a usable certificate was refused: %v", err)
	}
	server := artifactServer(
		field("listeners", api.ListValue(api.MapValue(text("name", "http"), text("protocol", "http"), number("port", "8080")))),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip-http"), text("listenerRef", "http"), text("addressRef", "ip")))),
	)
	plain := frozenBlock(t, controller(), server.WithSpec(server.Spec().Without("tls")))
	if err := capability.ProveBinding(context.Background(), testContext, plain, map[string]secrets.Material{}); err != nil {
		t.Fatalf("a server without TLS was refused: %v", err)
	}
}

func TestMissingBoundServingMaterialNamesItsSecret(t *testing.T) {
	check := func(err error) {
		t.Helper()
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "secret.store" || reported[0].Object == nil ||
			reported[0].Object.Kind != "Secret" || reported[0].Object.Name != testSecret ||
			!strings.Contains(reported[0].Remediation, "bootwright status --context "+testContext) {
			t.Fatalf("refusal = %#v, want secret.store on Secret/%s naming status in this context", reported, testSecret)
		}
	}
	_, err := BoundServingMaterial(map[string]secrets.Material{}, testSecret, testContext)
	check(err)
	call := execution(t, secrets.Material{})
	call.Context, call.Material = testContext, map[string]secrets.Material{}
	_, err = New(&fakeRunner{}, fixedClock{}).Apply(context.Background(), call)
	check(err)
}
