//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// lifecycleView is a coherent snapshot of everything one operation reasons
// about. Its capabilities expire with the callback that received it.
type lifecycleView struct {
	identity   lifecycle.ContextIdentity
	inputs     desiredstate.Sources
	controller prerequisites.StorageView
	evidence   []byte
	operations *operationArea
}

func (v *lifecycleView) Identity() lifecycle.ContextIdentity   { return v.identity }
func (v *lifecycleView) Inputs() desiredstate.Sources          { return v.inputs }
func (v *lifecycleView) Controller() prerequisites.StorageView { return v.controller }
func (v *lifecycleView) Evidence() []byte                      { return slices.Clone(v.evidence) }
func (v *lifecycleView) Operations() operationstore.Area       { return v.operations }

// lifecycleRun is the view one bounded operation outside the lifecycle holds.
// It adds the area its adapter output is retained in and nothing else: it
// publishes no evidence, takes no lease and registers no operation.
type lifecycleRun struct {
	*lifecycleView
	runs *operationArea
}

func (r *lifecycleRun) Runs() operationstore.Area { return r.runs }

type lifecycleTransaction struct {
	lifecycleView
	base    *transaction
	stored  controllerStored
	context *directory
	areas   openedBundles
	// active and guard are the capability boundary every area this operation
	// opens shares: it expires with the callback, and each access reproves that
	// the shared controller record has not changed underneath it.
	active func() bool
	guard  func(context.Context) error
}

// openedBundles collects every bundle area an opener handed out, so the
// callback that expires them closes all of them. The opener is a published
// capability reachable from an operation's concurrently running blocks, so
// what it collects is guarded here rather than by how often one caller
// happens to open a bundle.
type openedBundles struct {
	mutex sync.Mutex
	areas []*controllerBundleArea
}

func (o *openedBundles) keep(area *controllerBundleArea) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	o.areas = append(o.areas, area)
}

func (o *openedBundles) closeAll() {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	for _, area := range o.areas {
		area.close()
	}
	o.areas = nil
}

// find answers with the open area of one reservation, so a caller that already
// opened it works through the same handle rather than a second one.
func (o *openedBundles) find(id string) *controllerBundleArea {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	for _, area := range o.areas {
		if area.reservation.ID == id && area.dir != nil {
			return area
		}
	}
	return nil
}

func lifecycleRecord(registry contexts.Registry, name string) (contexts.Record, error) {
	for _, record := range registry.Contexts {
		if record.Name != name {
			continue
		}
		if record.Mode != contexts.Ready {
			return contexts.Record{}, state("the selected context is not ready for lifecycle work")
		}
		if record.Revision == "" {
			return contexts.Record{}, contexts.StateErrorWithRemediation("the selected context has no desired-state revision", "import one with context update --name "+name+" --input-dir <dir>")
		}
		return record, nil
	}
	return contexts.Record{}, state("the selected context does not exist")
}

// ReadLifecycle takes a coherent read under the shared root lock. It creates,
// repairs and publishes nothing.
func (s *Store) ReadLifecycle(ctx context.Context, name string, callback func(lifecycle.View) error) error {
	if callback == nil {
		return state("lifecycle inspection callback is missing")
	}
	return s.readLifecycle(ctx, name, false, func(view *lifecycleView, _ *operationArea) error {
		return callback(view)
	})
}

// RunLifecycle takes the same coherent read and adds the two capabilities one
// bounded operation outside the lifecycle needs: the controller's approved
// execution bundle, which its adapter call runs inside, and a writable area
// for what that adapter prints. It registers no operation, freezes no plan and
// publishes no evidence, so what it retains is troubleshooting material alone.
func (s *Store) RunLifecycle(ctx context.Context, name string, callback func(lifecycle.RunView) error) error {
	if callback == nil {
		return state("bounded lifecycle run callback is missing")
	}
	return s.readLifecycle(ctx, name, true, func(view *lifecycleView, runs *operationArea) error {
		return callback(&lifecycleRun{lifecycleView: view, runs: runs})
	})
}

