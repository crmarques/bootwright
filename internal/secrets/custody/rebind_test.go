package custody

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// rebindSession is the fixture's session with the optional Rebind and a Release
// that drops a binding, recording what a rebind received.
type rebindSession struct {
	*serviceSession
	binding string
	inputs  []secretstore.BoundInput
}

func (s *rebindSession) Rebind(_ context.Context, binding string, replacements []secretstore.BoundInput) (secretstore.Binding, error) {
	s.writes++
	s.binding = binding
	result := secretstore.Binding{ID: binding}
	for _, input := range replacements {
		s.inputs = append(s.inputs, secretstore.BoundInput{Declaration: input.Declaration, Version: input.Version})
		result.Versions = append(result.Versions, input.Version)
	}
	return result, nil
}

func (s *rebindSession) Release(_ context.Context, id string) (bool, error) {
	s.writes++
	for i, binding := range s.snapshot.Bindings {
		if binding.ID == id {
			s.snapshot.Bindings = slices.Delete(s.snapshot.Bindings, i, i+1)
			return true, nil
		}
	}
	return false, nil
}

// sessionAccess serves one chosen session through the fixture's access, so a
// test can lend a session of another shape than the fixture's own.
type sessionAccess struct {
	*serviceAccess
	session secretstore.StoreSession
}

func (a sessionAccess) Mutate(_ context.Context, _ secretstore.Context, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.transactions++
	return callback(a.session, secretstore.Selection{})
}

func (a sessionAccess) MutateArea(_ context.Context, _ secretstore.Context, area secretstore.Area, callback func(secretstore.StoreSession, secretstore.Selection) error) error {
	a.areas = append(a.areas, area)
	return callback(a.session, secretstore.Selection{})
}

func rebindFixture(t *testing.T) (*Service, *serviceAccess, *rebindSession) {
	t.Helper()
	service, access, _, _ := serviceFixture(t, declarationYAML("alpha", "")+declarationYAML("beta", ""))
	for _, name := range []string{"alpha", "beta"} {
		if _, err := service.Set(context.Background(), SetRequest{Name: name, Input: secrets.Input{ValueFile: "value"}}); err != nil {
			t.Fatal(err)
		}
	}
	session := &rebindSession{serviceSession: access.session}
	service.access = sessionAccess{access, session}
	return service, access, session
}

// A rebind replaces an existing binding's inputs with the current versions of
// the named Secrets, in name order, in one store transaction.
func TestRebindReplacesTheBindingWithTheCurrentVersions(t *testing.T) {
	service, access, session := rebindFixture(t)
	transactions := access.transactions
	result, err := service.Rebind(context.Background(), RebindRequest{ContextName: "fixture", BindingID: "binding-earlier", Names: []string{"beta", "alpha"}})
	alpha, _ := currentVersion(session.snapshot, "alpha")
	beta, _ := currentVersion(session.snapshot, "beta")
	if err != nil || result.ID != "binding-earlier" || !slices.Equal(result.Versions, []string{alpha.ID, beta.ID}) {
		t.Fatalf("rebind = %+v, %v, want the current versions %s and %s", result, err, alpha.ID, beta.ID)
	}
	if session.binding != "binding-earlier" || len(session.inputs) != 2 || session.inputs[0].Declaration.Name != "alpha" || session.inputs[1].Declaration.Name != "beta" {
		t.Fatalf("the session rebound %q with %+v", session.binding, session.inputs)
	}
	if access.transactions != transactions+1 {
		t.Fatalf("the rebind ran %d transactions", access.transactions-transactions)
	}
}

// A store whose session cannot rebind refuses before it reads any material.
func TestRebindRefusesASessionThatCannotRebind(t *testing.T) {
	service, access, _ := rebindFixture(t)
	service.access = sessionAccess{access, &struct{ secretstore.StoreSession }{access.session}}
	reads := access.session.reads
	_, err := service.Rebind(context.Background(), RebindRequest{ContextName: "fixture", BindingID: "binding-earlier", Names: []string{"alpha"}})
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "secret.store.implementation" || access.session.reads != reads {
		t.Fatalf("rebind over a session without Rebind = %+v after %d reads", found, access.session.reads-reads)
	}
	if _, err := service.Rebind(context.Background(), RebindRequest{ContextName: "fixture", BindingID: "binding-earlier", Names: []string{"alpha", "alpha"}}); len(diagnostics.Of(err)) != 1 {
		t.Fatalf("a duplicate name was admitted: %v", err)
	}
}

