//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

var _ prerequisites.Storage = (*Store)(nil)

type controllerStored struct {
	value    prerequisites.HostState
	bundles  []controllerBundleReservation
	data     []byte
	identity syscall.Stat_t
	runs     bool
}

// setupRetry settles a refusal setup itself meets: only the exact setup that
// recorded the controller state may continue it.
const setupRetry = "repeat the same controller setup command with its original input and compatible executable"

// completeSetup settles a command other than setup that needs the completed
// setup this host lacks.
const completeSetup = "run bootwright setup, then repeat the command"

// controllerFailure is a controller-state refusal and the action that settles
// it, which belongs to the command that met it.
func controllerFailure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
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
	names, _, err := controllerNames(dir)
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
		if name == setupRunsName {
			if _, err := verifyControllerRuns(ctx, dir); err != nil {
				return controllerStored{}, err
			}
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
			return controllerStored{}, state("uninitialized controller contains unattributable bundles")
		}
		if slices.Contains(names, setupRunsName) {
			return controllerStored{}, state("uninitialized controller contains unattributable setup runs")
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
	return controllerStored{value: value, bundles: bundles, data: data, identity: identity, runs: slices.Contains(names, setupRunsName)}, nil
}

func controllerSnapshot(ctx context.Context, root *directory, registry contexts.Registry, name string) (prerequisites.StorageView, controllerStored, error) {
	stored, err := readControllerStored(ctx, root, registry)
	if err != nil {
		return prerequisites.StorageView{}, controllerStored{}, err
	}
	view := prerequisites.StorageView{Exists: true, Initialized: registry.Controller.Mode == "ready", State: cloneControllerState(stored.value), Areas: heldAreas(stored.bundles), SetupRuns: stored.runs}
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

func (s *Store) MutateController(ctx context.Context, expected prerequisites.SetupContext, create bool, callback func(prerequisites.StorageTransaction) error) error {
	if callback == nil {
		return state("controller mutation callback is missing")
	}
	if expected.Name == "" && expected != (prerequisites.SetupContext{}) || expected.Name != "" && (!contextName(expected.Name) || !identifier(expected.Revision, "rev-") || !contextName(expected.Machine)) {
		return state("controller mutation requires exact scope and input evidence")
	}
	err := s.Transact(ctx, create && expected.Name == "", nil, func(base contexts.Transaction) error {
		t := base.(*transaction)
		view, stored, err := controllerSnapshot(ctx, t.root, t.registry, expected.Name)
		if err != nil {
			return err
		}
		if view.Context.Name != expected.Name || view.Context.Revision != expected.Revision {
			return controllerFailure("controller.conflict", "controller input changed after setup inspection", setupRetry)
		}
		view.Context.Machine = expected.Machine
		if expected.Name != "" {
			if _, err := t.leaseContext(ctx, expected.Name); err != nil {
				return err
			}
			if err := t.CheckControllerInput(ctx, expected.Name, expected.Machine); err != nil {
				// Exact setup retry owns its pending receipt; ordinary input guards
				// refuse it. Binding comparison is independently enforced below.
				if stored.data == nil || !stored.value.Receipt.Incomplete() || stored.value.Receipt.Context != expected {
					return err
				}
			}
		}
		if stored.data != nil && stored.value.Receipt.Incomplete() && stored.value.Receipt.Context != expected {
			return controllerFailure("controller.conflict", "another setup receipt requires exact recovery before host mutation", setupRetry)
		}
		for _, binding := range stored.value.Bindings {
			if binding.Context == expected.Name && binding.Machine != expected.Machine {
				return controllerFailure("controller.identity", "controller Machine differs from its established host binding", setupRetry)
			}
		}
		tx := &controllerTransaction{base: t, view: view, stored: stored, active: true}
		defer func() {
			tx.active = false
			for _, area := range tx.areas {
				area.close()
			}
		}()
		return callback(tx)
	})
	switch {
	case !errors.Is(err, contexts.ErrNoContexts):
		return err
	case expected.Name != "":
		return contexts.AbsentContext(expected.Name)
	}
	return state("context store does not exist")
}

type controllerTransaction struct {
	base      *transaction
	view      prerequisites.StorageView
	stored    controllerStored
	active    bool
	uncertain bool
	areas     []*controllerBundleArea
}

func (t *controllerTransaction) Snapshot() prerequisites.StorageView {
	if !t.active {
		return prerequisites.StorageView{}
	}
	view := t.view
	view.State = cloneControllerState(t.stored.value)
	view.Areas = heldAreas(t.stored.bundles)
	view.Sources.Files = slices.Clone(view.Sources.Files)
	view.Sources.Markers = slices.Clone(view.Sources.Markers)
	view.Sources.Roots = slices.Clone(view.Sources.Roots)
	view.OpenBundle = func(ctx context.Context, id string) (prerequisites.BundleArea, error) {
		if err := t.available(ctx); err != nil {
			return nil, err
		}
		area, err := openControllerBundle(ctx, t.base.store, t.base.root, t.base.registry, t.stored, id, func() bool { return t.active && !t.uncertain }, false, t.available)
		if err != nil {
			return nil, err
		}
		if area == nil {
			return nil, nil
		}
		t.areas = append(t.areas, area)
		return area, nil
	}
	return view
}

func (t *controllerTransaction) available(ctx context.Context) error {
	if !t.active || t.uncertain {
		return controllerFailure("controller.unknown", "controller storage capability is no longer available", setupRetry)
	}
	if err := t.base.available(ctx); err != nil {
		return err
	}
	actual, err := registryExpectation(ctx, t.base.root, t.base.registry)
	if err != nil || t.base.expected == nil || !sameFile(actual.identity, t.base.expected.identity) || !bytes.Equal(actual.data, t.base.expected.data) {
		return state("context registry changed during controller setup")
	}
	if t.view.Context.Name != "" {
		dir := t.base.leases[t.view.Context.Name]
		if dir == nil {
			return state("controller context lease is missing")
		}
		if err := dir.verify(); err != nil {
			return err
		}
		if err := verifyReservation(ctx, dir, t.view.Context.Name); err != nil {
			return err
		}
	}
	current, err := readControllerStored(ctx, t.base.root, t.base.registry)
	if err != nil {
		return err
	}
	if !bytes.Equal(current.data, t.stored.data) || current.data != nil && !sameFile(current.identity, t.stored.identity) {
		return state("controller receipt changed during its transaction")
	}
	return nil
}

func validateControllerTransition(before, next prerequisites.HostState, scope prerequisites.SetupContext) error {
	if next.Receipt.Context != scope {
		return state("setup receipt differs from its held context scope")
	}
	if !before.Host.Valid() {
		for _, action := range next.Receipt.Actions {
			if len(action.Preparation) != 0 {
				return state("controller before-state requires an existing durable action intent")
			}
		}
		return nil
	}
	if !before.Host.Equal(next.Host) {
		return controllerFailure("controller.identity", "stored controller host evidence does not match the executing host", setupRetry)
	}
	for _, binding := range before.Bindings {
		if !slices.Contains(next.Bindings, binding) {
			return state("setup cannot remove or replace an established context binding")
		}
	}
	for _, binding := range next.Bindings {
		if !slices.Contains(before.Bindings, binding) && (binding.Context != scope.Name || binding.Machine != scope.Machine) {
			return state("setup cannot publish another context's host binding")
		}
	}
	if before.Receipt.ID != next.Receipt.ID {
		if before.Receipt.Incomplete() {
			return controllerFailure("controller.unknown", "pending setup must be resolved before replacing its receipt", setupRetry)
		}
		for _, action := range next.Receipt.Actions {
			if len(action.Preparation) != 0 {
				return state("controller before-state requires an existing durable action intent")
			}
		}
		return nil
	}
	if before.Receipt.PlanDigest != next.Receipt.PlanDigest {
		return controllerFailure("controller.unknown", "exact setup retry requires the original host, input, catalog and plan", setupRetry)
	}
	if len(before.Receipt.Actions) != len(next.Receipt.Actions) {
		return state("setup retry changed its fixed action count")
	}
	if before.Receipt.Status == "complete" && next.Receipt.Status != "complete" {
		return state("completed setup cannot become pending again")
	}
	for index, previous := range before.Receipt.Actions {
		now := next.Receipt.Actions[index]
		if len(previous.Preparation) != 0 && !bytes.Equal(previous.Preparation, now.Preparation) {
			return state("setup cannot discard or replace durable action before-state")
		}
		if len(previous.Preparation) == 0 && len(now.Preparation) != 0 && (previous.Phase != "intent" || now.Phase != "intent") {
			return state("controller before-state requires the same durable action intent")
		}
		if previous.Phase == "intent" && now.Phase == "planned" || previous.Phase == "observed" && previous.Outcome == "unknown" && now.Phase != "observed" {
			return state("setup retry cannot presume an unresolved effect did not occur")
		}
		if previous.Phase == "observed" && (previous.Outcome == "changed" || previous.Outcome == "unchanged") && (now.Phase != previous.Phase || now.Outcome != previous.Outcome || !bytes.Equal(now.Evidence, previous.Evidence)) {
			return state("setup cannot discard a verified action postcondition")
		}
	}
	return nil
}

func (t *controllerTransaction) Publish(ctx context.Context, requested prerequisites.HostState) (prerequisites.Publication, error) {
	if err := t.available(ctx); err != nil {
		return prerequisites.NotCommitted, err
	}
	if len(requested.Bindings) > maxContexts || len(requested.RetainedSources) > maxControllerRetainedSources || len(requested.Receipt.Sources) > maxControllerSources || len(requested.Receipt.Actions) > maxControllerActions {
		return prerequisites.NotCommitted, state("controller publication exceeds its collection bounds")
	}
	next, err := retainControllerSources(t.stored.value, cloneControllerState(requested))
	if err != nil {
		return prerequisites.NotCommitted, err
	}
	if err := validateControllerState(next); err != nil {
		return prerequisites.NotCommitted, err
	}
	if err := validateControllerReferences(next, t.base.registry); err != nil {
		return prerequisites.NotCommitted, err
	}
	if err := validateControllerTransition(t.stored.value, next, t.view.Context); err != nil {
		return prerequisites.NotCommitted, err
	}
	// A new receipt names the bundle it will publish. One this store could not
	// reserve would stay pending with no way to complete, and no later setup
	// could replace it, so it is refused while the record is still unchanged.
	if next.Receipt.ID != t.stored.value.Receipt.ID && len(t.stored.bundles) >= maxControllerBundles &&
		!slices.ContainsFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == next.Receipt.CatalogDigest }) {
		return prerequisites.NotCommitted, state("retained controller bundle limit exceeded")
	}
	// First publication also admits only this context's new binding.
	for _, binding := range next.Bindings {
		if !slices.Contains(t.stored.value.Bindings, binding) && (binding.Context != t.view.Context.Name || binding.Machine != t.view.Context.Machine) {
			return prerequisites.NotCommitted, state("setup binding lacks its exact context lease")
		}
	}
	if t.stored.data != nil {
		candidate := controllerRecord(next, t.stored.bundles)
		data, err := encodeRecord(candidate, maxControllerState)
		if err != nil {
			return prerequisites.NotCommitted, err
		}
		if bytes.Equal(data, t.stored.data) && t.base.registry.Controller.Mode == "ready" {
			// Explicit retry establishes durability even if an earlier process
			// died after rename. No record is replaced on this path.
			dir, err := openControllerDirectory(t.base.root, t.base.registry)
			if err != nil {
				return prerequisites.NotCommitted, err
			}
			defer dir.file.Close()
			if err := t.base.store.syncDirectory(ctx, dir); err != nil {
				t.uncertain = true
				return prerequisites.Unknown, controllerFailure("controller.unknown", "controller receipt durability could not be established", setupRetry)
			}
			if err := t.available(ctx); err != nil {
				t.uncertain = true
				return prerequisites.Unknown, err
			}
			return prerequisites.Committed, nil
		}
	}
	if next.Receipt.Status == "complete" {
		for _, bundle := range t.stored.bundles {
			if bundle.ID != next.Receipt.CatalogDigest || bundle.Mode != "attributed" {
				continue
			}
			area, err := openControllerBundle(ctx, t.base.store, t.base.root, t.base.registry, t.stored, bundle.ID, func() bool { return t.active && !t.uncertain }, false, t.available)
			if err != nil {
				return prerequisites.NotCommitted, err
			}
			err = area.sync(ctx)
			area.close()
			if err != nil {
				return prerequisites.NotCommitted, err
			}
		}
	}
	if err := t.ensureController(ctx); err != nil {
		t.uncertain = true
		return prerequisites.Unknown, err
	}
	outcome, err := t.publishValue(ctx, next, t.stored.bundles)
	if outcome == prerequisites.Unknown {
		t.uncertain = true
	}
	return outcome, err
}

