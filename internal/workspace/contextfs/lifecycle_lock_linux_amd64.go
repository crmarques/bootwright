//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"sync"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// leasedLifecycle keeps the context leased while shared publications take short root transactions.
type leasedLifecycle struct {
	*lifecycleTransaction
	mutex sync.Mutex
}

func (l *leasedLifecycle) shared(ctx context.Context, call func(context.Context) error) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	t := l.lifecycleTransaction
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if err := lock(t.base.root); err != nil {
		return err
	}
	defer syscall.Flock(int(t.base.root.file.Fd()), syscall.LOCK_UN)
	registry, _, err := readRegistry(ctx, t.base.root)
	if err != nil {
		return err
	}
	record, err := lifecycleRecord(registry, t.identity.Name)
	if err != nil || record != t.record {
		return state("context identity changed during lifecycle operation")
	}
	actual, err := readControllerStored(ctx, t.base.root, registry)
	if err != nil {
		return err
	}
	if !sameLifecycleController(t.stored, actual, t.identity.Name) {
		return state("controller evidence changed during the operation")
	}
	expected, err := registryExpectation(ctx, t.base.root, registry)
	if err != nil {
		return err
	}
	if bytes.Equal(expected.data, t.base.expected.data) && !sameFile(expected.identity, t.base.expected.identity) {
		return state("registry identity changed during lifecycle operation")
	}
	t.base.registry, t.base.expected, t.stored = registry, expected, actual
	t.current.Store(&actual)
	err = call(context.WithValue(ctx, lifecycleRootHeld{}, l))
	stored := t.stored
	t.current.Store(&stored)
	return err
}

func sameLifecycleController(before, after controllerStored, name string) bool {
	if (before.data == nil) != (after.data == nil) {
		return false
	}
	if before.data == nil {
		return true
	}
	if bytes.Equal(before.data, after.data) && !sameFile(before.identity, after.identity) {
		return false
	}
	if !before.value.Host.Equal(after.value.Host) {
		return false
	}
	own := func(value prerequisites.HostState) prerequisites.HostState {
		value = cloneControllerState(value)
		value.Bindings = slices.DeleteFunc(value.Bindings, func(item prerequisites.ControllerBinding) bool { return item.Context != name })
		value.Reservations = slices.DeleteFunc(value.Reservations, func(item prerequisites.HostReservation) bool { return item.Context != name })
		value.ToolSources = nil
		return value
	}
	if !reflect.DeepEqual(own(before.value), own(after.value)) {
		return false
	}
	for _, source := range before.value.ToolSources {
		if !slices.Contains(after.value.ToolSources, source) {
			return false
		}
	}
	return true
}

func (l *leasedLifecycle) Bind(ctx context.Context, machine string, host controller.InstalledHostIdentity) error {
	return l.shared(ctx, func(ctx context.Context) error { return l.lifecycleTransaction.Bind(ctx, machine, host) })
}
func (l *leasedLifecycle) Reserve(ctx context.Context, reservations []prerequisites.HostReservation) error {
	return l.shared(ctx, func(ctx context.Context) error { return l.lifecycleTransaction.Reserve(ctx, reservations) })
}
func (l *leasedLifecycle) Claim(ctx context.Context, reservation prerequisites.HostReservation) error {
	return l.shared(ctx, func(ctx context.Context) error { return l.lifecycleTransaction.Claim(ctx, reservation) })
}
func (l *leasedLifecycle) ReleaseReservations(ctx context.Context) error {
	return l.shared(ctx, func(ctx context.Context) error { return l.lifecycleTransaction.ReleaseReservations(ctx) })
}
func (l *leasedLifecycle) ReproveReservations(ctx context.Context, check func([]prerequisites.HostReservation) error, releasing func(prerequisites.HostReservation) bool) error {
	return l.shared(ctx, func(ctx context.Context) error {
		return l.lifecycleTransaction.ReproveReservations(ctx, check, releasing)
	})
}
func (l *leasedLifecycle) ClientArea(ctx context.Context, id string) (area prerequisites.BundleArea, err error) {
	err = l.shared(ctx, func(ctx context.Context) error { area, err = l.lifecycleTransaction.ClientArea(ctx, id); return err })
	return area, err
}
func (l *leasedLifecycle) SealClientArea(ctx context.Context, id string) error {
	return l.shared(ctx, func(ctx context.Context) error { return l.lifecycleTransaction.SealClientArea(ctx, id) })
}
func (l *leasedLifecycle) RetainDependencies(ctx context.Context, sources []prerequisites.DependencySource) error {
	return l.shared(ctx, func(ctx context.Context) error { return l.lifecycleTransaction.RetainDependencies(ctx, sources) })
}
func (l *leasedLifecycle) RetireToolAreas(ctx context.Context, id string) error {
	return l.shared(ctx, func(ctx context.Context) error { return l.lifecycleTransaction.RetireToolAreas(ctx, id) })
}
func (l *leasedLifecycle) Secrets(ctx context.Context, callback func(secretstore.Context, secretstore.Area) error) error {
	return l.shared(ctx, func(ctx context.Context) error { return l.lifecycleTransaction.Secrets(ctx, callback) })
}

