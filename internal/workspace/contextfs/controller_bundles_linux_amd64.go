//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	maxControllerBundles = 16
	maxBundleEntries     = 32768
	maxBundleBytes       = 8 << 30
	maxBundleFileBytes   = 1 << 30
	maxBundleDepth       = 32
)

func verifyControllerBundleReservations(ctx context.Context, owner *directory, reservations []controllerBundleReservation) error {
	parent, err := openDirectory(owner, "bundles")
	if errors.Is(err, syscall.ENOENT) {
		for _, reservation := range reservations {
			if reservation.DirectoryInode != 0 {
				return state("attributed controller bundles are missing")
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.file.Close()
	names, err := directoryNames(parent, maxControllerBundles)
	if err != nil {
		return err
	}
	for _, name := range names {
		index := slices.IndexFunc(reservations, func(item controllerBundleReservation) bool { return item.ID == name })
		if index < 0 {
			return state("controller bundle directory is not reserved")
		}
		reservation := reservations[index]
		dir, err := openDirectory(parent, name)
		if err != nil {
			return err
		}
		matches := reservation.DirectoryInode != 0 && reservation.DirectoryInode == dir.identity.Ino && reservation.DirectoryDevice == uint64(dir.identity.Dev)
		dir.file.Close()
		// A reserved entry records intent published before the directory was
		// created, so an unattributed directory is this store's own interrupted
		// attempt. Reading it is safe; only an attributed identity can be
		// contradicted by substitution.
		if !matches && reservation.Mode != "reserved" {
			return state("controller bundle directory is unattributable or replaced")
		}
	}
	for _, reservation := range reservations {
		if reservation.DirectoryInode != 0 && !slices.Contains(names, reservation.ID) {
			return state("attributed controller bundle is missing")
		}
	}
	return ctx.Err()
}

type controllerBundleArea struct {
	store        *Store
	root         *directory
	registry     contexts.Registry
	reservation  controllerBundleReservation
	dir          *directory
	owned        []*directory
	active       func() bool
	writeAllowed bool
	canWrite     func() bool
	sealed       func() bool
	guard        func(context.Context) error
	entries      int
	bytes        int64
	scanned      bool
}

func (a *controllerBundleArea) close() {
	for index := len(a.owned) - 1; index >= 0; index-- {
		a.owned[index].file.Close()
	}
	a.owned = nil
	a.dir = nil
}

func openControllerBundle(ctx context.Context, store *Store, root *directory, registry contexts.Registry, stored controllerStored, id string, active func() bool, writable bool, guard func(context.Context) error) (*controllerBundleArea, error) {
	if !validControllerDigest(id) {
		return nil, state("controller bundle identity is invalid")
	}
	index := slices.IndexFunc(stored.bundles, func(item controllerBundleReservation) bool { return item.ID == id })
	if index < 0 {
		return nil, nil
	}
	if stored.bundles[index].DirectoryInode == 0 {
		return nil, controllerFailure("controller.identity", "required controller bundle is not attributable; run bastion setup")
	}
	area := &controllerBundleArea{store: store, root: root, registry: registry, reservation: stored.bundles[index], active: active, writeAllowed: writable, guard: guard}
	owner, err := openControllerDirectory(root, registry)
	if err != nil {
		return nil, err
	}
	area.owned = append(area.owned, owner)
	parent, err := openDirectory(owner, "bundles")
	if err == nil {
		area.owned = append(area.owned, parent)
	}
	if err != nil {
		area.close()
		return nil, err
	}
	dir, err := openDirectory(parent, id)
	if err == nil {
		area.owned = append(area.owned, dir)
	}
	if err != nil {
		area.close()
		return nil, err
	}
	area.dir = dir
	if err := area.available(ctx, false); err != nil {
		area.close()
		return nil, err
	}
	return area, nil
}

func (t *controllerTransaction) Bundle(ctx context.Context, id string) (prerequisites.BundleArea, error) {
	if err := t.available(ctx); err != nil {
		return nil, err
	}
	if !validControllerDigest(id) || t.stored.data == nil || id != t.stored.value.Receipt.CatalogDigest {
		return nil, state("bundle namespace is not the approved setup catalog")
	}
	writable := t.stored.value.Receipt.Incomplete() && slices.ContainsFunc(t.stored.value.Receipt.Actions, func(action prerequisites.SetupAction) bool { return action.Phase == "intent" })
	index := slices.IndexFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == id })
	if index >= 0 && t.stored.bundles[index].Mode == "sealed" {
		writable = false
	}
	// A reservation published by an earlier attempt is this store's durable
	// proof that it owns the directory that attempt may have created.
	interrupted := index >= 0 && t.stored.bundles[index].Mode == "reserved"
	if index < 0 || t.stored.bundles[index].DirectoryInode == 0 {
		if !writable {
			return nil, state("bundle creation requires a durably intended setup action")
		}
		if index < 0 {
			if len(t.stored.bundles) >= maxControllerBundles {
				return nil, state("retained controller bundle limit exceeded")
			}
			next := slices.Clone(t.stored.bundles)
			next = append(next, controllerBundleReservation{ID: id, Mode: "reserved"})
			slices.SortFunc(next, func(a, b controllerBundleReservation) int { return strings.Compare(a.ID, b.ID) })
			if _, err := t.publishValue(ctx, t.stored.value, next); err != nil {
				return nil, err
			}
		}
		owner, err := openControllerDirectory(t.base.root, t.base.registry)
		if err != nil {
			return nil, err
		}
		defer owner.file.Close()
		parent, err := t.base.store.ensureDirectory(ctx, owner, "bundles")
		if err != nil {
			return nil, err
		}
		defer parent.file.Close()
		var identity syscall.Stat_t
		switch existing, err := openDirectory(parent, id); {
		case err == nil:
			// Only an empty directory carried by this store's own reserved
			// entry may be attributed; a retry never adopts foreign content
			// and never repeats a completed publication.
			entries, listErr := directoryNames(existing, 1)
			identity = existing.identity
			existing.file.Close()
			if listErr != nil {
				return nil, listErr
			}
			if !interrupted || len(entries) != 0 {
				return nil, state("existing controller bundle cannot be adopted")
			}
		case !errors.Is(err, syscall.ENOENT):
			return nil, err
		default:
			dir, err := t.base.store.newDirectory(ctx, parent, id)
			if err != nil {
				return nil, err
			}
			defer dir.file.Close()
			identity = dir.identity
			if err := t.base.store.checkpoint(ctx, "after-controller-bundle-directory"); err != nil {
				return nil, err
			}
		}
		next := slices.Clone(t.stored.bundles)
		index = slices.IndexFunc(next, func(item controllerBundleReservation) bool { return item.ID == id })
		next[index] = controllerBundleReservation{ID: id, Mode: "attributed", DirectoryDevice: uint64(identity.Dev), DirectoryInode: identity.Ino}
		if _, err := t.publishValue(ctx, t.stored.value, next); err != nil {
			return nil, err
		}
	}
	area, err := openControllerBundle(ctx, t.base.store, t.base.root, t.base.registry, t.stored, id, func() bool { return t.active && !t.uncertain }, writable, t.available)
	if err != nil {
		return nil, err
	}
	t.areas = append(t.areas, area)
	area.canWrite = func() bool {
		return t.stored.value.Receipt.Incomplete() && slices.ContainsFunc(t.stored.value.Receipt.Actions, func(action prerequisites.SetupAction) bool { return action.Phase == "intent" }) && !slices.ContainsFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == id && item.Mode == "sealed" })
	}
	area.sealed = func() bool {
		return slices.ContainsFunc(t.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == id && item.Mode == "sealed" })
	}
	return area, nil
}

