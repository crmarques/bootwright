package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
)

type testArea struct {
	Area
	files map[string][]byte
}

func (a *testArea) ReadMutable(_ context.Context, name string, _ int) ([]byte, bool, error) {
	b, ok := a.files[name]
	return append([]byte(nil), b...), ok, nil
}
func (a *testArea) Entries(context.Context, string) ([]Entry, error) {
	result := []Entry{}
	for name, b := range a.files {
		result = append(result, Entry{Name: name, Size: int64(len(b))})
	}
	return result, nil
}

type testWorkspace struct {
	area          *testArea
	reads, writes int
	selected      Context
}

func (w *testWorkspace) SecretContext(context.Context, string) (ContextSnapshot, error) {
	return ContextSnapshot{Context: w.selected}, nil
}
func (w *testWorkspace) ReadSecrets(_ context.Context, c Context, callback func(Area) error) error {
	if c != w.selected {
		return errors.New("changed context")
	}
	w.reads++
	return callback(w.area)
}
func (w *testWorkspace) MutateSecrets(_ context.Context, c Context, callback func(Area) error) error {
	if c != w.selected {
		return errors.New("changed context")
	}
	w.writes++
	return callback(w.area)
}

type testMaterial struct{ closed bool }

func (m *testMaterial) Close() error { m.closed = true; return nil }

type testSource struct {
	calls    int
	material *testMaterial
}

func (s *testSource) Acquire(_ context.Context, _ Context, _ Selection, _ []SessionRequirement) (SessionMaterial, error) {
	s.calls++
	s.material = &testMaterial{}
	return s.material, nil
}

type testSession struct {
	StoreSession
	closed bool
}

func (s *testSession) Close() error { s.closed = true; return nil }
func (s *testSession) Read(context.Context, string) (secrets.Material, error) {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-confidential")}), nil
}

type testImplementation struct {
	selection           Selection
	needs               bool
	initialized, opened int
	last                *testSession
}

func (i *testImplementation) Selection() Selection { return i.selection }
func (i *testImplementation) Requirements() []SessionRequirement {
	if i.needs {
		return []SessionRequirement{{Kind: "test-unlock"}}
	}
	return nil
}
func (i *testImplementation) Initialize(ctx context.Context, c Context, a Area, m SessionMaterial) (StoreSession, error) {
	i.initialized++
	if entries, err := a.Entries(ctx, ""); err != nil || len(entries) != 0 {
		return nil, Failure("store.corrupt", "test implementation cannot recover this partial state")
	}
	s, err := i.newSession(m)
	if err != nil {
		return nil, err
	}
	b, _ := EncodeCanonical(Selector{SelectorVersion: 1, ContextID: c.ID, Selection: i.selection, Generation: "generation-one"})
	a.(*testArea).files["selector.json"] = b
	return s, nil
}
func (i *testImplementation) Open(_ context.Context, _ Context, _ Area, _ Selector, m SessionMaterial) (StoreSession, error) {
	i.opened++
	return i.newSession(m)
}
func (i *testImplementation) newSession(m SessionMaterial) (StoreSession, error) {
	if i.needs {
		if _, ok := m.(*testMaterial); !ok {
			return nil, Failure("store.key-unavailable", "locked")
		}
	}
	i.last = &testSession{}
	return i.last, nil
}
func testBackend(kind string, needs bool) *testImplementation {
	ref := ComponentRef{ID: kind, InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1}
	return &testImplementation{selection: Selection{Type: kind, Store: ref, KeyCustody: ref}, needs: needs}
}
func testAccess(backends ...SecretStoreImplementation) (*Access, *testWorkspace, *testSource) {
	w := &testWorkspace{area: &testArea{files: map[string][]byte{}}, selected: Context{Name: "fixture", ID: "ctx-fixture", Mode: "active", Revision: "rev-fixture"}}
	s := &testSource{}
	return NewAccess(w, NewCatalog(backends...), s), w, s
}

func TestSelectedImplementationAndSessionCapability(t *testing.T) {
	for _, needs := range []bool{false, true} {
		t.Run(map[bool]string{false: "unattended", true: "session"}[needs], func(t *testing.T) {
			a := testBackend("first", false)
			b := testBackend("second", needs)
			access, w, source := testAccess(a, b)
			callback := func(session StoreSession, _ Selection, _ bool) error {
				material, err := session.Read(context.Background(), "version")
				material.Clear()
				return err
			}
			if err := access.Initialize(context.Background(), w.selected, "second", callback); err != nil {
				t.Fatal(err)
			}
			if a.initialized != 0 || a.opened != 0 || b.initialized != 1 || !b.last.closed {
				t.Fatal("selection or session lifetime violated")
			}
			if needs && (source.calls != 1 || !source.material.closed) {
				t.Fatal("unlock capability was not acquired and closed")
			}
			// Reorder registration: reopening must follow persisted identity.
			access.catalog = NewCatalog(b, a)
			if err := access.View(context.Background(), w.selected, true, func(s StoreSession, _ Selection) error { return callback(s, Selection{}, false) }); err != nil {
				t.Fatal(err)
			}
			if a.opened != 0 || b.opened != 1 || !b.last.closed {
				t.Fatal("reopening changed selected implementation")
			}
			if err := access.Initialize(context.Background(), w.selected, "second", callback); err != nil || b.initialized != 1 {
				t.Fatal("same-type initialization not idempotent", err)
			}
			if err := access.Initialize(context.Background(), w.selected, "first", callback); err == nil || a.initialized != 0 {
				t.Fatal("implicit migration allowed")
			}
		})
	}
}