// A prepared rebind resolves its declarations before the lifecycle transaction
// lends its area, then runs only through that area and only for the context it
// was prepared against.
func TestPrepareRebindRunsOnlyThroughTheLentAreaForItsContext(t *testing.T) {
	service, access, session := rebindFixture(t)
	selected := access.snapshot.Context
	areas, transactions := len(access.areas), access.transactions
	scoped, err := service.PrepareRebind(context.Background(), RebindRequest{ContextName: "fixture", BindingID: "binding-earlier", Names: []string{"alpha"}})
	if err != nil || len(access.areas) != areas || len(session.inputs) != 0 {
		t.Fatalf("prepare = %v, took %d areas and rebound %d inputs", err, len(access.areas)-areas, len(session.inputs))
	}
	lent := &struct{ secretstore.Area }{}
	result, err := scoped(context.Background(), selected, lent)
	if err != nil || result.ID != "binding-earlier" || len(session.inputs) != 1 || session.inputs[0].Declaration.Name != "alpha" {
		t.Fatalf("scoped rebind = %+v, %v with inputs %+v", result, err, session.inputs)
	}
	if len(access.areas) != areas+1 || access.areas[areas] != lent || access.transactions != transactions {
		t.Fatalf("areas %d, transactions %d", len(access.areas)-areas, access.transactions-transactions)
	}
	moved := selected
	moved.Revision = "rev-moved"
	_, err = scoped(context.Background(), moved, lent)
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "secret.store.conflict" || len(access.areas) != areas+1 || len(session.inputs) != 1 {
		t.Fatalf("a changed context gave %+v with %d areas", found, len(access.areas)-areas)
	}
	for name, request := range map[string]RebindRequest{
		"no names":        {ContextName: "fixture", BindingID: "binding-earlier"},
		"an undeclared":   {ContextName: "fixture", BindingID: "binding-earlier", Names: []string{"absent"}},
		"a repeated name": {ContextName: "fixture", BindingID: "binding-earlier", Names: []string{"beta", "beta"}},
	} {
		if prepared, err := service.PrepareRebind(context.Background(), request); err == nil || prepared != nil {
			t.Fatalf("prepare admitted %s", name)
		}
	}
	access.snapshot.Context.Mode = "initializing"
	if prepared, err := service.PrepareRebind(context.Background(), RebindRequest{ContextName: "fixture", BindingID: "binding-earlier", Names: []string{"alpha"}}); err == nil || prepared != nil {
		t.Fatal("prepare admitted a context that is not ready")
	}
}

// A release reports whether the binding existed, so a second release changes
// nothing, and the listing no longer names it.
func TestReleaseReportsWhetherTheBindingExisted(t *testing.T) {
	service, access, session := rebindFixture(t)
	session.snapshot.Bindings = []secretstore.Binding{{ID: "binding-a"}, {ID: "binding-b"}}
	transactions := access.transactions
	for _, want := range []bool{true, false} {
		if changed, err := service.Release(context.Background(), BindingRequest{ContextName: "fixture", BindingID: "binding-a"}); err != nil || changed != want {
			t.Fatalf("release = %t, %v, want %t", changed, err, want)
		}
	}
	if access.transactions != transactions+2 {
		t.Fatalf("two releases ran %d transactions", access.transactions-transactions)
	}
	if listed, err := service.Bindings(context.Background(), BindingsRequest{ContextName: "fixture"}); err != nil || !slices.Equal(listed, []string{"binding-b"}) {
		t.Fatalf("bindings after a release = %v, %v", listed, err)
	}
}

