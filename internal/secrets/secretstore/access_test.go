package secretstore

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
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

func (i *testImplementation) Backend() string {
	return i.selection.Type + "-v" + strconv.Itoa(i.selection.Store.StateVersion)
}

func (i *testImplementation) Selection() Selection { return i.selection }
func (i *testImplementation) Requirements() []SessionRequirement {
	if i.needs {
		return []SessionRequirement{{Kind: "test-unlock"}}
	}
	return nil
}
func (i *testImplementation) Initialize(ctx context.Context, c Context, a Area, m SessionMaterial) (StoreSession, error) {
	if _, exists := a.(*testArea).files[RecordPath]; exists {
		i.opened++
		return i.newSession(m)
	}
	i.initialized++
	if entries, err := a.Entries(ctx, ""); err != nil || len(entries) != 0 {
		return nil, Failure("store.corrupt", "test implementation cannot recover this partial state")
	}
	s, err := i.newSession(m)
	if err != nil {
		return nil, err
	}
	b, _ := EncodeRecord(Selector{SelectorVersion: RecordVersion, ContextID: c.ID, Backend: i.Backend(), Generation: "generation-one"}, []byte(`{}`))
	a.(*testArea).files[RecordPath] = b
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
	w := &testWorkspace{area: &testArea{files: map[string][]byte{}}, selected: Context{Name: "fixture", ID: "ctx-fixture", Mode: "ready", Revision: "rev-fixture"}}
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
			access.resolver = NewCatalog(b, a)
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
		func(a *Access, _ *testWorkspace, b *testImplementation) { a.resolver = NewCatalog(b, b) },
		func(a *Access, _ *testWorkspace, _ *testImplementation) {
			a.resolver = NewCatalog(testBackend("other", false))
		},
		func(_ *Access, w *testWorkspace, _ *testImplementation) {
			w.area.files[RecordPath] = []byte(`{"selectorVersion":1}`)
		},
		func(_ *Access, w *testWorkspace, _ *testImplementation) {
			w.area.files[RecordPath] = []byte(strings.ReplaceAll(string(w.area.files[RecordPath]), "ctx-fixture", "ctx-other"))
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

type testResolver struct {
	implementation SecretStoreImplementation
	types          []string
	selected       []string
	reopened       []string
	failure        error
}

func (r *testResolver) Types() []string { return r.types }
func (r *testResolver) Select(kind string) (SecretStoreImplementation, error) {
	r.selected = append(r.selected, kind)
	return r.implementation, r.failure
}
func (r *testResolver) Reopen(selection string) (SecretStoreImplementation, error) {
	r.reopened = append(r.reopened, selection)
	return r.implementation, r.failure
}

func TestAccessUsesInjectedImplementationResolver(t *testing.T) {
	ctx := context.Background()
	backend := testBackend("independent", true)
	resolver := &testResolver{implementation: backend, types: []string{"independent"}}
	w := &testWorkspace{area: &testArea{files: map[string][]byte{}}, selected: Context{Name: "fixture", ID: "ctx-fixture", Mode: "ready"}}
	source := &testSource{}
	access := NewAccess(w, resolver, source)
	if got := access.Types(); !slices.Equal(got, []string{"independent"}) {
		t.Fatal("completion did not use the injected resolver", got)
	}
	if err := access.Initialize(ctx, w.selected, "independent", func(session StoreSession, selection Selection, created bool) error {
		if session == nil || selection != backend.selection || !created {
			t.Fatal("incorrect initialization result")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(resolver.selected, []string{"independent"}) || backend.initialized != 1 || !backend.last.closed || !source.material.closed {
		t.Fatal("initialization bypassed resolver or leaked capabilities")
	}
	callback := func(session StoreSession, selection Selection) error {
		if session == nil || selection != backend.selection {
			t.Fatal("incorrect reopened result")
		}
		return nil
	}
	if err := access.View(ctx, w.selected, true, callback); err != nil {
		t.Fatal(err)
	}
	if err := access.Mutate(ctx, w.selected, callback); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(resolver.reopened, []string{backend.Backend(), backend.Backend()}) || backend.opened != 2 || source.calls != 3 || !backend.last.closed || !source.material.closed {
		t.Fatal("reopening bypassed persisted selection or leaked capabilities")
	}
}

func TestAccessTypesDoesNotExposeResolverMetadata(t *testing.T) {
	resolver := &testResolver{types: []string{"independent"}}
	access := NewAccess(nil, resolver, nil)
	types := access.Types()
	types[0] = "changed"
	if !slices.Equal(resolver.types, []string{"independent"}) || !slices.Equal(access.Types(), []string{"independent"}) {
		t.Fatal("caller mutated resolver metadata")
	}
}

func TestResolverFailureStopsBeforeBackendAndMaterial(t *testing.T) {
	for _, operation := range []string{"initialize", "view", "mutate"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			backend := testBackend("independent", true)
			want := Failure("store.implementation", "injected resolver refused selection")
			resolver := &testResolver{implementation: backend, failure: want}
			w := &testWorkspace{area: &testArea{files: map[string][]byte{}}, selected: Context{Name: "fixture", ID: "ctx-fixture", Mode: "ready"}}
			source := &testSource{}
			access := NewAccess(w, resolver, source)
			selector, err := EncodeRecord(Selector{SelectorVersion: RecordVersion, ContextID: w.selected.ID, Backend: backend.Backend(), Generation: "fixture-generation"}, []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			w.area.files[RecordPath] = selector
			callback := func(StoreSession, Selection) error {
				t.Fatal("resolver failure reached callback")
				return nil
			}
			switch operation {
			case "initialize":
				err = access.Initialize(ctx, w.selected, "independent", func(session StoreSession, selection Selection, _ bool) error { return callback(session, selection) })
				if w.writes != 0 {
					t.Fatal("failed selection reached mutation")
				}
			case "view":
				err = access.View(ctx, w.selected, true, callback)
			case "mutate":
				err = access.Mutate(ctx, w.selected, callback)
			}
			if !errors.Is(err, want) || backend.initialized != 0 || backend.opened != 0 || source.calls != 0 {
				t.Fatal("resolver failure was ignored or acquired capabilities", err)
			}
		})
	}
}

func TestMissingResolverPreservesUninitializedInspectionAndRefusesSelection(t *testing.T) {
	var missing *ImplementationCatalog
	for name, resolver := range map[string]ImplementationResolver{"nil": nil, "typed-nil-catalog": missing} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			w := &testWorkspace{area: &testArea{files: map[string][]byte{}}, selected: Context{Name: "fixture", ID: "ctx-fixture", Mode: "ready"}}
			access := NewAccess(w, resolver, nil)
			if len(access.Types()) != 0 {
				t.Fatal("missing resolver offered completion candidates")
			}
			if err := access.View(ctx, w.selected, false, func(session StoreSession, selection Selection) error {
				if session != nil || selection != (Selection{}) {
					t.Fatal("uninitialized inspection returned a selected session")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			err := access.Initialize(ctx, w.selected, "independent", func(StoreSession, Selection, bool) error {
				t.Fatal("missing resolver reached initialization callback")
				return nil
			})
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "secret.store.implementation" || w.writes != 0 {
				t.Fatal("missing resolver did not safely refuse initialization", err)
			}
			selector, err := EncodeRecord(Selector{SelectorVersion: RecordVersion, ContextID: w.selected.ID, Backend: testBackend("independent", false).Backend(), Generation: "fixture-generation"}, []byte(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			w.area.files[RecordPath] = selector
			err = access.View(ctx, w.selected, true, func(StoreSession, Selection) error {
				t.Fatal("missing resolver reached reopen callback")
				return nil
			})
			found = diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "secret.store.implementation" || w.writes != 0 {
				t.Fatal("missing resolver did not safely refuse reopening", err)
			}
		})
	}
}

func FuzzCanonicalStoreRecord(f *testing.F) {
	valid, _ := EncodeRecord(Selector{SelectorVersion: RecordVersion, ContextID: "ctx-test", Backend: testBackend("selected", false).Backend(), Generation: "gen-test"}, []byte(`{}`))
	f.Add(valid)
	f.Add([]byte(`{"version":2,"version":3}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			return
		}
		if record, err := DecodeRecord(data, "ctx-test"); err == nil {
			encoded, err := EncodeRecord(record.Selector, record.Payload)
			if err != nil || string(encoded) != string(data) {
				t.Fatal("noncanonical record accepted")
			}
		}
	})
}
