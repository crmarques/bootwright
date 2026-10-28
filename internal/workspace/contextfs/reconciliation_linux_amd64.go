//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
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
	revision   func(context.Context, string) (desiredstate.Sources, error)
}

func (v *lifecycleView) Identity() lifecycle.ContextIdentity   { return v.identity }
func (v *lifecycleView) Inputs() desiredstate.Sources          { return v.inputs }
func (v *lifecycleView) Controller() prerequisites.StorageView { return v.controller }
func (v *lifecycleView) Evidence() []byte                      { return slices.Clone(v.evidence) }
func (v *lifecycleView) Operations() operationstore.Area       { return v.operations }

func (v *lifecycleView) Revision(ctx context.Context, id string) (desiredstate.Sources, error) {
	if v.revision == nil {
		return desiredstate.Sources{}, state("a lifecycle transaction reads only its selected input revision")
	}
	return v.revision(ctx, id)
}

func retainedReader(root *directory, record contexts.Record, inputs desiredstate.Sources, active func() bool) func(context.Context, string) (desiredstate.Sources, error) {
	return func(call context.Context, id string) (desiredstate.Sources, error) {
		if !active() {
			return desiredstate.Sources{}, state("bounded lifecycle capability has closed")
		}
		if !identifier(id, "rev-") {
			return desiredstate.Sources{}, state("the input revision identity is invalid")
		}
		if id == record.Revision {
			return inputs, nil
		}
		sources, err := readRetainedSnapshot(call, root, record, id)
		return sources, safeError(err)
	}
}

// lifecycleRun is the view one bounded operation outside the lifecycle holds.
// It adds the area its adapter output is retained in and nothing else: it
// publishes no evidence and registers no operation. Its shared context lease
// excludes lifecycle mutation. held are
// the run directories it opened, each held shared until its callback returns.
type lifecycleRun struct {
	*lifecycleView
	runs     *operationArea
	mutex    sync.Mutex
	held     []*directory
	released bool
}

func (r *lifecycleRun) Runs() operationstore.Area { return r.runs }

type lifecycleTransaction struct {
	lifecycleView
	base    *transaction
	stored  controllerStored
	current atomic.Pointer[controllerStored]
	context *directory
	areas   openedBundles
	// record is the context record the transaction was admitted over, which
	// every secret area it lends re-proves.
	record contexts.Record
	// lending serializes the secret areas this transaction lends: an
	// operation's blocks share it, and one area at a time may publish.
	lending sync.Mutex
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
			return contexts.Record{}, contexts.NotReady(record)
		}
		if record.Revision == "" {
			return contexts.Record{}, contexts.MissingInput(record.Name)
		}
		return record, nil
	}
	return contexts.Record{}, contexts.AbsentContext(name)
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
// bounded operation outside the lifecycle needs: the controller's tool
// areas, whose executables its adapter call runs, and a writable area
// for what that adapter prints. It registers no operation, freezes no plan and
// publishes no evidence, so what it retains is troubleshooting material alone.
func (s *Store) RunLifecycle(ctx context.Context, name string, callback func(lifecycle.RunView) error) error {
	if callback == nil {
		return state("bounded lifecycle run callback is missing")
	}
	return s.readLifecycle(ctx, name, true, func(view *lifecycleView, runs *operationArea) error {
		run := &lifecycleRun{lifecycleView: view, runs: runs}
		defer run.release()
		return callback(run)
	})
}

func (s *Store) readLifecycle(ctx context.Context, name string, bounded bool, callback func(*lifecycleView, *operationArea) error) error {
	if !contextName(name) {
		return state("lifecycle inspection requires an explicit context name")
	}
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return contexts.AbsentContext(name)
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
		return contexts.AbsentContext(name)
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
	if bounded {
		if err := lockShared(dir); err != nil {
			return err
		}
		defer syscall.Flock(int(dir.file.Fd()), syscall.LOCK_UN)
	}
	inputs, err := readSnapshot(ctx, root, record)
	if err != nil {
		return safeError(err)
	}
	evidence, err := s.readyEvidence(ctx, dir, record.Name)
	if err != nil {
		return safeError(err)
	}
	active := true
	var areas openedBundles
	defer func() {
		active = false
		areas.closeAll()
	}()
	live := func() bool { return active }
	if bounded {
		if err := s.prepareBoundedController(ctx, root, registry, record, stored, &controllerView, live, &areas); err != nil {
			return err
		}
	}

	born, err := incarnation(record, dir)
	if err != nil {
		return err
	}
	view := &lifecycleView{
		identity:   lifecycle.ContextIdentity{Name: record.Name, Revision: record.Revision, Mode: string(record.Mode), Incarnation: born},
		inputs:     inputs,
		controller: controllerView,
		evidence:   evidence,
		operations: &operationArea{store: s, subtree: "operations", context: dir, name: record.Name, active: live, readOnly: true},
		revision:   retainedReader(root, record, inputs, live),
	}
	runs := &operationArea{store: s, subtree: runsSubtree, context: dir, name: record.Name, active: live, readOnly: !bounded}
	if bounded {
		if err := syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN); err != nil {
			return err
		}
	}
	return safeError(callback(view, runs))
}