// Every binding and produced-entry service refuses an unconfigured service and
// a cancelled caller before it resolves a context or opens the store.
func TestBindingAndProducedServicesRefuseWithoutAccessOrACaller(t *testing.T) {
	ctx := context.Background()
	lent := &struct{ secretstore.Area }{}
	selected := secretstore.Context{Name: "fixture", Mode: "ready"}
	request := BindingRequest{ContextName: "fixture", BindingID: "binding-a"}
	produced := ReadProducedRequest{ContextName: "fixture", Block: "cluster-install-sno", Name: "token"}
	calls := map[string]func(Service, context.Context) error{
		"reopen": func(s Service, c context.Context) error { _, err := s.Reopen(c, request); return err },
		"bindings": func(s Service, c context.Context) error {
			_, err := s.Bindings(c, BindingsRequest{ContextName: "fixture"})
			return err
		},
		"release":      func(s Service, c context.Context) error { _, err := s.Release(c, request); return err },
		"withdraw":     func(s Service, c context.Context) error { _, err := s.Withdraw(c, selected, lent); return err },
		"holds":        func(s Service, c context.Context) error { _, err := s.Holds(c, selected, lent, "b", "n"); return err },
		"readProduced": func(s Service, c context.Context) error { _, _, err := s.ReadProduced(c, produced); return err },
		"readProducedInArea": func(s Service, c context.Context) error {
			_, _, err := s.ReadProducedInArea(c, selected, lent, "b", "n")
			return err
		},
		"producedAvailable": func(s Service, c context.Context) error { _, err := s.ProducedAvailable(c, produced); return err },
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for name, call := range calls {
		if found := diagnostics.Of(call(Service{}, ctx)); len(found) != 1 || found[0].Code != "secret.store.implementation" {
			t.Errorf("%s without an access gave %+v", name, found)
		}
		service, access, _, _ := serviceFixture(t, declarationYAML("token", ""))
		contexts, views := access.contexts, access.views
		if err := call(*service, cancelled); !errors.Is(err, context.Canceled) || access.contexts != contexts || access.views != views || len(access.areas) != 0 {
			t.Errorf("%s for a cancelled caller gave %v after %d context reads", name, err, access.contexts-contexts)
		}
	}
}

// A context that fails to resolve, or a store that fails to open, is the
// caller's error and leaves no result behind.
func TestBindingAndProducedReadsPassContextAndStoreFailuresThrough(t *testing.T) {
	ctx := context.Background()
	request := BindingRequest{ContextName: "fixture", BindingID: "binding-a"}
	produced := ReadProducedRequest{ContextName: "fixture", Block: "cluster-install-sno", Name: "token"}
	calls := map[string]func(*Service) error{
		"reopen":            func(s *Service) error { _, err := s.Reopen(ctx, request); return err },
		"bindings":          func(s *Service) error { _, err := s.Bindings(ctx, BindingsRequest{ContextName: "fixture"}); return err },
		"release":           func(s *Service) error { _, err := s.Release(ctx, request); return err },
		"readProduced":      func(s *Service) error { _, _, err := s.ReadProduced(ctx, produced); return err },
		"producedAvailable": func(s *Service) error { _, err := s.ProducedAvailable(ctx, produced); return err },
	}
	for name, call := range calls {
		service, access, _, _ := serviceFixture(t, declarationYAML("token", ""))
		access.contextFailure = errors.New("context unavailable")
		if err := call(service); err == nil || err.Error() != "context unavailable" {
			t.Errorf("%s over a failed context gave %v", name, err)
		}
		access.contextFailure = nil
		access.failure = errors.New("store unavailable")
		if err := call(service); err == nil || err.Error() != "store unavailable" {
			t.Errorf("%s over a failed store gave %v", name, err)
		}
	}
}

// Through the lent area, holds proves an entry readable and reads it without a
// lock of its own, an unproved entry says so, and an uninitialized store holds
// none.
func TestHoldsAndReadProducedInAreaReadThroughTheLentArea(t *testing.T) {
	service, access, _, _ := serviceFixture(t, declarationYAML("token", ""))
	withProduced(access)
	lent := &struct{ secretstore.Area }{}
	selected := access.snapshot.Context
	views, contexts := access.views, access.contexts
	held, err := service.Holds(context.Background(), selected, lent, "cluster-install-sno", "token")
	if err != nil || !held {
		t.Fatalf("holds = %t, %v", held, err)
	}
	if held, err := service.Holds(context.Background(), selected, lent, "cluster-install-sno", "other"); err != nil || held {
		t.Fatalf("holds an absent entry = %t, %v", held, err)
	}
	read, found, err := service.ReadProducedInArea(context.Background(), selected, lent, "cluster-install-sno", "token")
	value, _ := read.Material.Part(secrets.ValuePart)
	read.Material.Clear()
	if err != nil || !found || read.Unproved || string(value) != "produced-kubeconfig" {
		t.Fatalf("read = %q, unproved %t, found %t, %v", value, read.Unproved, found, err)
	}
	access.session.snapshot.Produced[0].Unproved = true
	read, found, err = service.ReadProducedInArea(context.Background(), selected, lent, "cluster-install-sno", "token")
	read.Material.Clear()
	if err != nil || !found || !read.Unproved {
		t.Fatalf("an unproved entry read as unproved %t, found %t, %v", read.Unproved, found, err)
	}
	if len(access.areas) != 4 || slices.ContainsFunc(access.areas, func(area secretstore.Area) bool { return area != lent }) || access.views != views || access.contexts != contexts {
		t.Fatalf("areas %d, views %d, contexts %d", len(access.areas), access.views-views, access.contexts-contexts)
	}
	access.session = nil
	if held, err := service.Holds(context.Background(), selected, lent, "cluster-install-sno", "token"); err != nil || held {
		t.Fatalf("an uninitialized store held an entry: %t, %v", held, err)
	}
	if _, found, err := service.ReadProducedInArea(context.Background(), selected, lent, "cluster-install-sno", "token"); err != nil || found {
		t.Fatalf("an uninitialized store read an entry: %t, %v", found, err)
	}
	access.failure = errors.New("store unavailable")
	if _, found, err := service.ReadProducedInArea(context.Background(), selected, lent, "cluster-install-sno", "token"); err == nil || found {
		t.Fatalf("a failed store read an entry: %t, %v", found, err)
	}
	if _, err := service.Holds(context.Background(), selected, lent, "cluster-install-sno", "token"); err == nil {
		t.Fatal("a failed store held an entry")
	}
}
