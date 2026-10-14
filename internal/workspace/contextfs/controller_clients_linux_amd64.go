//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// ClientArea opens the shared host area one exact client closure is published
// into. The area is content-addressed by that closure, so every context
// selecting the same clients proves the same files and a different closure
// never disturbs them.
//
// Publication takes the lifecycle operation as its intent: the reservation is published before the directory exists, the
// actual directory identity is recorded in a second publication, and a sealed
// area is immutable and reopens read-only. An unattributed directory beside a
// reservation this store published is its own interrupted attempt and may be
// adopted only while empty; anything else refuses.
func (t *lifecycleTransaction) ClientArea(ctx context.Context, id string) (prerequisites.BundleArea, error) {
	if err := t.base.available(ctx); err != nil {
		return nil, err
	}
	if !validControllerDigest(id) {
		return nil, state("controller client area identity is invalid")
	}
	if t.stored.data == nil || !t.controller.Prepared {
		return nil, controllerFailure("controller.identity", "this host has no completed controller setup", completeSetup)
	}
	index := slices.IndexFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == id })
	interrupted := index >= 0 && t.stored.bundles[index].Mode == "reserved"
	if index < 0 || t.stored.bundles[index].DirectoryInode == 0 {
		if err := t.attributeClientArea(ctx, id, index < 0, interrupted); err != nil {
			return nil, err
		}
	}
	sealed := func() bool {
		return slices.ContainsFunc(t.stored.bundles, func(item controllerBundleReservation) bool {
			return item.ID == id && item.Mode == "sealed"
		})
	}
	area, err := openControllerBundle(ctx, t.base.store, t.base.root, t.base.registry, t.stored, id, t.active, !sealed(), t.guard)
	if err != nil || area == nil {
		if err == nil {
			err = state("controller client area is not reserved")
		}
		return nil, err
	}
	area.canWrite = func() bool { return !sealed() }
	area.sealed = sealed
	t.areas.keep(area)
	return area, nil
}

// clientBoundFailure refuses a controller stage whose client closure finds the
// host's bound already held. No command of this build retires a tool area.
func clientBoundFailure(held string) error {
	return controllerFailure("controller.conflict", "this host already holds "+held+", so the controller stage's client closure has no room",
		"no command of this build frees a tool area; run this context's controller stage on another controller host")
}

// attributeClientArea publishes the reservation, then creates the directory,
// then records its physical identity. The record is durable before the
// directory exists, so a crash leaves this store's own recognizable intent
// rather than an orphan no publication can explain.
func (t *lifecycleTransaction) attributeClientArea(ctx context.Context, id string, fresh, interrupted bool) error {
	if fresh {
		if len(t.stored.bundles) >= maxControllerBundles {
			return clientBoundFailure("the " + strconv.Itoa(maxControllerBundles) + " tool areas this host may hold")
		}
		next := append(slices.Clone(t.stored.bundles), controllerBundleReservation{ID: id, Mode: "reserved"})
		slices.SortFunc(next, func(x, y controllerBundleReservation) int { return strings.Compare(x.ID, y.ID) })
		if err := t.publishControllerState(ctx, cloneControllerState(t.stored.value), next, applyRetry(t.identity.Name), checkpointBeforeClientAreaReservation); err != nil {
			return err
		}
	}
	owner, err := openControllerDirectory(t.base.root, t.base.registry)
	if err != nil {
		return err
	}
	defer owner.file.Close()
	parent, err := t.base.store.ensureDirectory(ctx, owner, "bundles")
	if err != nil {
		return err
	}
	defer parent.file.Close()
	var identity syscall.Stat_t
	switch existing, err := openDirectory(parent, id); {
	case err == nil:
		entries, listErr := directoryNames(existing, 1)
		identity = existing.identity
		existing.file.Close()
		if listErr != nil {
			return listErr
		}
		if !fresh && !interrupted || len(entries) != 0 {
			return state("existing controller client area cannot be adopted")
		}
	case !errors.Is(err, syscall.ENOENT):
		return err
	default:
		dir, err := t.base.store.newDirectory(ctx, parent, id)
		if err != nil {
			return err
		}
		defer dir.file.Close()
		identity = dir.identity
		if err := t.base.store.checkpoint(ctx, checkpointAfterClientAreaDirectory); err != nil {
			return err
		}
	}
	next := slices.Clone(t.stored.bundles)
	index := slices.IndexFunc(next, func(item controllerBundleReservation) bool { return item.ID == id })
	if index < 0 {
		return state("controller client area reservation was lost before attribution")
	}
	next[index] = controllerBundleReservation{ID: id, Mode: "attributed", DirectoryDevice: uint64(identity.Dev), DirectoryInode: identity.Ino}
	return t.publishControllerState(ctx, cloneControllerState(t.stored.value), next, applyRetry(t.identity.Name), checkpointBeforeClientAreaAttribution)
}

