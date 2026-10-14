//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

var _ prerequisites.Storage = (*Store)(nil)

type controllerStored struct {
	value    prerequisites.HostState
	bundles  []controllerBundleReservation
	data     []byte
	identity syscall.Stat_t
}

// completeSetup settles a command other than setup that needs the completed
// setup this host lacks.
const completeSetup = "run bootwright setup, then repeat the command"

// applyRetry settles an uncertain controller record publication an apply of
// the named context met.
func applyRetry(name string) string {
	return "once storage is healthy, repeat bootwright apply --context " + name
}

// destroyRetry settles an uncertain controller record publication a destroy of
// the named context met while it released its reservations.
func destroyRetry(name string) string {
	return "once storage is healthy, repeat bootwright destroy --context " + name
}

// deletionRetry settles an uncertain controller record publication the named
// context's deletion met after the registry marked it deleting.
func deletionRetry(name string) string {
	return "once storage is healthy, finish the deletion with bootwright context delete --name " + name + " --purge"
}

func openControllerDirectory(root *directory, registry contexts.Registry) (*directory, error) {
	// An unattributed descriptor is an interrupted initialization. Any
	// directory beside it holds no readable controller state, so reads report
	// missing setup instead of refusing; only explicit setup resolves the
	// orphan, and it still declines to adopt it.
	if registry.Controller.DirectoryInode == 0 {
		return nil, syscall.ENOENT
	}
	dir, err := openDirectory(root, "controller")
	if err != nil {
		return nil, state("attributed controller directory is missing or unsafe")
	}
	if registry.Controller.DirectoryInode != dir.identity.Ino || registry.Controller.DirectoryDevice != uint64(dir.identity.Dev) {
		dir.file.Close()
		return nil, state("controller directory is unattributable or replaced; preserve the complete store for recovery")
	}
	return dir, nil
}

func readControllerStored(ctx context.Context, root *directory, registry contexts.Registry) (controllerStored, error) {
	if registry.Controller == (contexts.ControllerDescriptor{}) {
		return controllerStored{}, nil
	}
	if err := verifyRootEntries(ctx, root, registry); err != nil {
		return controllerStored{}, err
	}
	dir, err := openControllerDirectory(root, registry)
	if errors.Is(err, syscall.ENOENT) {
		return controllerStored{}, nil
	}
	if err != nil {
		return controllerStored{}, err
	}
	defer dir.file.Close()
	names, err := controllerNames(dir)
	if err != nil {
		return controllerStored{}, err
	}
	for _, name := range names {
		if name == "state.json" {
			continue
		}
		if name == "bundles" {
			child, err := openDirectory(dir, name)
			if err != nil {
				return controllerStored{}, err
			}
			child.file.Close()
			continue
		}
		if !pendingInitialRegistryName(name) {
			return controllerStored{}, state("controller directory contains unsupported state")
		}
		if err := verifyIgnoredRegistryStage(ctx, dir, name); err != nil {
			return controllerStored{}, err
		}
	}
	data, identity, err := readBoundedIdentity(ctx, dir, "state.json", maxControllerState, true)
	if errors.Is(err, syscall.ENOENT) && registry.Controller.Mode == "initializing" {
		if slices.Contains(names, "bundles") {
			return controllerStored{}, state("uninitialized controller contains unattributable tool areas")
		}
		return controllerStored{}, nil
	}
	if err != nil {
		return controllerStored{}, state("controller receipt is missing or unsafe")
	}
	value, bundles, err := decodeControllerRecord(data)
	if err != nil {
		return controllerStored{}, err
	}
	if err := validateControllerReferences(value, registry); err != nil {
		return controllerStored{}, err
	}
	if err := verifyControllerBundleReservations(ctx, dir, bundles); err != nil {
		return controllerStored{}, err
	}
	return controllerStored{value: value, bundles: bundles, data: data, identity: identity}, nil
}

