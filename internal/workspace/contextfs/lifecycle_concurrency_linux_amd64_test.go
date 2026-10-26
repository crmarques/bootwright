//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func TestLifecycleEffectsAllowReadersAndAnotherContext(t *testing.T) {
	ctx := context.Background()
	store, sources := fixture(t)
	publish(t, store, "example", sources)
	publish(t, store, "other", sources)
	other := New(Options{Root: store.options.Root, Owner: &Ownership{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}})
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
			identity, inputs := tx.Identity(), tx.Inputs()
			if err := tx.Operations().WriteExclusive(ctx, "index.json", []byte(`{"running":true}`)); err != nil {
				return err
			}
			close(entered)
			<-release
			if tx.Identity() != identity || !reflect.DeepEqual(tx.Inputs(), inputs) {
				return state("operation snapshot changed")
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatal(err)
	}
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	if _, err := other.View(ctx); err != nil {
		t.Fatal("context list/current blocked", err)
	}
	if _, err := other.ReadInputs(ctx, "example"); err != nil {
		t.Fatal("validate/render blocked", err)
	}
	if err := other.ReadLifecycle(ctx, "example", func(view lifecycle.View) error {
		data, found, err := view.Operations().Read(ctx, "index.json", 4096)
		if err != nil {
			return err
		}
		if !found || string(data) != `{"running":true}` {
			return state("reader missed published progress")
		}
		return nil
	}); err != nil {
		t.Fatal("status blocked", err)
	}
	if err := other.MutateLifecycle(ctx, "other", func(tx lifecycle.Transaction) error {
		return tx.Operations().WriteExclusive(ctx, "index.json", []byte(`{"other":true}`))
	}); err != nil {
		t.Fatal("independent context blocked", err)
	}
}

func TestConcurrentContextReservationsRebaseWithoutLosingAnotherOwner(t *testing.T) {
	ctx := context.Background()
	store, sources := fixture(t)
	publish(t, store, "example", sources)
	publish(t, store, "other", sources)
	publishControllerState(t, store, syntheticControllerState(t))
	claim := func(name, key string) []prerequisites.HostReservation {
		return []prerequisites.HostReservation{{Context: name, Kind: "test", Service: "service", Keys: []string{key}}}
	}
	if err := store.MutateLifecycle(ctx, "example", func(first lifecycle.Transaction) error {
		if err := first.Reserve(ctx, claim("example", "path:/var/lib/example")); err != nil {
			return err
		}
		if err := store.MutateLifecycle(ctx, "other", func(second lifecycle.Transaction) error {
			if err := second.Reserve(ctx, claim("other", "path:/var/lib/example")); err == nil {
				return state("conflicting reservation was admitted")
			}
			return second.Reserve(ctx, claim("other", "path:/var/lib/other"))
		}); err != nil {
			return err
		}
		return first.Reserve(ctx, claim("example", "path:/var/lib/example-next"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		if len(view.State.Reservations) != 2 {
			t.Fatalf("lost reservation: %+v", view.State.Reservations)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleBundleLeaseSurvivesOtherContextsAndDefersRetirement(t *testing.T) {
	for _, bounded := range []bool{false, true} {
		t.Run(map[bool]string{false: "mutation", true: "bounded-run"}[bounded], func(t *testing.T) {
			ctx := context.Background()
			store, record := lifecycleFixture(t)
			old := sealedBundleFixture(t, store, record)
			if err := publishClients(store, clientClosure, true); err != nil {
				t.Fatal(err)
			}
			_, sources := fixture(t)
			publish(t, store, "other", sources)
			if err := store.MutateLifecycle(ctx, "other", func(tx lifecycle.Transaction) error { return tx.Bind(ctx, "controller", syntheticHost(t)) }); err != nil {
				t.Fatal(err)
			}
			use := func(view lifecycle.View) error {
				area, err := view.Controller().OpenBundle(ctx, old)
				if err != nil {
					return err
				}
				if area == nil {
					return state("missing sealed bundle")
				}
				publish(t, store, "third", sources)
				if err := area.Verify(ctx); err != nil {
					return err
				}
				if err := retireToolAreas(store, "example", clientClosure); err == nil {
					return state("retired a bundle held by another context")
				}
				return area.Verify(ctx)
			}
			var err error
			if bounded {
				err = store.RunLifecycle(ctx, "other", func(view lifecycle.RunView) error { return use(view) })
			} else {
				err = store.MutateLifecycle(ctx, "other", func(tx lifecycle.Transaction) error { return use(tx) })
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := retireToolAreas(store, "example", clientClosure); err != nil {
				t.Fatal("retirement did not resume after reader closed", err)
			}
			requireAreas(t, store, clientClosure)
		})
	}
}

func TestLifecycleSecretsRefreshRegistryAfterAnotherContextIsCreated(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	_, sources := fixture(t)
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		publish(t, store, "other", sources)
		return tx.Secrets(ctx, func(_ secretstore.Context, area secretstore.Area) error {
			_, _, err := area.Read(ctx, "store.json", 4096)
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleControllerSnapshotRejectsOwnMeaningChanges(t *testing.T) {
	before := controllerStored{data: []byte("before"), value: syntheticControllerState(t)}
	before.value.Bindings = []prerequisites.ControllerBinding{{Context: "example", Machine: "controller"}}
	before.value.Reservations = []prerequisites.HostReservation{{Context: "example", Kind: "test", Service: "service", Keys: []string{"path:/var/lib/example"}}}
	before.value.ToolSources = []prerequisites.DependencySource{{ID: "client"}}
	for _, test := range []struct {
		name    string
		change  func(*controllerStored)
		allowed bool
	}{
		{"other binding", func(after *controllerStored) {
			after.value.Bindings = append(after.value.Bindings, prerequisites.ControllerBinding{Context: "other", Machine: "controller"})
		}, true},
		{"other reservation", func(after *controllerStored) {
			after.value.Reservations = append(after.value.Reservations, prerequisites.HostReservation{Context: "other"})
		}, true},
		{"new dependency", func(after *controllerStored) {
			after.value.ToolSources = append(after.value.ToolSources, prerequisites.DependencySource{ID: "new"})
		}, true},
		{"own binding", func(after *controllerStored) { after.value.Bindings[0].Machine = "changed" }, false},
		{"own reservation", func(after *controllerStored) { after.value.Reservations[0].Keys[0] = "path:/var/lib/changed" }, false},
		{"removed dependency", func(after *controllerStored) { after.value.ToolSources = nil }, false},
		{"host replacement", func(after *controllerStored) { after.value.Host = prerequisites.HostState{}.Host }, false},
		{"same bytes replaced inode", func(after *controllerStored) { after.data = before.data; after.identity.Ino++ }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := before
			after.data = []byte("after")
			after.value = cloneControllerState(before.value)
			test.change(&after)
			if got := sameLifecycleController(before, after, "example"); got != test.allowed {
				t.Fatalf("accepted=%v, want %v", got, test.allowed)
			}
		})
	}
}
