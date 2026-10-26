//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// retireToolAreas publishes value and retires every sealed tool area no
// binding in it names and keep does not hold, finishing any retirement an
// earlier command began. An unsealed area is an unfinished stage's intent and
// stays. Each step is durable before the next: the entries turn retiring, the
// directories go, then the entries go.
func (t *transaction) retireToolAreas(ctx context.Context, stored controllerStored, value prerequisites.HostState, keep func(string) bool, retry string) (controllerStored, error) {
	named := map[string]bool{}
	for _, binding := range value.Bindings {
		named[binding.Tools] = true
	}
	areas := slices.Clone(stored.bundles)
	retiring := false
	for index, area := range areas {
		if area.Mode == "sealed" && !named[area.ID] && (keep == nil || !keep(area.ID)) {
			areas[index].Mode = retiringArea
		}
		retiring = retiring || areas[index].Mode == retiringArea
	}
	data, err := encodeRecord(controllerRecord(value, areas), maxControllerState)
	if err != nil {
		return stored, err
	}
	if !bytes.Equal(data, stored.data) {
		if err := t.store.checkpoint(ctx, checkpointBeforeToolAreaRetiring); err != nil {
			return stored, err
		}
		if stored, err = t.publishToolAreas(ctx, stored, value, areas, retry); err != nil {
			return stored, err
		}
	}
	if !retiring {
		return stored, nil
	}
	if err := t.removeRetiringAreas(ctx, stored.bundles); err != nil {
		return stored, err
	}
	kept := slices.DeleteFunc(slices.Clone(stored.bundles), func(area controllerBundleReservation) bool { return area.Mode == retiringArea })
	if err := t.store.checkpoint(ctx, checkpointBeforeToolAreaRelease); err != nil {
		return stored, err
	}
	return t.publishToolAreas(ctx, stored, stored.value, kept, retry)
}

func (t *transaction) publishToolAreas(ctx context.Context, stored controllerStored, value prerequisites.HostState, areas []controllerBundleReservation, retry string) (controllerStored, error) {
	if err := validateControllerState(value, areas); err != nil {
		return stored, err
	}
	data, err := encodeRecord(controllerRecord(value, areas), maxControllerState)
	if err != nil {
		return stored, err
	}
	dir, err := openControllerDirectory(t.root, t.registry)
	if err != nil {
		return stored, err
	}
	defer dir.file.Close()
	if _, err := t.store.replaceControllerRecord(ctx, dir, stored, data, retry); err != nil {
		return stored, err
	}
	return readControllerStored(ctx, t.root, t.registry)
}

// removeRetiringAreas removes each retiring area's directory, proving it is
// still the attributed one, and syncs the parent once they are gone.
func (t *transaction) removeRetiringAreas(ctx context.Context, areas []controllerBundleReservation) error {
	owner, err := openControllerDirectory(t.root, t.registry)
	if err != nil {
		return err
	}
	defer owner.file.Close()
	parent, err := openDirectory(owner, toolAreasName)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.file.Close()
	for _, area := range areas {
		if area.Mode != retiringArea {
			continue
		}
		if err := t.store.checkpoint(ctx, checkpointBeforeToolAreaRemoval); err != nil {
			return err
		}
		stat, err := listedEntry(parent, area.ID)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		if err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFDIR || stat.Ino != area.DirectoryInode || uint64(stat.Dev) != area.DirectoryDevice {
			return state("retiring controller tool area directory was replaced")
		}
		held, err := openDirectory(parent, area.ID)
		if err != nil {
			return err
		}
		defer held.file.Close()
		if held.identity.Ino != stat.Ino || held.identity.Dev != stat.Dev {
			return state("retiring controller tool area directory was replaced before its lease")
		}
		if err := lock(held); err != nil {
			return err
		}
		remaining := maxBundleEntries
		if err := emptySetupTree(ctx, parent, area.ID, stat, parent.identity, 0, &remaining); err != nil {
			return err
		}
		if errno := unlinkAt(parent, area.ID, true); errno != 0 {
			return state("retiring controller tool area could not be removed")
		}
	}
	return t.store.syncDirectory(ctx, parent)
}

// RetireToolAreas ends a successful controller stage: this context's binding
// names current, the area the stage proved, or nothing when it selects no
// tool, and every sealed area no binding names is retired. An area an earlier
// attempt published whole but never sealed is sealed first. Areas this
// operation holds open stay.
func (t *lifecycleTransaction) RetireToolAreas(ctx context.Context, current string) error {
	if err := t.base.available(ctx); err != nil {
		return err
	}
	if t.stored.data == nil || !t.controller.Prepared {
		return controllerFailure("controller.identity", "this host has no completed controller setup", completeSetup)
	}
	value := cloneControllerState(t.stored.value)
	if current != "" {
		index := slices.IndexFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == current })
		if index < 0 || t.stored.bundles[index].Mode == retiringArea || t.stored.bundles[index].DirectoryInode == 0 {
			return state("controller client area is not published")
		}
		if t.stored.bundles[index].Mode != "sealed" && t.openArea(current) == nil {
			if _, err := t.ClientArea(ctx, current); err != nil {
				return err
			}
		}
		if err := t.SealClientArea(ctx, current); err != nil {
			return err
		}
		value = cloneControllerState(t.stored.value)
	} else if bound := slices.IndexFunc(value.Bindings, func(binding prerequisites.ControllerBinding) bool { return binding.Context == t.identity.Name }); bound >= 0 {
		value.Bindings[bound].Tools = ""
	}
	return t.retire(ctx, value)
}

func (t *lifecycleTransaction) retire(ctx context.Context, value prerequisites.HostState) error {
	stored, err := t.base.retireToolAreas(ctx, t.stored, value, func(id string) bool { return t.openArea(id) != nil }, applyRetry(t.identity.Name))
	if !slices.Equal(stored.data, t.stored.data) {
		t.stored = stored
		t.controller.State = cloneControllerState(stored.value)
		t.controller.Areas = heldAreas(stored.bundles)
	}
	return err
}
