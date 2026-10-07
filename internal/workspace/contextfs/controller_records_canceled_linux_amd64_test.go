//go:build linux && amd64

package contextfs

import (
	"context"
	"reflect"
	"strings"
	"testing"

	p "github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// twoActionState is a pending receipt of setup's own that plans its execution
// bundle and its native transaction, each in the phase given.
func twoActionState(t *testing.T, bundle, native string) p.HostState {
	t.Helper()
	value := syntheticControllerState(t, p.SetupContext{})
	value.Receipt.Actions = []p.SetupAction{
		{ID: "execution-bundle", Request: []byte(`{"catalogDigest":"` + value.Receipt.CatalogDigest + `","readyBefore":false}`), Phase: bundle, Evidence: []byte(`{}`)},
		{ID: "container-runtime", Request: []byte(`{"readyBefore":false,"version":"latest"}`), Phase: native, Evidence: []byte(`{}`)},
	}
	var err error
	value.Receipt.PlanDigest, err = p.SetupPlanDigest(value.Host, value.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// published fails the test unless the store holds exactly value's receipt.
func published(t *testing.T, store *Store, value p.HostState) {
	t.Helper()
	if err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		if !reflect.DeepEqual(view.State.Receipt, value.Receipt) {
			t.Fatalf("the store holds %#v, want %#v", view.State.Receipt, value.Receipt)
		}
		return nil
	}); err != nil {
		t.Fatalf("the record is unreadable: %#v", diagnostics.Of(err))
	}
}

// The store accepts each cancellation setup records below the bound (D93) and
// the fresh receipt that replaces it. A setup whose native transaction never
// started keeps its published bundle's observation and its preparation, and
// its native action is observed canceled over an unchanged inventory; a
// setup that never started has its intended bundle observed canceled over an
// unsealed area. The store never lets a cancellation drop that preparation.
func TestTheStoreAcceptsACancellationOfANativeActionThatNeverStarted(t *testing.T) {
	t.Run("a native transaction that never started", func(t *testing.T) {
		store, _ := fixture(t)
		value := twoActionState(t, "intent", "planned")
		publishControllerState(t, store, p.SetupContext{}, value)
		value.Receipt.Actions[0].Phase, value.Receipt.Actions[0].Outcome = "observed", "changed"
		value.Receipt.Actions[0].Evidence = []byte(`{"postcondition":"verified"}`)
		value.Receipt.Actions[1].Phase = "intent"
		publishControllerState(t, store, p.SetupContext{}, value)
		value.Receipt.Actions[1].Preparation = []byte(`{"addedSources":[],"afterInventorySHA256":"` + strings.Repeat("f", 64) + `","inventorySHA256":"` + strings.Repeat("d", 64) + `","planDigest":"` + strings.Repeat("c", 64) + `","transitionsSHA256":"` + strings.Repeat("b", 64) + `"}`)
		publishControllerState(t, store, p.SetupContext{}, value)
		dropped := cloneControllerState(value)
		dropped.Receipt.Actions[1].Phase, dropped.Receipt.Actions[1].Outcome = "observed", "canceled"
		dropped.Receipt.Actions[1].Evidence = []byte(`{"nativeInventory":"unchanged"}`)
		dropped.Receipt.Actions[1].Preparation = nil
		dropped.Receipt.Status = "canceled"
		if err := store.MutateController(context.Background(), p.SetupContext{}, false, func(tx p.StorageTransaction) error {
			if outcome, err := tx.Publish(context.Background(), dropped); err == nil || outcome != p.NotCommitted {
				t.Fatal("a cancellation dropped the native preparation")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		canceled := cloneControllerState(value)
		canceled.Receipt.Actions[1].Phase, canceled.Receipt.Actions[1].Outcome = "observed", "canceled"
		canceled.Receipt.Actions[1].Evidence = []byte(`{"nativeInventory":"unchanged"}`)
		canceled.Receipt.Status = "canceled"
		publishControllerState(t, store, p.SetupContext{}, canceled)
		published(t, store, canceled)
		fresh := twoActionState(t, "planned", "planned")
		fresh.Receipt.ID = "setup-" + strings.Repeat("2", 32)
		publishControllerState(t, store, p.SetupContext{}, fresh)
		published(t, store, fresh)
	})
	t.Run("a setup that never started over an unsealed area", func(t *testing.T) {
		store, _ := fixture(t)
		value := twoActionState(t, "intent", "planned")
		publishControllerState(t, store, p.SetupContext{}, value)
		canceled := cloneControllerState(value)
		canceled.Receipt.Actions[0].Phase, canceled.Receipt.Actions[0].Outcome = "observed", "canceled"
		canceled.Receipt.Actions[0].Evidence = []byte(`{"bundleArea":"unsealed"}`)
		canceled.Receipt.Status = "canceled"
		publishControllerState(t, store, p.SetupContext{}, canceled)
		published(t, store, canceled)
	})
}
