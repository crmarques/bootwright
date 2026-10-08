package custody

import (
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type promptLeaseAccess struct {
	*serviceAccess
	active bool
	events []string
}

func (a *promptLeaseAccess) View(ctx context.Context, selected secretstore.Context, unlock bool, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.events = append(a.events, "view")
	return a.serviceAccess.View(ctx, selected, unlock, func(session secretstore.StoreSession, selection secretstore.Selection) error {
		a.active = true
		defer func() { a.active = false }()
		return callback(session, selection)
	})
}

func (a *promptLeaseAccess) Mutate(ctx context.Context, selected secretstore.Context, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.events = append(a.events, "mutate")
	return a.serviceAccess.Mutate(ctx, selected, func(session secretstore.StoreSession, selection secretstore.Selection) error {
		a.active = true
		defer func() { a.active = false }()
		return callback(session, selection)
	})
}

type promptLeaseConfirmer struct {
	access    *promptLeaseAccess
	heldAtAsk []bool
	duringAsk func()
}

func (c *promptLeaseConfirmer) Confirm(context.Context, string, string) error {
	c.heldAtAsk = append(c.heldAtAsk, c.access.active)
	if c.duringAsk != nil {
		c.duringAsk()
	}
	return nil
}

func promptLeaseFixture(t *testing.T) (*Service, *promptLeaseAccess, *promptLeaseConfirmer, *serviceMaterial) {
	t.Helper()
	base, inner, material, _ := serviceFixture(t, declarationYAML("token", ""))
	access := &promptLeaseAccess{serviceAccess: inner}
	confirmer := &promptLeaseConfirmer{access: access}
	service := New(access, base.compiler, material, confirmer)
	if _, err := service.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueFile: "explicit"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	access.events = nil
	return service, access, confirmer, material
}

func storeAnotherVersion(t *testing.T, session *serviceSession) {
	t.Helper()
	d := secrets.Declaration{Name: "token", Type: "token", Source: "contextStore"}
	if _, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: d, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("another-value")})}}); err != nil {
		t.Fatal(err)
	}
}

func TestSecretPromptsHoldNoStoreReadAndNoLease(t *testing.T) {
	for name, invoke := range map[string]func(*Service) error{
		"replace": func(s *Service) error {
			_, err := s.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueFile: "other"}})
			return err
		},
		"delete": func(s *Service) error {
			_, err := s.Delete(context.Background(), DeleteRequest{Name: "token"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, access, confirmer, _ := promptLeaseFixture(t)
			if err := invoke(service); err != nil {
				t.Fatal(err, diagnostics.Of(err))
			}
			if !slices.Equal(confirmer.heldAtAsk, []bool{false}) {
				t.Fatalf("prompts held a store callback: %v", confirmer.heldAtAsk)
			}
			if !slices.Equal(access.events, []string{"view", "mutate"}) {
				t.Fatalf("store accesses = %v, want a shared read then the lease", access.events)
			}
		})
	}
}

func TestSecretChangesWithYesTakeOneTransaction(t *testing.T) {
	service, access, confirmer, _ := promptLeaseFixture(t)
	if _, err := service.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueFile: "other"}, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Delete(context.Background(), DeleteRequest{Name: "token", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(access.events, []string{"mutate", "mutate"}) || len(confirmer.heldAtAsk) != 0 {
		t.Fatalf("store accesses = %v and %d prompts, want one lease each and no prompt", access.events, len(confirmer.heldAtAsk))
	}
}

func TestADeleteOfAnAbsentSecretAsksNothingAndTakesNoLease(t *testing.T) {
	service, access, confirmer, _ := promptLeaseFixture(t)
	result, err := service.Delete(context.Background(), DeleteRequest{Name: "absent"})
	if err != nil || result.Unchanged != 1 || len(confirmer.heldAtAsk) != 0 || !slices.Equal(access.events, []string{"view"}) {
		t.Fatalf("delete of an absent Secret = %+v, %v, prompts %v, accesses %v", result, err, confirmer.heldAtAsk, access.events)
	}
}

func TestASecretChangedDuringItsConfirmationIsRefusedAndNothingWritten(t *testing.T) {
	for name, invoke := range map[string]func(*Service) error{
		"replace": func(s *Service) error {
			_, err := s.Set(context.Background(), SetRequest{Name: "token", Input: secrets.Input{ValueFile: "other"}})
			return err
		},
		"delete": func(s *Service) error {
			_, err := s.Delete(context.Background(), DeleteRequest{Name: "token"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, access, confirmer, material := promptLeaseFixture(t)
			session := access.session
			writes := -1
			confirmer.duringAsk = func() {
				storeAnotherVersion(t, session)
				writes = session.writes
			}
			acquired := material.acquired
			err := invoke(service)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || reported[0].Code != "secret.store.conflict" || reported[0].Object == nil || reported[0].Object.Name != "token" {
				t.Fatalf("a Secret changed during its prompt = %+v", reported)
			}
			current, exists := currentVersion(session.snapshot, "token")
			if !exists || current.ID != "version-1" || session.writes != writes || material.acquired != acquired {
				t.Fatalf("the refused command wrote or read: current %v %v, acquired %d", current, exists, material.acquired-acquired)
			}
		})
	}
}