func (s *Store) readLifecycle(ctx context.Context, name string, bounded bool, callback func(*lifecycleView, *operationArea) error) error {
	if !contextName(name) {
		return state("lifecycle inspection requires an explicit context name")
	}
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return state("context store does not exist")
	}
	if err != nil {
		return safeError(err)
	}
	defer root.file.Close()
	if err := lockShared(root); err != nil {
		return err
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, exists, err := readRegistry(ctx, root)
	if err != nil {
		return safeError(err)
	}
	if !exists {
		return state("context store does not exist")
	}
	if err := verifyMappings(ctx, root, registry); err != nil {
		return safeError(err)
	}
	record, err := lifecycleRecord(registry, name)
	if err != nil {
		return err
	}
	controllerView, stored, err := controllerSnapshot(ctx, root, registry, name)
	if err != nil {
		return safeError(err)
	}
	container, err := openDirectory(root, "contexts")
	if err != nil {
		return safeError(err)
	}
	defer container.file.Close()
	dir, err := openDirectory(container, record.Name)
	if err != nil {
		return safeError(err)
	}
	defer dir.file.Close()
	if record.DirectoryInode != 0 && (dir.identity.Ino != record.DirectoryInode || uint64(dir.identity.Dev) != record.DirectoryDevice) {
		return state("context directory was replaced")
	}
	inputs, err := readSnapshot(ctx, root, record)
	if err != nil {
		return safeError(err)
	}
	evidence, err := readMutation(ctx, dir)
	if err != nil {
		return safeError(err)
	}
	active := true
	var areas openedBundles
	defer func() {
		active = false
		areas.closeAll()
	}()
	// A bounded run executes inside the controller's approved bundle, so it
	// needs the same opener a mutation has. An inspection runs nothing and is
	// offered none.
	if bounded {
		guard := func(call context.Context) error {
			actual, err := readControllerStored(call, root, registry)
			if err != nil || !bytes.Equal(actual.data, stored.data) || actual.data != nil && !sameFile(actual.identity, stored.identity) {
				return state("controller evidence changed during the run")
			}
			return nil
		}
		controllerView.OpenBundle = func(call context.Context, id string) (prerequisites.BundleArea, error) {
			if !active {
				return nil, state("bounded lifecycle run capability has closed")
			}
			if err := guard(call); err != nil {
				return nil, err
			}
			area, err := openControllerBundle(call, s, root, registry, stored, id, func() bool { return active }, false, guard)
			if err != nil || area == nil {
				return nil, err
			}
			areas.keep(area)
			return area, nil
		}
	}
	live := func() bool { return active }
	view := &lifecycleView{
		identity:   lifecycle.ContextIdentity{Name: record.Name, Revision: record.Revision},
		inputs:     inputs,
		controller: controllerView,
		evidence:   evidence,
		operations: &operationArea{store: s, subtree: "operations", context: dir, name: record.Name, active: live, readOnly: true},
	}
	runs := &operationArea{store: s, subtree: "runs", context: dir, name: record.Name, active: live, readOnly: !bounded}
	return safeError(callback(view, runs))
}

