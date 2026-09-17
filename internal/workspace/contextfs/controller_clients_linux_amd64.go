//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// ClientArea opens the shared host area one exact client closure is published
// into. The area is content-addressed by that closure, so every context
// selecting the same clients proves the same files and a different closure
// never disturbs them.
//
// Publication follows the setup bundle's rules with the lifecycle operation as
// its intent: the reservation is published before the directory exists, the
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
	if t.stored.data == nil || t.stored.value.Receipt.Status != "complete" {
		return nil, controllerFailure("controller.identity", "this host has no completed controller setup; run bootwright setup")
	}
	if id == t.stored.value.Receipt.CatalogDigest {
		return nil, state("the approved setup bundle is not a client publication area")
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

// attributeClientArea publishes the reservation, then creates the directory,
// then records its physical identity. The record is durable before the
// directory exists, so a crash leaves this store's own recognizable intent
// rather than an orphan no publication can explain.
func (t *lifecycleTransaction) attributeClientArea(ctx context.Context, id string, fresh, interrupted bool) error {
	if fresh {
		if len(t.stored.bundles) >= maxControllerBundles {
			return state("retained controller bundle limit exceeded")
		}
		next := append(slices.Clone(t.stored.bundles), controllerBundleReservation{ID: id, Mode: "reserved"})
		slices.SortFunc(next, func(x, y controllerBundleReservation) int { return strings.Compare(x.ID, y.ID) })
		if err := t.publishControllerState(ctx, cloneControllerState(t.stored.value), next, "before-client-area-reservation"); err != nil {
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
		if err := t.base.store.checkpoint(ctx, "after-client-area-directory"); err != nil {
			return err
		}
	}
	next := slices.Clone(t.stored.bundles)
	index := slices.IndexFunc(next, func(item controllerBundleReservation) bool { return item.ID == id })
	if index < 0 {
		return state("controller client area reservation was lost before attribution")
	}
	next[index] = controllerBundleReservation{ID: id, Mode: "attributed", DirectoryDevice: uint64(identity.Dev), DirectoryInode: identity.Ino}
	return t.publishControllerState(ctx, cloneControllerState(t.stored.value), next, "before-client-area-attribution")
}

// SealClientArea makes the published closure immutable. Sealing verifies and
// syncs the complete tree first, so a sealed area is durable evidence rather
// than a claim about files that may still be in flight.
func (t *lifecycleTransaction) SealClientArea(ctx context.Context, id string) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	index := slices.IndexFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == id })
	if index < 0 || t.stored.bundles[index].DirectoryInode == 0 {
		return state("controller client area is not attributed")
	}
	if t.stored.bundles[index].Mode == "sealed" {
		return nil
	}
	area := t.openArea(id)
	if area == nil {
		return state("controller client area is not open in this operation")
	}
	if err := area.sync(ctx); err != nil {
		return err
	}
	next := slices.Clone(t.stored.bundles)
	next[index].Mode = "sealed"
	return t.publishControllerState(ctx, cloneControllerState(t.stored.value), next, "before-client-area-sealing")
}

func (t *lifecycleTransaction) openArea(id string) *controllerBundleArea {
	return t.areas.find(id)
}

// RetainDependencies records what a controller stage resolved before it
// installs anything. Sources are the immutable acquisition identities every
// later inspection recovers a closure from; a resolution carrying the native
// packages a context selects is retained beside them so presence-only
// readiness can prove those roots without resolving again.
func (t *lifecycleTransaction) RetainDependencies(ctx context.Context, definition *prerequisites.Definition, sources []prerequisites.DependencySource) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if t.stored.data == nil || t.stored.value.Receipt.Status != "complete" {
		return controllerFailure("controller.identity", "this host has no completed controller setup; run bootwright setup")
	}
	value, err := retainDependencies(cloneControllerState(t.stored.value), definition, sources)
	if err != nil {
		return err
	}
	return t.publishControllerState(ctx, value, t.stored.bundles, "before-retained-dependencies")
}

func retainDependencies(value prerequisites.HostState, definition *prerequisites.Definition, sources []prerequisites.DependencySource) (prerequisites.HostState, error) {
	merged := make(map[string]prerequisites.DependencySource, len(value.RetainedSources)+len(sources))
	for _, source := range slices.Concat(value.RetainedSources, sources) {
		if prior, exists := merged[source.ID]; exists && prior != source {
			return prerequisites.HostState{}, state("a retained controller dependency source cannot be replaced")
		}
		merged[source.ID] = source
	}
	if len(merged) > maxControllerRetainedSources {
		return prerequisites.HostState{}, state("retained controller dependency limit exceeded")
	}
	value.RetainedSources = slices.SortedFunc(maps.Values(merged), func(x, y prerequisites.DependencySource) int {
		return strings.Compare(x.ID, y.ID)
	})
	if definition == nil {
		return value, nil
	}
	index := slices.IndexFunc(value.RetainedDefinitions, func(item prerequisites.Definition) bool {
		return item.ResolutionDigest == definition.ResolutionDigest
	})
	if index >= 0 {
		if !prerequisites.SameDefinition(value.RetainedDefinitions[index], *definition) {
			return prerequisites.HostState{}, state("a controller stage would replace immutable resolution evidence")
		}
		return value, nil
	}
	if len(value.RetainedDefinitions) >= maxControllerBundles {
		return prerequisites.HostState{}, state("retained controller resolution limit exceeded")
	}
	value.RetainedDefinitions = append(value.RetainedDefinitions, prerequisites.CloneDefinition(*definition))
	return value, nil
}
