//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"syscall"

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

type lifecycleTransaction struct {
	lifecycleView
	base    *transaction
	stored  controllerStored
	context *directory
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
	controllerView, _, err := controllerSnapshot(ctx, root, registry, name)
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
	defer func() { active = false }()
	view := &lifecycleView{
		identity:   lifecycle.ContextIdentity{Name: record.Name, ID: record.ID, Revision: record.Revision},
		inputs:     inputs,
		controller: controllerView,
		evidence:   evidence,
		operations: &operationArea{store: s, context: dir, active: func() bool { return active }, readOnly: true},
	}
	return safeError(callback(view))
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
		dir, err := t.leaseContext(ctx, record.ID)
		if err != nil {
			return err
		}
		inputs, err := readSnapshot(ctx, t.root, record)
		if err != nil {
			return err
		}
		evidence, err := t.MutationState(ctx, record.ID)
		if err != nil {
			return err
		}
		active := true
		defer func() { active = false }()
		tx := &lifecycleTransaction{
			lifecycleView: lifecycleView{
				identity:   lifecycle.ContextIdentity{Name: record.Name, ID: record.ID, Revision: record.Revision},
				inputs:     inputs,
				controller: controllerView,
				evidence:   evidence,
				operations: &operationArea{store: s, context: dir, active: func() bool { return active }},
			},
			base: t, stored: stored, context: dir,
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
	t.base.evidence[t.identity.ID] = slices.Clone(data)
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
		if reservation.ContextID != t.identity.ID {
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

func (t *lifecycleTransaction) publishReservations(ctx context.Context, next []prerequisites.HostReservation) error {
	if t.stored.data == nil {
		if len(next) == 0 {
			return nil
		}
		return controllerFailure("controller.identity", "locally hosted services require a completed bastion setup on this host")
	}
	claimed := map[string]string{}
	retained := []prerequisites.HostReservation{}
	for _, reservation := range t.stored.value.Reservations {
		if reservation.ContextID == t.identity.ID {
			continue
		}
		retained = append(retained, reservation)
		for _, key := range reservation.Keys {
			claimed[key] = reservation.ContextID
		}
	}
	for _, reservation := range next {
		for _, key := range reservation.Keys {
			if owner, taken := claimed[key]; taken {
				return controllerFailure("controller.conflict", "another context already reserves a host resource this service needs; destroy or continue context "+owner+" first")
			}
		}
	}
	combined := append(retained, next...)
	slices.SortFunc(combined, func(x, y prerequisites.HostReservation) int {
		if order := strings.Compare(x.ContextID, y.ContextID); order != 0 {
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
	if err := t.base.store.checkpoint(ctx, "before-reservation"); err != nil {
		return err
	}
	dir, err := openControllerDirectory(t.base.root, t.base.registry)
	if err != nil {
		return err
	}
	defer dir.file.Close()
	data, err := encodeRecord(controllerRecord(value), maxControllerState)
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