func (a *controllerBundleArea) available(ctx context.Context, write bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.dir == nil || a.active == nil || !a.active() || write && (!a.writeAllowed || a.canWrite == nil || !a.canWrite()) {
		return state("controller bundle capability is closed or read-only")
	}
	if a.guard != nil {
		if err := a.guard(ctx); err != nil {
			return err
		}
	}
	if err := a.dir.verify(); err != nil {
		return err
	}
	if a.reservation.DirectoryInode != a.dir.identity.Ino || a.reservation.DirectoryDevice != uint64(a.dir.identity.Dev) {
		return state("controller bundle directory was replaced")
	}
	return nil
}

func bundleParts(path string) ([]string, error) {
	if path == "" || len(path) > maxPath || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
		return nil, state("controller bundle path is invalid")
	}
	parts := strings.Split(path, "/")
	if len(parts) > maxBundleDepth {
		return nil, state("controller bundle path exceeds its depth bound")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return nil, state("controller bundle path is invalid")
		}
		for _, c := range part {
			if c < 32 || c >= 127 {
				return nil, state("controller bundle path is not bounded ASCII")
			}
		}
	}
	return parts, nil
}

func (a *controllerBundleArea) parent(parts []string) (*directory, string, func(), error) {
	owned := []*directory{}
	close := func() {
		for index := len(owned) - 1; index >= 0; index-- {
			owned[index].file.Close()
		}
	}
	parent := a.dir
	for _, name := range parts[:len(parts)-1] {
		child, err := openDirectory(parent, name)
		if err != nil {
			close()
			return nil, "", func() {}, err
		}
		owned = append(owned, child)
		parent = child
	}
	return parent, parts[len(parts)-1], close, nil
}