// SealClientArea makes the published closure immutable and records it as the
// one this context's binding last published. Sealing verifies and syncs the
// complete tree first, so a sealed area is durable evidence rather than a
// claim about files that may still be in flight.
func (t *lifecycleTransaction) SealClientArea(ctx context.Context, id string) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	index := slices.IndexFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == id })
	if index < 0 || t.stored.bundles[index].DirectoryInode == 0 {
		return state("controller client area is not attributed")
	}
	next := slices.Clone(t.stored.bundles)
	if next[index].Mode != "sealed" {
		area := t.openArea(id)
		if area == nil {
			return state("controller client area is not open in this operation")
		}
		if err := area.sync(ctx); err != nil {
			return err
		}
		next[index].Mode = "sealed"
	}
	value := cloneControllerState(t.stored.value)
	bound := slices.IndexFunc(value.Bindings, func(binding prerequisites.ControllerBinding) bool { return binding.Context == t.identity.Name })
	if bound >= 0 {
		value.Bindings[bound].Tools = id
	}
	if next[index].Mode == t.stored.bundles[index].Mode && (bound < 0 || t.stored.value.Bindings[bound].Tools == id) {
		return nil
	}
	return t.publishControllerState(ctx, value, next, applyRetry(t.identity.Name), checkpointBeforeClientAreaSealing)
}

func (t *lifecycleTransaction) openArea(id string) *controllerBundleArea {
	return t.areas.find(id)
}

// RetainDependencies records the tool sources a controller stage resolved
// before it installs anything: the immutable acquisition identities every
// later inspection recovers a closure from.
func (t *lifecycleTransaction) RetainDependencies(ctx context.Context, sources []prerequisites.DependencySource) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if t.stored.data == nil || !t.controller.Prepared {
		return controllerFailure("controller.identity", "this host has no completed controller setup", completeSetup)
	}
	value, err := retainToolSources(cloneControllerState(t.stored.value), sources)
	if err != nil {
		return err
	}
	return t.publishControllerState(ctx, value, t.stored.bundles, applyRetry(t.identity.Name), checkpointBeforeRetainedDependencies)
}

func retainToolSources(value prerequisites.HostState, sources []prerequisites.DependencySource) (prerequisites.HostState, error) {
	merged := make(map[string]prerequisites.DependencySource, len(value.ToolSources)+len(sources))
	for _, source := range slices.Concat(value.ToolSources, sources) {
		if prior, exists := merged[source.ID]; exists && prior != source {
			return prerequisites.HostState{}, state("a retained controller dependency source cannot be replaced")
		}
		merged[source.ID] = source
	}
	if len(merged) > maxControllerToolSources {
		return prerequisites.HostState{}, controllerFailure("controller.conflict", "this host already retains the "+strconv.Itoa(maxControllerToolSources)+" tool source identities it may hold, so this closure's sources have no room", toolSourcesRemedy)
	}
	value.ToolSources = slices.SortedFunc(maps.Values(merged), func(x, y prerequisites.DependencySource) int {
		return strings.Compare(x.ID, y.ID)
	})
	return value, nil
}