// controllerNames lists the controller directory: its record, the tool area
// parent and at most maxControllerStages publication stages.
func controllerNames(dir *directory) ([]string, error) {
	names, err := directoryNames(dir, maxControllerStages+2)
	if err != nil {
		return nil, state("state directory entry count exceeds its limit")
	}
	return names, nil
}

// setupPrepared reports a completed setup in the setup subtree: a receipt, no
// interrupted setup and no other build's format.
func setupPrepared(ctx context.Context, root *directory) (bool, error) {
	dir, err := openDirectory(root, setupSubtree)
	if errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer dir.file.Close()
	view, err := readSetupView(ctx, dir)
	if err != nil {
		return false, err
	}
	return view.Exists && view.Foreign == "" && view.State.Pending == nil && view.State.Receipt != nil, nil
}

func controllerSnapshot(ctx context.Context, root *directory, registry contexts.Registry, name string) (prerequisites.StorageView, controllerStored, error) {
	stored, err := readControllerStored(ctx, root, registry)
	if err != nil {
		return prerequisites.StorageView{}, controllerStored{}, err
	}
	prepared, err := setupPrepared(ctx, root)
	if err != nil {
		return prerequisites.StorageView{}, controllerStored{}, err
	}
	view := prerequisites.StorageView{Exists: true, Initialized: registry.Controller.Mode == "ready", Prepared: prepared, State: cloneControllerState(stored.value), Areas: heldAreas(stored.bundles)}
	if name == "" {
		return view, stored, nil
	}
	if !contextName(name) {
		return prerequisites.StorageView{}, controllerStored{}, state("explicit controller context name is invalid")
	}
	for _, record := range registry.Contexts {
		if record.Name != name {
			continue
		}
		if record.Mode != contexts.Ready {
			return prerequisites.StorageView{}, controllerStored{}, contexts.NotReady(record)
		}
		if record.Revision == "" {
			return prerequisites.StorageView{}, controllerStored{}, contexts.MissingInput(record.Name)
		}
		sources, err := readSnapshot(ctx, root, record)
		if err != nil {
			return prerequisites.StorageView{}, controllerStored{}, err
		}
		view.Context = prerequisites.SetupContext{Name: record.Name, Revision: record.Revision}
		view.Sources = sources
		return view, stored, nil
	}
	return prerequisites.StorageView{}, controllerStored{}, contexts.AbsentContext(name)
}

func (s *Store) ReadController(ctx context.Context, name string, callback func(prerequisites.StorageView) error) error {
	if callback == nil {
		return state("controller inspection callback is missing")
	}
	if name != "" && !contextName(name) {
		return state("explicit controller context name is invalid")
	}
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		if name != "" {
			return contexts.AbsentContext(name)
		}
		active := true
		defer func() { active = false }()
		return callback(prerequisites.StorageView{OpenBundle: func(call context.Context, id string) (prerequisites.BundleArea, error) {
			if !active {
				return nil, state("controller inspection capability has closed")
			}
			if err := call.Err(); err != nil {
				return nil, err
			}
			if !validControllerDigest(id) {
				return nil, state("controller bundle identity is invalid")
			}
			return nil, nil
		}})
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
	if err == nil && exists {
		err = verifyRootEntries(ctx, root, registry)
	}
	if err == nil {
		err = verifyMappings(ctx, root, registry)
	}
	if err != nil {
		return safeError(err)
	}
	view, stored, err := controllerSnapshot(ctx, root, registry, name)
	if err != nil {
		return safeError(err)
	}
	view.Exists = exists
	active := true
	areas := []*controllerBundleArea{}
	defer func() {
		active = false
		for _, area := range areas {
			area.close()
		}
	}()
	view.OpenBundle = func(call context.Context, id string) (prerequisites.BundleArea, error) {
		if !active {
			return nil, state("controller inspection capability has closed")
		}
		guard := func(call context.Context) error {
			actual, err := readControllerStored(call, root, registry)
			if err != nil || !bytes.Equal(actual.data, stored.data) || actual.data != nil && !sameFile(actual.identity, stored.identity) {
				return state("controller evidence changed during inspection")
			}
			return nil
		}
		area, err := openControllerBundle(call, s, root, registry, stored, id, func() bool { return active }, false, guard)
		if err != nil {
			return nil, err
		}
		if area == nil {
			return nil, nil
		}
		areas = append(areas, area)
		return area, nil
	}
	return safeError(callback(view))
}