// MutateLifecycle holds the exclusive root lock and the context lease for the
// whole callback, so host reservations, controller evidence and operation
// records cannot disagree while effects run.
func (s *Store) MutateLifecycle(ctx context.Context, name string, callback func(lifecycle.Transaction) error) error {
	if callback == nil {
		return state("lifecycle mutation callback is missing")
	}
	if !contextName(name) {
		return state("lifecycle mutation requires an explicit context name")
	}
	return s.Transact(ctx, false, nil, func(base contexts.Transaction) error {
		t := base.(*transaction)
		record, err := lifecycleRecord(t.registry, name)
		if err != nil {
			return err
		}
		controllerView, stored, err := controllerSnapshot(ctx, t.root, t.registry, name)
		if err != nil {
			return err
		}
		dir, err := t.leaseContext(ctx, record.Name)
		if err != nil {
			return err
		}
		inputs, err := readSnapshot(ctx, t.root, record)
		if err != nil {
			return err
		}
		evidence, err := t.MutationState(ctx, record.Name)
		if err != nil {
			return err
		}
		active := true
		tx := &lifecycleTransaction{
			lifecycleView: lifecycleView{
				identity:   lifecycle.ContextIdentity{Name: record.Name, Revision: record.Revision},
				inputs:     inputs,
				controller: controllerView,
				evidence:   evidence,
				operations: &operationArea{store: s, subtree: "operations", context: dir, name: record.Name, active: func() bool { return active }},
			},
			base: t, stored: stored, context: dir,
		}
		defer func() {
			active = false
			tx.areas.closeAll()
		}()
		tx.active = func() bool { return active }
		tx.guard = func(call context.Context) error {
			if !active {
				return state("lifecycle controller capability has closed")
			}
			if err := t.available(call); err != nil {
				return err
			}
			actual, err := readControllerStored(call, t.root, t.registry)
			if err != nil || !bytes.Equal(actual.data, tx.stored.data) || actual.data != nil && !sameFile(actual.identity, tx.stored.identity) {
				return state("controller evidence changed during the operation")
			}
			return nil
		}
		// A block runs inside the controller's approved bundle, so the operation
		// needs the same read-only opener setup uses. Without it no effect can
		// execute at all.
		tx.controller.OpenBundle = func(call context.Context, id string) (prerequisites.BundleArea, error) {
			if err := tx.guard(call); err != nil {
				return nil, err
			}
			area, err := openControllerBundle(call, s, t.root, t.registry, tx.stored, id, tx.active, false, tx.guard)
			if err != nil || area == nil {
				return nil, err
			}
			tx.areas.keep(area)
			return area, nil
		}
		// Every publication this transaction performs is independently atomic
		// and verified, so it needs no registry commit: a lifecycle operation
		// changes no context record.
		return callback(tx)
	})
}

// PublishEvidence replaces the context's mutation record atomically after
// proving it still holds exactly what this transaction last observed.
func (t *lifecycleTransaction) PublishEvidence(ctx context.Context, data []byte) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > maxRecord {
		return state("context mutation evidence exceeds its bounds")
	}
	runtime, err := openDirectory(t.context, "state")
	if err != nil {
		return state("context state directory is unsafe")
	}
	defer runtime.file.Close()
	current, err := readBounded(ctx, runtime, "mutation.json", maxRecord, true)
	if err != nil || !bytes.Equal(current, t.evidence) {
		return state("context mutation evidence changed before publication")
	}
	if bytes.Equal(current, data) {
		return nil
	}
	if err := t.base.store.checkpoint(ctx, "before-evidence"); err != nil {
		return err
	}
	var pending string
	for range 16 {
		candidate, err := t.base.store.candidate("pending-")
		if err != nil {
			return err
		}
		pending = candidate + ".json"
		err = t.base.store.writeExclusive(ctx, runtime, pending, data)
		if errors.Is(err, syscall.EEXIST) {
			pending = ""
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if pending == "" {
		return state("context mutation evidence exhausted its collision limit")
	}
	if err := runtime.verify(); err != nil {
		return err
	}
	if err := syscall.Renameat(int(runtime.file.Fd()), pending, int(runtime.file.Fd()), "mutation.json"); err != nil {
		return state("context mutation evidence could not be atomically published")
	}
	published, err := readBounded(ctx, runtime, "mutation.json", maxRecord, true)
	if err != nil || !bytes.Equal(published, data) {
		return state("published context mutation evidence is unsafe")
	}
	if err := t.base.store.syncDirectory(ctx, runtime); err != nil {
		return err
	}
	t.evidence = slices.Clone(data)
	t.base.evidence[t.identity.Name] = slices.Clone(data)
	return nil
}

// Reserve replaces this context's claims after proving no other context holds
// any requested key. A key another context owns refuses; it is never taken.
func (t *lifecycleTransaction) Reserve(ctx context.Context, reservations []prerequisites.HostReservation) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	next := make([]prerequisites.HostReservation, 0, len(reservations))
	for _, reservation := range reservations {
		if reservation.Context != t.identity.Name {
			return state("a lifecycle reservation must belong to its own context")
		}
		keys := slices.Clone(reservation.Keys)
		slices.Sort(keys)
		reservation.Keys = keys
		next = append(next, reservation)
	}
	slices.SortFunc(next, func(x, y prerequisites.HostReservation) int {
		if order := strings.Compare(x.Kind, y.Kind); order != 0 {
			return order
		}
		return strings.Compare(x.Service, y.Service)
	})
	return t.publishReservations(ctx, next)
}

func (t *lifecycleTransaction) ReleaseReservations(ctx context.Context) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	return t.publishReservations(ctx, nil)
}