func privateBundleFile(stat syscall.Stat_t, parent *directory) bool {
	if stat.Mode&07777 == 0700 {
		stat.Mode = stat.Mode&^07777 | 0600
	}
	return stat.Dev == parent.identity.Dev && private(stat, syscall.S_IFREG, parent.identity.Uid, parent.identity.Gid)
}

func (a *controllerBundleArea) Read(ctx context.Context, path string, maximum int) ([]byte, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	parts, err := bundleParts(path)
	if err != nil {
		return nil, err
	}
	if maximum < 0 || maximum > maxBundleFileBytes {
		return nil, state("controller bundle read exceeds its byte bound")
	}
	parent, name, close, err := a.parent(parts)
	if err != nil {
		return nil, err
	}
	defer close()
	file, err := openRelative(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, safeError(err)
	}
	defer file.Close()
	before, err := statHandle(file)
	if err != nil || !privateBundleFile(before, parent) || before.Size < 0 || before.Size > int64(maximum) {
		return nil, state("controller bundle file metadata is unsafe")
	}
	data := make([]byte, 0, int(before.Size))
	buffer := make([]byte, min(32768, maximum+1))
	for len(data) <= maximum {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := file.Read(buffer[:min(len(buffer), maximum+1-len(data))])
		data = append(data, buffer[:n]...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil || len(data) > maximum {
			return nil, state("controller bundle read failed or exceeded its bound")
		}
	}
	after, err := statHandle(file)
	if err != nil || !sameFile(before, after) || after.Size != int64(len(data)) {
		return nil, state("controller bundle file changed during inspection")
	}
	current, err := openRelative(parent, name, pathHandle, 0)
	if err != nil {
		return nil, state("controller bundle file was replaced")
	}
	actual, err := statHandle(current)
	current.Close()
	if err != nil || !sameFile(after, actual) {
		return nil, state("controller bundle file was replaced")
	}
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	return data, nil
}

func (a *controllerBundleArea) EnsureDirectory(ctx context.Context, path string) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := bundleParts(path)
	if err != nil {
		return err
	}
	if !a.scanned {
		if err := a.Verify(ctx); err != nil {
			return err
		}
	}
	parent := a.dir
	owned := []*directory{}
	defer func() {
		for index := len(owned) - 1; index >= 0; index-- {
			owned[index].file.Close()
		}
	}()
	for _, name := range parts {
		child, err := openDirectory(parent, name)
		if errors.Is(err, syscall.ENOENT) {
			if a.entries >= maxBundleEntries {
				return state("controller bundle entry limit exceeded")
			}
			a.scanned = false
			child, err = a.store.newDirectory(ctx, parent, name)
			if err == nil {
				a.entries++
				a.scanned = true
			}
		}
		if err != nil {
			return err
		}
		owned = append(owned, child)
		parent = child
	}
	return a.available(ctx, true)
}