func (s *Store) readyEvidence(ctx context.Context, dir *directory, name string) ([]byte, error) {
	evidence, err := readReadyMutation(ctx, dir)
	if err != nil {
		return nil, err
	}
	_, err = s.retiredOperations(ctx, dir, name)
	return evidence, err
}

func (s *Store) retiredOperations(ctx context.Context, dir *directory, name string) ([]byte, error) {
	active := true
	defer func() { active = false }()
	area := &operationArea{store: s, subtree: "operations", context: dir, name: name, active: func() bool { return active }, readOnly: true}
	data, found, err := area.Read(ctx, "index.json", operationstore.MaxIndexBytes)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	if err != nil {
		return nil, errors.Join(contexts.ErrUnreadableEvidence, err)
	}
	if !found {
		return nil, nil
	}
	if err := operationstore.CheckIndex(data, name); err != nil {
		return nil, errors.Join(contexts.ErrUnreadableEvidence, err)
	}
	return data, nil
}

// MutateLifecycle snapshots under the root lock, then holds only its context lease during effects.
func (s *Store) MutateLifecycle(ctx context.Context, name string, callback func(lifecycle.Transaction) error) error {
	if callback == nil {
		return state("lifecycle mutation callback is missing")
	}
	if !contextName(name) {
		return state("lifecycle mutation requires an explicit context name")
	}
	err := s.Transact(ctx, false, nil, func(base contexts.Transaction) error {
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
		born, err := incarnation(record, dir)
		if err != nil {
			return err
		}
		active := true
		tx := &lifecycleTransaction{
			lifecycleView: lifecycleView{
				identity:   lifecycle.ContextIdentity{Name: record.Name, Revision: record.Revision, Mode: string(record.Mode), Incarnation: born},
				inputs:     inputs,
				controller: controllerView,
				evidence:   evidence,
				operations: &operationArea{store: s, subtree: "operations", context: dir, name: record.Name, active: func() bool { return active }, cached: true},
			},
			base: t, stored: stored, context: dir, record: record,
		}
		defer func() {
			active = false
			tx.areas.closeAll()
		}()
		leased := &leasedLifecycle{lifecycleTransaction: tx}
		tx.current.Store(&stored)
		tx.active = func() bool { return active }
		leased.prepareControllerAccess()

		// Every publication this transaction performs is independently atomic
		// and verified, so it needs no registry commit: a lifecycle operation
		// changes no context record.
		if err := syscall.Flock(int(t.root.file.Fd()), syscall.LOCK_UN); err != nil {
			return err
		}
		return callback(leased)
	})
	if errors.Is(err, contexts.ErrNoContexts) {
		return contexts.AbsentContext(name)
	}
	return err
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
	if err := t.base.store.checkpoint(ctx, checkpointBeforeEvidence); err != nil {
		return err
	}
	// Only a committed publication advances what this transaction observed:
	// after a failure at or after the rename, the next command reads what was
	// published instead.
	_, err = t.base.store.publishStage(ctx, runtime, "mutation.json", data, stagedPublication{
		subject: "context mutation evidence", suffix: ".json", replace: true, immutable: true, bound: maxRecord,
		renameFailure: "context mutation evidence could not be atomically published",
		prove: func(ctx context.Context) error {
			current, err := readBounded(ctx, runtime, "mutation.json", maxRecord, true)
			if err != nil || !bytes.Equal(current, t.evidence) {
				return state("context mutation evidence changed before publication")
			}
			return nil
		},
	}, checkpointBeforeEvidenceRename, checkpointAfterEvidenceRename)
	if err != nil {
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
	return t.publishReservations(ctx, next, applyRetry(t.identity.Name))
}

// Claim adds one reservation to this context's claims, replacing the one of
// its kind and service, and writes nothing when it is already held exactly.
func (t *lifecycleTransaction) Claim(ctx context.Context, reservation prerequisites.HostReservation) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if reservation.Context != t.identity.Name {
		return state("a lifecycle reservation must belong to its own context")
	}
	reservation.Keys = slices.Sorted(slices.Values(reservation.Keys))
	next := []prerequisites.HostReservation{reservation}
	if t.stored.data != nil {
		for _, held := range t.stored.value.Reservations {
			if held.Context != t.identity.Name {
				continue
			}
			if held.Kind == reservation.Kind && held.Service == reservation.Service {
				if held.Shared == reservation.Shared && held.ObjectKind == reservation.ObjectKind && slices.Equal(held.Keys, reservation.Keys) {
					return nil
				}
				next[0].Releasing = held.Releasing
				continue
			}
			next = append(next, held)
		}
	}
	slices.SortFunc(next, func(x, y prerequisites.HostReservation) int {
		if order := strings.Compare(x.Kind, y.Kind); order != 0 {
			return order
		}
		return strings.Compare(x.Service, y.Service)
	})
	return t.publishReservations(ctx, next, applyRetry(t.identity.Name))
}

func (t *lifecycleTransaction) ReleaseReservations(ctx context.Context) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	return t.publishReservations(ctx, nil, destroyRetry(t.identity.Name))
}

