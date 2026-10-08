package encryption

import (
	"context"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type leaseTrackingAccess struct {
	*serviceAccess
	active bool
	events []string
}

func (a *leaseTrackingAccess) View(ctx context.Context, selected secretstore.Context, unlock bool, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.events = append(a.events, "view")
	return a.serviceAccess.View(ctx, selected, unlock, func(session secretstore.StoreSession, selection secretstore.Selection) error {
		a.active = true
		defer func() { a.active = false }()
		return callback(session, selection)
	})
}

func (a *leaseTrackingAccess) Mutate(ctx context.Context, selected secretstore.Context, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.events = append(a.events, "mutate")
	return a.serviceAccess.Mutate(ctx, selected, func(session secretstore.StoreSession, selection secretstore.Selection) error {
		a.active = true
		defer func() { a.active = false }()
		return callback(session, selection)
	})
}

type leaseObservingConfirmer struct {
	access     *leaseTrackingAccess
	heldAtAsk  []bool
	duringAsk  func()
	confirmErr error
}

func (c *leaseObservingConfirmer) Confirm(context.Context, string, string) error {
	c.heldAtAsk = append(c.heldAtAsk, c.access.active)
	if c.duringAsk != nil {
		c.duringAsk()
	}
	return c.confirmErr
}

func rotationPromptFixture() (*Service, *leaseTrackingAccess, *leaseObservingConfirmer) {
	_, inner := serviceFixture()
	inner.session.snapshot = &secretstore.Snapshot{ActiveKey: "fixture-key", Keys: []secretstore.Key{{ID: "fixture-key", State: "active"}}}
	access := &leaseTrackingAccess{serviceAccess: inner}
	confirmer := &leaseObservingConfirmer{access: access}
	return New(access, confirmer), access, confirmer
}

func TestARotationIsConfirmedWithNoStoreReadOrLeaseHeld(t *testing.T) {
	service, access, confirmer := rotationPromptFixture()
	rotated, err := service.Rotate(context.Background(), EncryptionRotateRequest{ContextName: "fixture"})
	if err != nil || rotated.ActiveKey != "rotated-key" || access.session.rotations != 1 {
		t.Fatalf("confirmed rotation = %+v, %v, %d rotations", rotated, err, access.session.rotations)
	}
	if len(confirmer.heldAtAsk) != 1 || confirmer.heldAtAsk[0] {
		t.Fatalf("the rotation prompt ran inside a store callback: %v", confirmer.heldAtAsk)
	}
	if len(access.events) != 2 || access.events[0] != "view" || access.events[1] != "mutate" {
		t.Fatalf("store accesses = %v, want a shared read then the lease", access.events)
	}
}

func TestARotationWithYesTakesOneTransactionAndAsksNothing(t *testing.T) {
	service, access, confirmer := rotationPromptFixture()
	if _, err := service.Rotate(context.Background(), EncryptionRotateRequest{ContextName: "fixture", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(access.events) != 1 || access.events[0] != "mutate" || len(confirmer.heldAtAsk) != 0 {
		t.Fatalf("store accesses = %v and %d prompts, want the lease alone", access.events, len(confirmer.heldAtAsk))
	}
}

func TestARotationWhoseKeyChangedDuringItsPromptRotatesNothing(t *testing.T) {
	service, access, confirmer := rotationPromptFixture()
	confirmer.duringAsk = func() { access.session.snapshot.ActiveKey = "another-key" }
	_, err := service.Rotate(context.Background(), EncryptionRotateRequest{ContextName: "fixture"})
	want := []diagnostics.Diagnostic{{Severity: "error", Code: "secret.store.conflict",
		Message:     "the secret encryption key of context fixture changed while its rotation was being confirmed; nothing was rotated",
		Remediation: "review it with bootwright secret encryption status --context fixture, then repeat bootwright secret encryption rotate --context fixture"}}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
		t.Fatalf("a rotation whose key changed = %+v, want %+v", reported, want)
	}
	if access.session.rotations != 0 {
		t.Fatalf("rotated %d times after the key changed", access.session.rotations)
	}
}