// Bind records this context's controller relationship on the prepared host.
// The first apply establishes it; every later one revalidates the exact same
// relationship. A different Machine or host never replaces an existing
// binding, so relocation stays an explicit operator decision.
func (t *lifecycleTransaction) Bind(ctx context.Context, machine string, host controller.InstalledHostIdentity) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if machine == "" {
		return state("a controller binding requires the selected Machine name")
	}
	if t.stored.data == nil || t.stored.value.Receipt.Status != "complete" {
		return controllerFailure("controller.identity", "this host has no completed controller setup; run bootwright setup")
	}
	if !t.stored.value.Host.Equal(host) {
		return controllerFailure("controller.identity", "this host is not the host this controller state belongs to")
	}
	digest, err := host.PrivateDigest()
	if err != nil {
		return err
	}
	bindings := slices.Clone(t.stored.value.Bindings)
	for _, binding := range bindings {
		if binding.Context != t.identity.Name {
			continue
		}
		if binding.Machine != machine || binding.HostDigest != digest {
			return controllerFailure("controller.identity", "this context is already bound to another controller Machine or host; restore it, or create a context here")
		}
		return nil
	}
	bindings = append(bindings, prerequisites.ControllerBinding{Context: t.identity.Name, Machine: machine, HostDigest: digest})
	slices.SortFunc(bindings, func(x, y prerequisites.ControllerBinding) int { return strings.Compare(x.Context, y.Context) })
	value := cloneControllerState(t.stored.value)
	value.Bindings = bindings
	return t.publishControllerState(ctx, value, t.stored.bundles, "before-binding")
}

func (t *lifecycleTransaction) publishReservations(ctx context.Context, next []prerequisites.HostReservation) error {
	if t.stored.data == nil {
		if len(next) == 0 {
			return nil
		}
		return controllerFailure("controller.identity", "locally hosted services require a completed controller setup on this host")
	}
	claimed := map[string]string{}
	retained := []prerequisites.HostReservation{}
	for _, reservation := range t.stored.value.Reservations {
		if reservation.Context == t.identity.Name {
			continue
		}
		retained = append(retained, reservation)
		if reservation.Shared {
			continue
		}
		for _, key := range reservation.Keys {
			claimed[key] = reservation.Context
		}
	}
	for _, reservation := range next {
		if reservation.Shared {
			continue
		}
		for _, key := range reservation.Keys {
			if owner, taken := claimed[key]; taken {
				return controllerFailure("controller.conflict", "another context already reserves a host resource this service needs; destroy or continue context "+owner+" first")
			}
		}
	}
	combined := append(retained, next...)
	slices.SortFunc(combined, func(x, y prerequisites.HostReservation) int {
		if order := strings.Compare(x.Context, y.Context); order != 0 {
			return order
		}
		if order := strings.Compare(x.Kind, y.Kind); order != 0 {
			return order
		}
		return strings.Compare(x.Service, y.Service)
	})
	if len(combined) == 0 {
		combined = nil
	}
	value := cloneControllerState(t.stored.value)
	value.Reservations = combined
	return t.publishControllerState(ctx, value, t.stored.bundles, "before-reservation")
}

// publishControllerState replaces the shared record atomically under the held
// root lock, then re-reads it so the transaction keeps working from exactly
// what is durable.
func (t *lifecycleTransaction) publishControllerState(ctx context.Context, value prerequisites.HostState, bundles []controllerBundleReservation, checkpoint string) error {
	if err := t.base.store.checkpoint(ctx, checkpoint); err != nil {
		return err
	}
	dir, err := openControllerDirectory(t.base.root, t.base.registry)
	if err != nil {
		return err
	}
	defer dir.file.Close()
	data, err := encodeRecord(controllerRecord(value, bundles), maxControllerState)
	if err != nil {
		return err
	}
	if err := validateControllerState(value); err != nil {
		return err
	}
	if _, err := t.base.store.replaceControllerRecord(ctx, dir, t.stored, data); err != nil {
		return err
	}
	refreshed, err := readControllerStored(ctx, t.base.root, t.base.registry)
	if err != nil {
		return err
	}
	t.stored = refreshed
	t.controller.State = cloneControllerState(refreshed.value)
	return nil
}