// ReproveReservations marks a selected claim releasing in the publication
// that follows the admitting check, so no claim lands between the proof and
// the mark. The mark is durable: a removal interrupted during its effect
// leaves it, and only the claim's release or its replacement clears it.
func (t *lifecycleTransaction) ReproveReservations(ctx context.Context, check func([]prerequisites.HostReservation) error, releasing func(prerequisites.HostReservation) bool) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if t.stored.data == nil {
		return check(nil)
	}
	if err := check(cloneControllerState(t.stored.value).Reservations); err != nil || releasing == nil {
		return err
	}
	value := cloneControllerState(t.stored.value)
	marked := false
	for index, reservation := range value.Reservations {
		if reservation.Context == t.identity.Name && !reservation.Releasing && releasing(reservation) {
			value.Reservations[index].Releasing, marked = true, true
		}
	}
	if !marked {
		return nil
	}
	return t.publishControllerState(ctx, value, t.stored.bundles, destroyRetry(t.identity.Name), checkpointBeforeReservation)
}

// Bind records this context's controller relationship on the prepared host.
// The first apply establishes it, creating the controller record with this
// host's identity when no context bound one yet; every later one revalidates
// the exact same relationship. A different Machine or host never replaces an
// existing binding, so relocation stays an explicit operator decision.
func (t *lifecycleTransaction) Bind(ctx context.Context, machine string, host controller.InstalledHostIdentity) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if machine == "" {
		return state("a controller binding requires the selected Machine name")
	}
	if !t.controller.Prepared {
		return controllerFailure("controller.identity", "this host has no completed controller setup", completeSetup)
	}
	if t.stored.data == nil || t.base.registry.Controller.Mode != "ready" {
		if err := t.initializeController(ctx, host); err != nil {
			return err
		}
	}
	if !t.stored.value.Host.Equal(host) {
		return controllerFailure("controller.identity", "this host is not the host this controller state belongs to",
			controller.HostMismatchRemedy(t.identity.Name))
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
			return controllerFailure("controller.identity", "this context is already bound to another controller Machine or host",
				"restore the bound controller Machine input, or create a context on this host")
		}
		return nil
	}
	bindings = append(bindings, prerequisites.ControllerBinding{Context: t.identity.Name, Machine: machine, HostDigest: digest})
	slices.SortFunc(bindings, func(x, y prerequisites.ControllerBinding) int { return strings.Compare(x.Context, y.Context) })
	value := cloneControllerState(t.stored.value)
	value.Bindings = bindings
	return t.publishControllerState(ctx, value, t.stored.bundles, applyRetry(t.identity.Name), checkpointBeforeBinding)
}

// initializeController creates the controller record holding only this host's
// identity, so the first binding has one to join.
func (t *lifecycleTransaction) initializeController(ctx context.Context, host controller.InstalledHostIdentity) error {
	stored, err := t.base.initializeController(ctx, host, applyRetry(t.identity.Name))
	if err != nil {
		return err
	}
	t.stored = stored
	t.controller.Initialized = true
	t.controller.State = cloneControllerState(stored.value)
	return nil
}

func (t *lifecycleTransaction) publishReservations(ctx context.Context, next []prerequisites.HostReservation, retry string) error {
	if t.stored.data == nil {
		if len(next) == 0 {
			return nil
		}
		return controllerFailure("controller.identity", "locally hosted services require a completed controller setup on this host", completeSetup)
	}
	retained := []prerequisites.HostReservation{}
	for _, reservation := range t.stored.value.Reservations {
		if reservation.Context != t.identity.Name {
			retained = append(retained, reservation)
		}
	}
	if conflicts := prerequisites.Conflicts(retained, next); len(conflicts) > 0 {
		found := make([]diagnostics.Diagnostic, 0, len(conflicts))
		for _, conflict := range conflicts {
			found = append(found, prerequisites.ConflictDiagnostic(conflict))
		}
		return &diagnostics.Failure{Diagnostics: found}
	}
	if len(retained)+len(next) > maxControllerReservations {
		return controllerFailure("controller.conflict", fmt.Sprintf("the controller record holds at most %d host reservations: this apply needs %d and other contexts hold %d",
			maxControllerReservations, len(next), len(retained)),
			"take back a context this host no longer needs with bootwright destroy --context <name>, or delete it with bootwright context delete --name <name> --purge, then repeat the apply")
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
	return t.publishControllerState(ctx, value, t.stored.bundles, retry, checkpointBeforeReservation)
}

// publishControllerState replaces the shared record atomically under the held
// root lock, then re-reads it so the transaction keeps working from exactly
// what is durable.
func (t *lifecycleTransaction) publishControllerState(ctx context.Context, value prerequisites.HostState, bundles []controllerBundleReservation, retry string, point checkpoint) error {
	if err := t.base.store.checkpoint(ctx, point); err != nil {
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
	if err := validateControllerState(value, bundles); err != nil {
		return err
	}
	if _, err := t.base.store.replaceControllerRecord(ctx, dir, t.stored, data, retry); err != nil {
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
