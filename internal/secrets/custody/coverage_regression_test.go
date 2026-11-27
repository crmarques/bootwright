package custody

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func TestStoreBindingRetainsAllReferringEffectHolds(t *testing.T) {
	service, access, _, ledger := generatedPair(t)
	row := ledger.holds.Bindings[0]
	ledger.holds.Bindings = append(ledger.holds.Bindings, Hold{Binding: row.Binding, Operation: "other apply", Secrets: []string{"bmc-password"}})
	selected := access.snapshot.Context
	holds := service.readHolds(context.Background(), selected)
	for _, name := range []string{"passphrase", "recovery", "bmc-password"} {
		if who, held := holds.holder(access.session.snapshot, name); !held || who != row.Operation+" as effect material" {
			t.Errorf("holder of %s = %q (%t), want the first named binding with all its holds", name, who, held)
		}
	}

	access.failure = errors.New("cannot inspect bindings")
	reads := ledger.reads
	holds = service.readHolds(context.Background(), selected)
	if _, held := holds.holder(access.session.snapshot, "passphrase"); !held || ledger.reads != reads {
		t.Fatalf("failed inspection did not conservatively hold existing bindings without consulting the ledger")
	}
}

func TestReleaseInAreaDoesNotInventAnAbsentStore(t *testing.T) {
	ctx := context.Background()
	access := &serviceAccess{}
	service := Service{access: access}
	if changed, err := service.ReleaseInArea(ctx, secretstore.Context{Name: "fixture"}, nil, "binding-0"); changed || err != nil || len(access.areas) != 1 {
		t.Fatalf("release on an absent store: changed %t, error %v, transactions %d", changed, err, len(access.areas))
	}
}
