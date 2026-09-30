package secretstore

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const retiredRemedy = "destroy this context's effects with the Bootwright build that created it, then run bootwright context delete --name <context> --purge and create the context again"

// A store whose persisted backend this build's catalog no longer carries is
// refused by every access with the identity it read and the way out, before
// any session opens or any unlock material is acquired.
func TestAnUnknownPersistedBackendRefusesWithItsIdentityAndRemedy(t *testing.T) {
	current := testBackend("current", true)
	access, w, source := testAccess(current)
	record, err := EncodeRecord(Selector{SelectorVersion: RecordVersion, Context: "fixture", Backend: "retired-v3", Generation: "generation-one"}, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	w.area.files[RecordPath] = record
	entered := 0
	callback := func(StoreSession, Selection) error { entered++; return nil }
	for name, attempt := range map[string]func() error{
		"view":       func() error { return access.View(context.Background(), w.selected, true, callback) },
		"mutate":     func() error { return access.Mutate(context.Background(), w.selected, callback) },
		"mutateArea": func() error { return access.MutateArea(context.Background(), w.selected, w.area, callback) },
		"initialize": func() error { return access.InitializeArea(context.Background(), w.selected, "current", w.area) },
	} {
		found := diagnostics.Of(attempt())
		if len(found) != 1 || found[0].Code != "secret.store.implementation" || !strings.Contains(found[0].Message, "retired-v3") || found[0].Remediation != retiredRemedy {
			t.Fatalf("%s refused as %+v", name, found)
		}
	}
	if entered != 0 || source.calls != 0 || current.opened != 0 || current.initialized != 0 {
		t.Fatalf("a refused store was opened: callbacks %d, unlocks %d, opens %d, initializations %d", entered, source.calls, current.opened, current.initialized)
	}
	other := testBackend("other", false)
	access, w, _ = testAccess(current, other)
	w.area.files[RecordPath], _ = EncodeRecord(Selector{SelectorVersion: RecordVersion, Context: "fixture", Backend: other.Backend(), Generation: "generation-one"}, []byte(`{}`))
	if found := diagnostics.Of(access.InitializeArea(context.Background(), w.selected, "current", w.area)); len(found) != 1 || found[0].Message != "initialization cannot change an existing store implementation" {
		t.Fatalf("a known but different backend refused as %+v", found)
	}
}

// MutateArea opens the store inside an area its caller already holds, so it
// takes no lock of its own; it admits only a ready context and hands a store
// never initialized to its callback as a nil session.
func TestMutateAreaOpensOnlyAReadyStoreAndPassesNilWhenUninitialized(t *testing.T) {
	backend := testBackend("current", false)
	access, w, _ := testAccess(backend)
	var sessions []StoreSession
	callback := func(session StoreSession, _ Selection) error { sessions = append(sessions, session); return nil }
	if err := access.MutateArea(context.Background(), w.selected, w.area, callback); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0] != nil || backend.opened != 0 {
		t.Fatalf("an uninitialized store was handed %v after %d opens", sessions, backend.opened)
	}
	if err := access.InitializeArea(context.Background(), w.selected, "current", w.area); err != nil {
		t.Fatal(err)
	}
	if err := access.MutateArea(context.Background(), w.selected, w.area, callback); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[1] == nil || !backend.last.closed {
		t.Fatalf("an initialized store was handed %v and left open", sessions)
	}
	if w.reads != 0 || w.writes != 0 {
		t.Fatalf("the held area was reacquired: %d reads, %d writes", w.reads, w.writes)
	}
	initializing := w.selected
	initializing.Mode = "initializing"
	if found := diagnostics.Of(access.MutateArea(context.Background(), initializing, w.area, callback)); len(found) != 1 || found[0].Code != "secret.store.conflict" || len(sessions) != 2 {
		t.Fatalf("a context that is not ready was admitted: %+v", found)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := access.MutateArea(ctx, w.selected, w.area, callback); err != context.Canceled || len(sessions) != 2 {
		t.Fatalf("a canceled access returned %v", err)
	}
}