func TestImplementationAndSelectorRefusalBeforeBackend(t *testing.T) {
	for _, mutate := range []func(*Access, *testWorkspace, *testImplementation){
		func(a *Access, _ *testWorkspace, b *testImplementation) { a.catalog = NewCatalog(b, b) },
		func(a *Access, _ *testWorkspace, _ *testImplementation) {
			a.catalog = NewCatalog(testBackend("other", false))
		},
		func(_ *Access, w *testWorkspace, _ *testImplementation) {
			w.area.files["selector.json"] = []byte(`{"selectorVersion":1}`)
		},
		func(_ *Access, w *testWorkspace, _ *testImplementation) {
			w.area.files["selector.json"] = []byte(strings.ReplaceAll(string(w.area.files["selector.json"]), "ctx-fixture", "ctx-other"))
		},
		func(_ *Access, _ *testWorkspace, b *testImplementation) { b.selection.Store.StateVersion++ },
	} {
		b := testBackend("selected", false)
		access, w, _ := testAccess(b)
		if err := access.Initialize(context.Background(), w.selected, "selected", func(StoreSession, Selection, bool) error { return nil }); err != nil {
			t.Fatal(err)
		}
		mutate(access, w, b)
		if err := access.View(context.Background(), w.selected, true, func(StoreSession, Selection) error { t.Fatal("refused input reached caller"); return nil }); err == nil {
			t.Fatal("invalid implementation/selector accepted")
		}
		if b.opened != 0 {
			t.Fatal("invalid selection opened a backend")
		}
	}
}

func TestStatusNeverAcquiresCredentialAndFailuresCloseSessions(t *testing.T) {
	b := testBackend("session", true)
	access, w, source := testAccess(b)
	if err := access.Initialize(context.Background(), w.selected, "session", func(StoreSession, Selection, bool) error { return nil }); err != nil {
		t.Fatal(err)
	}
	before := source.calls
	if err := access.View(context.Background(), w.selected, false, func(StoreSession, Selection) error { return nil }); err == nil {
		t.Fatal("locked backend claimed ready")
	}
	if source.calls != before {
		t.Fatal("metadata status acquired unlock material")
	}
	want := errors.New("callback failed")
	if err := access.Mutate(context.Background(), w.selected, func(StoreSession, Selection) error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if !source.material.closed || !b.last.closed {
		t.Fatal("failed operation leaked a session")
	}
}

func TestUninitializedVersusOrphanedStore(t *testing.T) {
	a, w, _ := testAccess(testBackend("selected", false))
	if err := a.View(context.Background(), w.selected, false, func(s StoreSession, _ Selection) error {
		if s != nil {
			t.Fatal("empty area initialized")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if w.writes != 0 {
		t.Fatal("read created storage")
	}
	w.area.files["orphan"] = []byte("not a fresh store")
	if err := a.View(context.Background(), w.selected, false, func(StoreSession, Selection) error {
		t.Fatal("partial state reached a read callback")
		return nil
	}); err == nil {
		t.Fatal("inspection accepted a missing selector in nonempty state")
	}
	if err := a.Initialize(context.Background(), w.selected, "selected", func(StoreSession, Selection, bool) error { return nil }); err == nil {
		t.Fatal("orphaned storage overwritten")
	}
}

func TestTypedNilImplementationFailsClosed(t *testing.T) {
	var missing *testImplementation
	catalog := NewCatalog(missing, testBackend("configured", false))
	if len(catalog.Types()) != 0 {
		t.Fatal("invalid catalog offered completion candidates")
	}
	if _, err := catalog.Select("configured"); err == nil {
		t.Fatal("catalog with a missing implementation was usable")
	}
}

func FuzzCanonicalSelector(f *testing.F) {
	valid, _ := EncodeCanonical(Selector{SelectorVersion: 1, ContextID: "ctx-test", Selection: testBackend("selected", false).selection, Generation: "gen-test"})
	f.Add(valid)
	f.Add([]byte(`{"selectorVersion":1,"selectorVersion":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			return
		}
		var s Selector
		if DecodeCanonical(data, &s) == nil {
			encoded, err := EncodeCanonical(s)
			if err != nil || string(encoded) != string(data) {
				t.Fatal("noncanonical record accepted")
			}
		}
	})
}