func (t *controllerTransaction) ensureController(ctx context.Context) error {
	registry := t.base.registry
	if registry.Controller == (contexts.ControllerDescriptor{}) {
		if err := verifyRootEntries(ctx, t.base.root, registry); err != nil {
			return err
		}
		registry = cloneRegistry(registry)
		registry.Controller = contexts.ControllerDescriptor{Version: 1, Mode: "initializing"}
		if err := t.base.save(ctx, registry); err != nil {
			return err
		}
	}
	if t.base.registry.Controller.DirectoryInode != 0 {
		return nil
	}
	dir, err := openDirectory(t.base.root, "controller")
	if err == nil {
		dir.file.Close()
		return state("existing controller directory cannot be adopted")
	}
	if !errors.Is(err, syscall.ENOENT) {
		return err
	}
	dir, err = t.base.store.newDirectory(ctx, t.base.root, "controller")
	if err != nil {
		return err
	}
	defer dir.file.Close()
	if err := t.base.store.checkpoint(ctx, checkpointAfterControllerDirectory); err != nil {
		return err
	}
	registry = cloneRegistry(t.base.registry)
	registry.Controller.DirectoryDevice = uint64(dir.identity.Dev)
	registry.Controller.DirectoryInode = dir.identity.Ino
	return t.base.save(ctx, registry)
}