// lifecycleRootHeld avoids reacquiring a short root lease inside its own bundle check.
type lifecycleRootHeld struct{}

func (l *leasedLifecycle) inspect(ctx context.Context, call func(context.Context) error) error {
	if ctx.Value(lifecycleRootHeld{}) == l {
		return call(ctx)
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if err := lockShared(l.base.root); err != nil {
		return err
	}
	defer syscall.Flock(int(l.base.root.file.Fd()), syscall.LOCK_UN)
	return call(context.WithValue(ctx, lifecycleRootHeld{}, l))
}

func (l *leasedLifecycle) Controller() prerequisites.StorageView {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	view := l.lifecycleTransaction.controller
	view.State = cloneControllerState(view.State)
	return view
}

// A bounded run executes inside the controller's approved bundle, so it
// needs the same opener a mutation has. An inspection runs nothing and is
// offered none.
func (s *Store) prepareBoundedController(ctx context.Context, root *directory, registry contexts.Registry, record contexts.Record, stored controllerStored, view *prerequisites.StorageView, active func() bool, areas *openedBundles) error {
	expected, err := registryExpectation(ctx, root, registry)
	if err != nil {
		return err
	}
	var mutex sync.Mutex
	inspect := func(call context.Context, read func(context.Context) error) error {
		if call.Value(lifecycleRootHeld{}) == &mutex {
			return read(call)
		}
		mutex.Lock()
		defer mutex.Unlock()
		if err := lockShared(root); err != nil {
			return err
		}
		defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
		return read(context.WithValue(call, lifecycleRootHeld{}, &mutex))
	}
	guard := func(call context.Context) error {
		return inspect(call, func(call context.Context) error {
			if !active() {
				return state("bounded lifecycle run capability has closed")
			}
			latest, _, err := readRegistry(call, root)
			if err != nil {
				return err
			}
			observed, err := registryExpectation(call, root, latest)
			if err != nil {
				return err
			}
			if bytes.Equal(observed.data, expected.data) && !sameFile(observed.identity, expected.identity) {
				return state("registry identity changed during bounded lifecycle run")
			}
			currentRecord, err := lifecycleRecord(latest, record.Name)
			if err != nil || currentRecord != record {
				return state("context identity changed during bounded lifecycle run")
			}
			actual, err := readControllerStored(call, root, latest)
			if err != nil || !sameLifecycleController(stored, actual, record.Name) {
				return state("controller evidence changed during the run")
			}
			return nil
		})
	}
	view.OpenBundle = func(call context.Context, id string) (area prerequisites.BundleArea, err error) {
		err = inspect(call, func(call context.Context) error {
			if err := guard(call); err != nil {
				return err
			}
			if existing := areas.find(id); existing != nil {
				area = existing
				return nil
			}
			latest, _, err := readRegistry(call, root)
			if err != nil {
				return err
			}
			actual, err := readControllerStored(call, root, latest)
			if err != nil {
				return err
			}
			opened, err := openControllerBundle(call, s, root, latest, actual, id, active, false, guard)
			if err != nil || opened == nil {
				return err
			}
			areas.keep(opened)
			area = opened
			return nil
		})
		return area, err
	}
	return nil
}

func (l *leasedLifecycle) prepareControllerAccess() {
	tx := l.lifecycleTransaction
	t, record := tx.base, tx.record
	tx.guard = func(call context.Context) error {
		return l.inspect(call, func(call context.Context) error {
			if !tx.active() {
				return state("lifecycle controller capability has closed")
			}
			if err := t.available(call); err != nil {
				return err
			}
			registry, _, err := readRegistry(call, t.root)
			if err != nil {
				return err
			}
			observed, err := registryExpectation(call, t.root, registry)
			if err != nil {
				return err
			}
			if bytes.Equal(observed.data, t.expected.data) && !sameFile(observed.identity, t.expected.identity) {
				return state("registry identity changed during lifecycle operation")
			}
			currentRecord, err := lifecycleRecord(registry, record.Name)
			if err != nil || currentRecord != record {
				return state("context identity changed during lifecycle operation")
			}
			actual, err := readControllerStored(call, t.root, registry)
			if err != nil || !sameLifecycleController(*tx.current.Load(), actual, tx.identity.Name) {
				return state("controller evidence changed during the operation")
			}
			return nil
		})
	}
	tx.controller.OpenBundle = func(call context.Context, id string) (area prerequisites.BundleArea, err error) {
		err = l.shared(call, func(call context.Context) error {
			if err := tx.guard(call); err != nil {
				return err
			}
			if existing := tx.areas.find(id); existing != nil {
				area = existing
				return nil
			}
			opened, err := openControllerBundle(call, t.store, t.root, t.registry, tx.stored, id, tx.active, false, tx.guard)
			if err != nil || opened == nil {
				return err
			}
			tx.areas.keep(opened)
			area = opened
			return nil
		})
		return area, err
	}
}