func (a *controllerBundleArea) Write(ctx context.Context, path string, data []byte, executable bool) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := bundleParts(path)
	if err != nil {
		return err
	}
	if len(data) > maxBundleFileBytes {
		return state("controller bundle file exceeds its byte bound")
	}
	if !a.scanned {
		if err := a.Verify(ctx); err != nil {
			return err
		}
	}
	if a.entries >= maxBundleEntries || int64(len(data)) > maxBundleBytes-a.bytes {
		return state("controller bundle capacity exceeded")
	}
	parent, name, close, err := a.parent(parts)
	if err != nil {
		return err
	}
	defer close()
	if err := a.store.checkpoint(ctx, "before-controller-bundle-write"); err != nil {
		return err
	}
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if err := parent.verify(); err != nil {
		return err
	}
	mode := uint32(0600)
	if executable {
		mode = 0700
	}
	file, err := openRelative(parent, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, mode)
	if err != nil {
		return safeError(err)
	}
	defer file.Close()
	a.scanned = false
	before, err := statHandle(file)
	if err != nil || !privateBundleFile(before, parent) || before.Mode&07777 != mode {
		return state("created controller bundle file is unsafe")
	}
	size := len(data)
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Write(data[:min(32768, len(data))])
		if err != nil || n == 0 {
			return state("controller bundle file could not be written")
		}
		data = data[n:]
	}
	if err := file.Sync(); err != nil {
		return state("controller bundle file durability is uncertain")
	}
	after, err := statHandle(file)
	if err != nil || !sameIdentity(before, after) || after.Size != int64(size) {
		return state("controller bundle file changed during publication")
	}
	current, err := openRelative(parent, name, pathHandle, 0)
	if err != nil {
		return state("controller bundle file was replaced")
	}
	actual, err := statHandle(current)
	current.Close()
	if err != nil || !sameFile(after, actual) {
		return state("controller bundle file was replaced")
	}
	if err := a.store.syncDirectory(ctx, parent); err != nil {
		return err
	}
	a.entries++
	a.bytes += int64(size)
	a.scanned = true
	return a.available(ctx, true)
}

func (a *controllerBundleArea) Entries(ctx context.Context) ([]prerequisites.BundleEntry, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	entries, total := 0, int64(0)
	result := []prerequisites.BundleEntry{}
	var walk func(*directory, string, int) error
	walk = func(dir *directory, prefix string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > maxBundleDepth {
			return state("controller bundle tree exceeds its depth bound")
		}
		names, err := directoryNames(dir, maxBundleEntries-entries)
		if err != nil {
			return err
		}
		entries += len(names)
		if entries > maxBundleEntries {
			return state("controller bundle tree exceeds its entry bound")
		}
		for _, name := range names {
			if _, err := bundleParts(prefix + name); err != nil {
				return err
			}
			file, err := openRelative(dir, name, pathHandle, 0)
			if err != nil {
				return state("controller bundle entry is unsafe")
			}
			identity, err := statHandle(file)
			file.Close()
			if err != nil {
				return err
			}
			if identity.Mode&syscall.S_IFMT == syscall.S_IFDIR {
				result = append(result, prerequisites.BundleEntry{Path: prefix + name, Directory: true})
				child, err := openDirectory(dir, name)
				if err != nil {
					return err
				}
				if !sameIdentity(identity, child.identity) {
					child.file.Close()
					return state("controller bundle directory changed during inspection")
				}
				err = walk(child, prefix+name+"/", depth+1)
				child.file.Close()
				if err != nil {
					return err
				}
			} else {
				if !privateBundleFile(identity, dir) || identity.Size < 0 || identity.Size > maxBundleFileBytes || identity.Size > maxBundleBytes-total {
					return state("controller bundle file type, ownership, mode or size is unsafe")
				}
				total += identity.Size
				result = append(result, prerequisites.BundleEntry{Path: prefix + name, Executable: identity.Mode&07777 == 0700, Size: identity.Size})
			}
		}
		current, err := directoryNames(dir, maxBundleEntries)
		if err != nil || !slices.Equal(names, current) {
			return state("controller bundle entries changed during inspection")
		}
		return dir.verify()
	}
	if err := walk(a.dir, "", 0); err != nil {
		return nil, err
	}
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	a.entries, a.bytes, a.scanned = entries, total, true
	slices.SortFunc(result, func(a, b prerequisites.BundleEntry) int { return strings.Compare(a.Path, b.Path) })
	return result, nil
}