func (t *controllerTransaction) publishValue(ctx context.Context, value prerequisites.HostState, bundles []controllerBundleReservation) (prerequisites.Publication, error) {
	dir, err := openControllerDirectory(t.base.root, t.base.registry)
	if err != nil {
		return prerequisites.NotCommitted, err
	}
	defer dir.file.Close()
	record := controllerRecord(value, bundles)
	if value.Receipt.Status == "complete" {
		for index := range record.Bundles {
			if record.Bundles[index].ID == value.Receipt.CatalogDigest && record.Bundles[index].Mode == "attributed" {
				record.Bundles[index].Mode = "sealed"
			}
		}
	}
	data, err := encodeRecord(record, maxControllerState)
	if err != nil {
		return prerequisites.NotCommitted, err
	}
	outcome, err := t.base.store.replaceControllerRecord(ctx, dir, t.stored, data)
	if err != nil {
		if outcome == prerequisites.Unknown {
			t.uncertain = true
		}
		return outcome, err
	}
	if t.base.registry.Controller.Mode != "ready" {
		registry := cloneRegistry(t.base.registry)
		registry.Controller.Mode = "ready"
		if err := t.base.save(ctx, registry); err != nil {
			t.uncertain = true
			return prerequisites.Unknown, controllerFailure("controller.unknown", "controller receipt publication may have completed", setupRetry)
		}
	}
	stored, err := readControllerStored(ctx, t.base.root, t.base.registry)
	if err != nil {
		t.uncertain = true
		return prerequisites.Unknown, controllerFailure("controller.unknown", "published controller receipt could not be reverified", setupRetry)
	}
	t.stored = stored
	t.view.State = cloneControllerState(stored.value)
	t.view.Initialized = true
	return prerequisites.Committed, nil
}

func (s *Store) replaceControllerRecord(ctx context.Context, dir *directory, expected controllerStored, data []byte) (prerequisites.Publication, error) {
	_, counted, err := controllerNames(dir)
	if err != nil || counted >= maxControllerStages+2 {
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
		return prerequisites.Unknown, controllerFailure("controller.unknown", "controller receipt publication may have completed; preserve all verified progress", setupRetry)
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
	if stored.data != nil && stored.value.Receipt.Incomplete() && stored.value.Receipt.Context.Name == name {
		return state("context input is protected by incomplete controller setup; repeat its exact setup command")
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
	_, err = t.store.replaceControllerRecord(ctx, dir, stored, data)
	if err == nil {
		current, readErr := readControllerStored(ctx, t.root, t.registry)
		if readErr != nil {
			return readErr
		}
		t.controllerEvidence = &current
	}
	return err
}