// initializeController attributes the controller directory and publishes the
// record holding only host's identity. Each step is durable before the next,
// and an unattributed directory is never adopted.
func (t *transaction) initializeController(ctx context.Context, host controller.InstalledHostIdentity, retry string) (controllerStored, error) {
	if !host.Valid() {
		return controllerStored{}, state("controller host evidence is invalid")
	}
	if t.registry.Controller == (contexts.ControllerDescriptor{}) {
		registry := cloneRegistry(t.registry)
		registry.Controller = contexts.ControllerDescriptor{Version: 1, Mode: "initializing"}
		if err := t.save(ctx, registry); err != nil {
			return controllerStored{}, err
		}
	}
	if t.registry.Controller.DirectoryInode == 0 {
		if dir, err := openDirectory(t.root, "controller"); err == nil {
			dir.file.Close()
			return controllerStored{}, state("existing controller directory cannot be adopted")
		} else if !errors.Is(err, syscall.ENOENT) {
			return controllerStored{}, err
		}
		dir, err := t.store.newDirectory(ctx, t.root, "controller")
		if err != nil {
			return controllerStored{}, err
		}
		identity := dir.identity
		dir.file.Close()
		if err := t.store.checkpoint(ctx, checkpointAfterControllerDirectory); err != nil {
			return controllerStored{}, err
		}
		registry := cloneRegistry(t.registry)
		registry.Controller.DirectoryDevice, registry.Controller.DirectoryInode = uint64(identity.Dev), identity.Ino
		if err := t.save(ctx, registry); err != nil {
			return controllerStored{}, err
		}
	}
	stored, err := readControllerStored(ctx, t.root, t.registry)
	if err != nil {
		return controllerStored{}, err
	}
	if stored.data == nil {
		dir, err := openControllerDirectory(t.root, t.registry)
		if err != nil {
			return controllerStored{}, err
		}
		value := prerequisites.HostState{Host: host, Bindings: []prerequisites.ControllerBinding{}, ToolSources: []prerequisites.DependencySource{}}
		data, err := encodeRecord(controllerRecord(value, nil), maxControllerState)
		if err == nil {
			_, err = t.store.replaceControllerRecord(ctx, dir, stored, data, retry)
		}
		dir.file.Close()
		if err != nil {
			return controllerStored{}, err
		}
	}
	if t.registry.Controller.Mode != "ready" {
		registry := cloneRegistry(t.registry)
		registry.Controller.Mode = "ready"
		if err := t.save(ctx, registry); err != nil {
			return controllerStored{}, err
		}
	}
	return readControllerStored(ctx, t.root, t.registry)
}

func (s *Store) replaceControllerRecord(ctx context.Context, dir *directory, expected controllerStored, data []byte, retry string) (prerequisites.Publication, error) {
	names, err := controllerNames(dir)
	if err != nil || len(names) >= maxControllerStages+2 {
		return prerequisites.NotCommitted, state("controller publication stages exceed their bound")
	}
	outcome, err := s.publishStage(ctx, dir, "state.json", data, stagedPublication{
		subject: "controller receipt", suffix: ".json", replace: expected.data != nil, immutable: true,
		bound: maxControllerState, renameFailure: "controller receipt could not be atomically published",
		prove: func(ctx context.Context) error {
			current, identity, err := readBoundedIdentity(ctx, dir, "state.json", maxControllerState, true)
			if expected.data == nil {
				if !errors.Is(err, syscall.ENOENT) {
					return state("initial controller receipt destination exists")
				}
				return nil
			}
			if err != nil || !bytes.Equal(current, expected.data) || !sameFile(identity, expected.identity) {
				return state("controller receipt changed before publication")
			}
			return nil
		},
	}, checkpointBeforeControllerRename, checkpointAfterControllerRename)
	switch outcome {
	case publicationCommitted:
		return prerequisites.Committed, nil
	case publicationUnknown:
		return prerequisites.Unknown, controllerFailure("controller.unknown", "controller receipt publication may have completed; preserve all verified progress", retry)
	}
	return prerequisites.NotCommitted, err
}

