//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"slices"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// RetireBundles removes the execution bundles this host no longer needs. It
// records the intent before it removes anything, so an interruption leaves an
// area marked retiring rather than one the record still presents as usable,
// and repeating the command completes it.
func (t *controllerTransaction) RetireBundles(ctx context.Context, ids []string) error {
	if err := t.available(ctx); err != nil {
		return err
	}
	if t.stored.data == nil {
		return state("controller retirement requires an initialized record")
	}
	retiring, next, err := t.plannedRetirement(ids)
	if err != nil || len(retiring) == 0 {
		return err
	}
	// The resolution a retired bundle carries is retired with it: one whose
	// sources are gone can be carried forward from nothing.
	value := t.stored.value
	value.RetainedDefinitions = slices.DeleteFunc(slices.Clone(value.RetainedDefinitions),
		func(definition prerequisites.Definition) bool { return retired(retiring, definition.CatalogDigest) })
	if _, err := t.publishValue(ctx, value, next); err != nil {
		return err
	}
	if err := t.base.store.checkpoint(ctx, checkpointAfterControllerBundleRetiring); err != nil {
		return err
	}
	for _, reservation := range retiring {
		if err := t.removeBundleArea(ctx, reservation); err != nil {
			return err
		}
	}
	final := slices.DeleteFunc(slices.Clone(next),
		func(item controllerBundleReservation) bool { return retired(retiring, item.ID) })
	_, err = t.publishValue(ctx, value, final)
	return err
}

func retired(retiring []controllerBundleReservation, id string) bool {
	return id != "" && slices.ContainsFunc(retiring,
		func(item controllerBundleReservation) bool { return item.ID == id })
}

// plannedRetirement decides what this store will remove. Which areas are
// superseded execution bundles is the caller's judgement, read from the
// resolutions it retains; what the store refuses on its own is the bundle its
// receipt names and every client closure, because neither is ever superseded
// by a setup. A receipt whose setup completed, failed or was canceled admits a
// retirement; a pending one does not, because its setup resumes it exactly. An
// area it does not hold is already gone.
func (t *controllerTransaction) plannedRetirement(ids []string) ([]controllerBundleReservation, []controllerBundleReservation, error) {
	if receipt := t.stored.value.Receipt; receipt.ID == "" || receipt.Incomplete() {
		return nil, nil, state("controller retirement requires a settled setup receipt")
	}
	current := t.stored.value.Receipt.CatalogDigest
	next := slices.Clone(t.stored.bundles)
	var retiring []controllerBundleReservation
	for _, id := range ids {
		if !validControllerDigest(id) {
			return nil, nil, state("retired controller bundle identity is invalid")
		}
		if id == current {
			return nil, nil, state("the execution bundle this receipt names may not be retired")
		}
		// A client closure is shared host state no context uninstalls, so the
		// store refuses one whatever it is asked to retire.
		if slices.ContainsFunc(t.stored.value.RetainedDefinitions, func(definition prerequisites.Definition) bool {
			return len(definition.Tools) != 0 && prerequisites.ToolsDigest(definition.Tools) == id
		}) {
			return nil, nil, state("a client closure may not be retired")
		}
		index := slices.IndexFunc(next, func(item controllerBundleReservation) bool { return item.ID == id })
		if index < 0 || retired(retiring, id) {
			continue
		}
		next[index].Mode = "retiring"
		retiring = append(retiring, next[index])
	}
	return retiring, next, nil
}

// removeBundleArea empties one bundle area and unlinks it. Every entry is
// proved to be this store's own bundle content before it is removed, so a
// substituted tree refuses rather than being deleted.
func (t *controllerTransaction) removeBundleArea(ctx context.Context, reservation controllerBundleReservation) error {
	owner, err := openControllerDirectory(t.base.root, t.base.registry)
	if err != nil {
		return err
	}
	defer owner.file.Close()
	parent, err := openDirectory(owner, "bundles")
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.file.Close()
	dir, err := openDirectory(parent, reservation.ID)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	identity := dir.identity
	if reservation.DirectoryInode != 0 &&
		(reservation.DirectoryInode != identity.Ino || reservation.DirectoryDevice != uint64(identity.Dev)) {
		dir.file.Close()
		return state("retired controller bundle directory is unattributable or replaced")
	}
	remaining := maxBundleEntries
	err = t.base.store.removeBundleTree(ctx, dir, 0, &remaining)
	dir.file.Close()
	if err != nil {
		return err
	}
	if err := unlinkVerified(parent, reservation.ID, identity, true); err != nil {
		return state("retired controller bundle directory could not be removed")
	}
	return t.base.store.syncDirectory(ctx, parent)
}

// removeBundleTree empties one bundle directory depth first, under the same
// bounds and the same entry rules its inspection reads it with.
func (s *Store) removeBundleTree(ctx context.Context, dir *directory, depth int, remaining *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxBundleDepth {
		return state("retired controller bundle tree exceeds its depth bound")
	}
	names, err := directoryNames(dir, *remaining)
	if err != nil {
		return err
	}
	*remaining -= len(names)
	if *remaining < 0 {
		return state("retired controller bundle tree exceeds its entry bound")
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := openRelative(dir, name, pathHandle, 0)
		if err != nil {
			return state("retired controller bundle entry cannot be verified")
		}
		identity, err := statHandle(file)
		file.Close()
		if err != nil {
			return err
		}
		kind := identity.Mode & syscall.S_IFMT
		if kind != syscall.S_IFDIR && kind != syscall.S_IFREG || identity.Dev != dir.identity.Dev {
			return state("retired controller bundle contains unknown or unsafe content")
		}
		if kind == syscall.S_IFDIR {
			child, err := openDirectory(dir, name)
			if err != nil {
				return err
			}
			if !sameIdentity(identity, child.identity) {
				child.file.Close()
				return state("retired controller bundle directory was substituted")
			}
			err = s.removeBundleTree(ctx, child, depth+1, remaining)
			child.file.Close()
			if err != nil {
				return err
			}
		} else if !privateBundleFile(identity, dir) {
			return state("retired controller bundle file type, ownership or mode is unsafe")
		}
		if err := s.checkpoint(ctx, checkpointBeforeControllerBundleUnlink); err != nil {
			return err
		}
		if err := unlinkVerified(dir, name, identity, kind == syscall.S_IFDIR); err != nil {
			return state("retired controller bundle entry could not be removed")
		}
	}
	return s.syncDirectory(ctx, dir)
}
