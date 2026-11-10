package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// A completed apply holds as effect material every Secret its blocks
// reference other than an access credential they present, under the binding
// its operation names, and a context at rest is no registration.
func TestSecretHoldsNameTheEffectMaterialACompletedApplyBound(t *testing.T) {
	alpha := definition("alpha")
	alpha.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"alpha","passphraseRef":"passphrase"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{alpha})
	h.capability.secrets = []string{"bmc", "passphrase"}
	h.capability.access = map[string][]string{"alpha": {"bmc"}}
	h.binder.material = map[string]secrets.Material{"bmc": password("first"), "passphrase": password("disk")}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("%#v", diagnostics.Of(err))
	}
	applied, _ := durableOperation(t, h)
	holds, err := h.service.SecretHolds(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	if holds.Registering || len(holds.Bindings) != 1 || holds.Bindings[0].Binding != applied.Bindings[0] || holds.Bindings[0].All ||
		!slices.Equal(holds.Bindings[0].Secrets, []string{"passphrase"}) || holds.Bindings[0].Operation != "the apply "+applied.ID+" (done)" {
		t.Fatalf("holds = %+v, want the passphrase alone under %v", holds, applied.Bindings)
	}
}