func (t *transaction) checkControllerRecovery(ctx context.Context, name string) error {
	stored, err := readControllerStored(ctx, t.root, t.registry)
	if err != nil {
		return err
	}
	if expected := t.controllerEvidence; expected != nil {
		if !bytes.Equal(expected.data, stored.data) || stored.data != nil && !sameFile(expected.identity, stored.identity) {
			return state("controller recovery evidence changed during context mutation")
		}
	} else {
		expected := stored
		t.controllerEvidence = &expected
	}
	return nil
}

func (t *transaction) CheckControllerInput(ctx context.Context, name, machine string) error {
	if err := t.available(ctx); err != nil {
		return err
	}
	if err := t.checkControllerRecovery(ctx, name); err != nil {
		return err
	}
	stored, err := readControllerStored(ctx, t.root, t.registry)
	if err != nil {
		return err
	}
	for _, binding := range stored.value.Bindings {
		if binding.Context == name && binding.Machine != machine {
			return state("replacement input changes the established controller Machine; create a separate context")
		}
	}
	if t.controllerInputs == nil {
		t.controllerInputs = make(map[string]string)
	}
	t.controllerInputs[name] = machine
	return nil
}

func (t *transaction) checkControllerPublication(ctx context.Context, name string) error {
	stored, err := readControllerStored(ctx, t.root, t.registry)
	if err != nil {
		return err
	}
	for _, binding := range stored.value.Bindings {
		if binding.Context == name && t.controllerInputs[name] != binding.Machine {
			return state("input publication requires preservation of its controller Machine binding")
		}
	}
	return nil
}

// HostReservations names every host resource key the controller record
// reserves for one context, which its deletion releases.
func (t *transaction) HostReservations(ctx context.Context, name string) ([]string, error) {
	if err := t.available(ctx); err != nil {
		return nil, err
	}
	stored, err := readControllerStored(ctx, t.root, t.registry)
	if err != nil || stored.data == nil {
		return nil, err
	}
	keys := []string{}
	for _, reservation := range stored.value.Reservations {
		if reservation.Context == name {
			keys = append(keys, reservation.Keys...)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys), nil
}

// dropContextClaims removes what the controller record holds for a context
// being deleted: its binding and its host reservations. A deleted context can
// hold neither, and a reservation it kept would refuse every other context the
// keys it names, with no context left to release them.
func (t *transaction) dropContextClaims(ctx context.Context, name string) error {
	if err := t.checkControllerRecovery(ctx, name); err != nil {
		return err
	}
	stored, err := readControllerStored(ctx, t.root, t.registry)
	if err != nil || stored.data == nil {
		return err
	}
	value := cloneControllerState(stored.value)
	value.Bindings = slices.DeleteFunc(value.Bindings, func(binding prerequisites.ControllerBinding) bool { return binding.Context == name })
	value.Reservations = slices.DeleteFunc(value.Reservations, func(reservation prerequisites.HostReservation) bool { return reservation.Context == name })
	if len(value.Bindings) == len(stored.value.Bindings) && len(value.Reservations) == len(stored.value.Reservations) {
		return nil
	}
	dir, err := openControllerDirectory(t.root, t.registry)
	if err != nil {
		return err
	}
	defer dir.file.Close()
	record := controllerRecord(value, stored.bundles)
	data, err := encodeRecord(record, maxControllerState)
	if err != nil {
		return err
	}
	_, err = t.store.replaceControllerRecord(ctx, dir, stored, data, deletionRetry(name))
	if err == nil {
		current, readErr := readControllerStored(ctx, t.root, t.registry)
		if readErr != nil {
			return readErr
		}
		t.controllerEvidence = &current
	}
	return err
}
