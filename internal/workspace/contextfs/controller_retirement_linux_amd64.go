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

// RetireResolutions drops superseded retained resolutions of a bundle that
// stays, or of one it holds no area for, in one publication that leaves every
// area as it is. The record keeps no kind per area, so an execution bundle is
// known by the resolutions naming it: the store refuses to drop the last of
// those naming an area it holds, as it refuses the one the receipt carries.
// One naming no area identifies nothing, and a resolution it does not hold is
// already gone.
func (t *controllerTransaction) RetireResolutions(ctx context.Context, digests []string) error {
	if err := t.available(ctx); err != nil {
		return err
	}
	if t.stored.data == nil {
		return state("controller retirement requires an initialized record")
	}
	receipt := t.stored.value.Receipt
	if receipt.ID == "" || receipt.Incomplete() {
		return state("controller retirement requires a settled setup receipt")
	}
	for _, digest := range digests {
		if !validControllerDigest(digest) {
			return state("retired controller resolution identity is invalid")
		}
		if receipt.Definition != nil && digest == receipt.Definition.ResolutionDigest {
			return state("the resolution this receipt carries may not be retired")
		}
	}
	value := cloneControllerState(t.stored.value)
	held := value.RetainedDefinitions
	value.RetainedDefinitions = slices.DeleteFunc(slices.Clone(held),
		func(definition prerequisites.Definition) bool {
			return slices.Contains(digests, definition.ResolutionDigest)
		})
	if len(value.RetainedDefinitions) == len(held) {
		return nil
	}
	for _, definition := range held {
		holds := slices.ContainsFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == definition.CatalogDigest })
		if holds && slices.Contains(digests, definition.ResolutionDigest) && !slices.ContainsFunc(value.RetainedDefinitions,
			func(other prerequisites.Definition) bool { return other.CatalogDigest == definition.CatalogDigest }) {
			return state("a retained controller resolution may not be retired while no other names its bundle")
		}
	}
	_, err := t.publishValue(ctx, value, t.stored.bundles)
	return err
}

// stranded reports a pending receipt an earlier build published although the
// bundle it names could never be reserved: the record holds every area it
// may, and none for that bundle. Its setup can resume only once room is made,
// and the one area it reads is the one no retirement may name, so it admits a
// retirement of areas as a settled receipt does. Its resolutions stay.
func (t *controllerTransaction) stranded() bool {
	receipt := t.stored.value.Receipt
	return receipt.Incomplete() && len(t.stored.bundles) >= maxControllerBundles &&
		!slices.ContainsFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == receipt.CatalogDigest })
}

func retired(retiring []controllerBundleReservation, id string) bool {
	return id != "" && slices.ContainsFunc(retiring,
		func(item controllerBundleReservation) bool { return item.ID == id })
}

// plannedRetirement decides what this store will remove. Which areas are
// superseded is the caller's judgement; the store itself refuses the bundle its
// receipt names and every area it cannot prove is an execution bundle, so no
// caller can remove a client area. The record keeps no kind per area: an
// execution bundle is one a retained resolution names, or one an earlier
// retirement marked retiring when it dropped that resolution, and a client
// area is named by a closure no resolution names. A receipt whose setup
// completed, failed or was canceled admits a retirement; a pending one does
// not, because its setup resumes it exactly, unless it is stranded. An area it
// does not hold is already gone.
//
// A retiring entry keeps the identity its removal is verified against and a
// reserved one records none, so a reserved area is attributed first, as a
// resumed publication adopts it. With no directory nothing is left to remove:
// its reservation is dropped with the intent, and returned so that its
// resolution is retired with it.
func (t *controllerTransaction) plannedRetirement(ids []string) ([]controllerBundleReservation, []controllerBundleReservation, error) {
	if receipt := t.stored.value.Receipt; receipt.ID == "" || receipt.Incomplete() && !t.stranded() {
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
		index := slices.IndexFunc(next, func(item controllerBundleReservation) bool { return item.ID == id })
		if index < 0 || retired(retiring, id) {
			continue
		}
		if next[index].Mode != "retiring" && !slices.ContainsFunc(t.stored.value.RetainedDefinitions,
			func(definition prerequisites.Definition) bool { return definition.CatalogDigest == id }) {
			return nil, nil, state("a client area, or any area no retained resolution names, may not be retired")
		}
		if next[index].Mode == "reserved" {
			identity, err := t.reservedDirectory(id)
			if err != nil {
				return nil, nil, err
			}
			if identity == nil {
				retiring = append(retiring, next[index])
				next = slices.Delete(next, index, index+1)
				continue
			}
			next[index].DirectoryDevice, next[index].DirectoryInode = uint64(identity.Dev), identity.Ino
		}
		next[index].Mode = "retiring"
		retiring = append(retiring, next[index])
	}
	return retiring, next, nil
}

// reservedDirectory reads what an interrupted publication left under a
// reserved area: nothing, or the empty directory it created before recording
// its identity. Content arrives only after attribution, so a directory
// holding any refuses, as adopting it would.
func (t *controllerTransaction) reservedDirectory(id string) (*syscall.Stat_t, error) {
	owner, err := openControllerDirectory(t.base.root, t.base.registry)
	if err != nil {
		return nil, err
	}
	defer owner.file.Close()
	parent, err := openDirectory(owner, "bundles")
	if errors.Is(err, syscall.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer parent.file.Close()
	dir, err := openDirectory(parent, id)
	if errors.Is(err, syscall.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.file.Close()
	entries, err := directoryNames(dir, 1)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, state("a reserved controller bundle directory holds content this store did not publish, so it may not be retired")
	}
	identity := dir.identity
	return &identity, nil
}

// removeBundleArea empties one bundle area and unlinks it. Only a directory
// whose identity the record proves is removed, and every entry is proved to be
// this store's own bundle content before it is removed, so a substituted tree
// refuses rather than being deleted.
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
	if reservation.DirectoryInode == 0 ||
		reservation.DirectoryInode != identity.Ino || reservation.DirectoryDevice != uint64(identity.Dev) {
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