func (a *controllerBundleArea) Verify(ctx context.Context) error {
	_, err := a.Entries(ctx)
	return err
}

// Native assembly may write files outside Write. Completion establishes every
// bounded payload and directory's durability before the receipt seals the bundle.
func (a *controllerBundleArea) sync(ctx context.Context) error {
	entries, err := a.Entries(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Directory {
			continue
		}
		parts, err := bundleParts(entry.Path)
		if err != nil {
			return err
		}
		parent, name, close, err := a.parent(parts)
		if err != nil {
			return err
		}
		file, err := openRelative(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			close()
			return err
		}
		before, err := statHandle(file)
		if err == nil && (!privateBundleFile(before, parent) || before.Size != entry.Size || (before.Mode&07777 == 0700) != entry.Executable) {
			err = state("controller bundle file changed before durable sealing")
		}
		if err == nil {
			err = a.store.checkpoint(ctx, "before-controller-bundle-sync")
		}
		if err == nil {
			err = file.Sync()
		}
		after, statErr := statHandle(file)
		file.Close()
		if err == nil && (statErr != nil || !sameFile(before, after)) {
			err = state("controller bundle file changed during durable sealing")
		}
		if err == nil {
			current, openErr := openRelative(parent, name, pathHandle, 0)
			if openErr != nil {
				err = openErr
			} else {
				actual, statErr := statHandle(current)
				current.Close()
				if statErr != nil || !sameFile(after, actual) {
					err = state("controller bundle file was replaced during durable sealing")
				}
			}
		}
		close()
		if err != nil {
			return safeError(err)
		}
	}
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if !entry.Directory {
			continue
		}
		parts, err := bundleParts(entry.Path)
		if err != nil {
			return err
		}
		parent, name, close, err := a.parent(parts)
		if err != nil {
			return err
		}
		dir, err := openDirectory(parent, name)
		if err == nil {
			err = a.store.syncDirectory(ctx, dir)
			dir.file.Close()
		}
		close()
		if err != nil {
			return err
		}
	}
	if err := a.store.syncDirectory(ctx, a.dir); err != nil {
		return err
	}
	return a.Verify(ctx)
}

func (a *controllerBundleArea) Location(ctx context.Context) (prerequisites.BundleLocation, error) {
	if err := a.Verify(ctx); err != nil {
		return prerequisites.BundleLocation{}, err
	}
	a.scanned = false // A native child may change the tree before the next call.
	sealed := a.reservation.Mode == "sealed"
	if a.sealed != nil {
		sealed = a.sealed()
	}
	return prerequisites.BundleLocation{Path: a.dir.path, Device: uint64(a.dir.identity.Dev), Inode: a.dir.identity.Ino, Writable: a.writeAllowed && a.canWrite != nil && a.canWrite(), Sealed: sealed}, nil
}
