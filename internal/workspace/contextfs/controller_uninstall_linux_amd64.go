//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

var _ prerequisites.UninstallStore = (*Store)(nil)

const (
	productionStaging   = "/var/lib/bootwright-staging"
	uninstallSuffix     = "-uninstall-"
	stagingSuffix       = "-staging"
	stageLockName       = "lock"
	maxHostParentNames  = 1 << 16
	maxUninstallDepth   = 256
	maxUninstallEntries = 1 << 24
)

type uninstallTransaction struct {
	store   *Store
	parent  *directory
	view    prerequisites.UninstallView
	root    *directory
	staging *os.File
	active  bool
}

func (s *Store) stagingPath(root string) (string, error) {
	path := s.options.Staging
	switch {
	case path == "" && s.options.Root == "":
		path = productionStaging
	case path == "":
		path = root + stagingSuffix
	}
	if !filepath.IsAbs(path) || len(path) > maxPath || strings.ContainsRune(path, 0) || !canonicalPath(filepath.Clean(path)) {
		return "", state("staging directory must be an absolute canonical path")
	}
	path = filepath.Clean(path)
	if filepath.Dir(path) != filepath.Dir(root) || path == root {
		return "", state("staging directory must be the state root's sibling")
	}
	return path, nil
}

func (s *Store) Uninstall(ctx context.Context, callback func(prerequisites.UninstallTransaction) error) error {
	if callback == nil {
		return state("uninstall callback is missing")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rootPath, err := s.rootPath()
	if err != nil {
		return err
	}
	stagingPath, err := s.stagingPath(rootPath)
	if err != nil {
		return err
	}
	uid, gid := uint32(0), uint32(0)
	if s.options.Owner != nil {
		if s.options.Root == "" {
			return state("test ownership requires an isolated explicit root")
		}
		uid, gid = s.options.Owner.UID, s.options.Owner.GID
	}
	if uint32(os.Geteuid()) != uid || uint32(os.Getegid()) != gid {
		return state("context storage requires root privileges")
	}
	view := prerequisites.UninstallView{Root: rootPath, Staging: stagingPath, Leftovers: []string{}, Contexts: []string{}, Media: []string{}}
	parentFile, err := walkAbsolute(filepath.Dir(rootPath), nil)
	if errors.Is(err, syscall.ENOENT) {
		return safeError(callback(&uninstallTransaction{store: s, view: view, active: true}))
	}
	if err != nil {
		return safeError(err)
	}
	defer parentFile.Close()
	if err := qualifiedFileSystem(parentFile); err != nil {
		return err
	}
	identity, err := statHandle(parentFile)
	if err != nil {
		return err
	}
	var held []*os.File
	parent := &directory{file: parentFile, path: filepath.Dir(rootPath), identity: identity}
	view.Leftovers, err = s.leftovers(parent, filepath.Base(rootPath), filepath.Base(stagingPath), uid, gid, &held)
	defer func() {
		for _, file := range held {
			unlockAndClose(file)
		}
	}()
	if err != nil {
		return err
	}
	tx := &uninstallTransaction{store: s, parent: parent, active: true}
	defer func() { tx.active = false }()
	root, err := s.openRoot(ctx, false, nil)
	switch {
	case errors.Is(err, syscall.ENOENT):
	case err != nil:
		return safeError(err)
	default:
		defer root.file.Close()
		if err := lock(root); err != nil {
			return safeError(err)
		}
		defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
		release, err := s.inspectRootForUninstall(ctx, parent, root, &view)
		defer release()
		if err != nil {
			return safeError(err)
		}
		view.RootExists, tx.root = true, root
	}
	staging, err := s.openStagingForUninstall(parent, filepath.Base(stagingPath), uid, &held)
	if err != nil {
		return err
	}
	if staging != nil {
		defer staging.Close()
		view.StagingSeen, tx.staging = true, staging
	}
	tx.view = view
	return safeError(callback(tx))
}

func (s *Store) inspectRootForUninstall(ctx context.Context, parent, root *directory, view *prerequisites.UninstallView) (func(), error) {
	release := func() {}
	if root.identity.Dev != parent.identity.Dev {
		return release, mountedAside(root.path)
	}
	if err := s.sameEntry(parent, filepath.Base(root.path), root.identity); err != nil {
		return release, err
	}
	registry, _, err := readRegistry(ctx, root)
	if err != nil {
		return release, err
	}
	for _, record := range registry.Contexts {
		view.Contexts = append(view.Contexts, record.Name)
	}
	slices.Sort(view.Contexts)
	if view.Media, err = uninstallMedia(ctx, s, root); err != nil {
		return release, err
	}
	dir, err := openDirectory(root, setupSubtree)
	if errors.Is(err, syscall.ENOENT) {
		return release, nil
	}
	if err != nil {
		return release, err
	}
	held, err := lockSetup(ctx, dir, false)
	if err != nil {
		dir.file.Close()
		return release, err
	}
	release = func() {
		held.Close()
		dir.file.Close()
	}
	view.Setup, err = readSetupView(ctx, dir)
	return release, err
}

func uninstallMedia(ctx context.Context, s *Store, root *directory) ([]string, error) {
	dir, err := openDirectory(root, mediaContainer)
	if errors.Is(err, syscall.ENOENT) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.file.Close()
	area := &mediaArea{store: s, root: root, dir: dir, active: func() bool { return true }}
	names, err := area.Names(ctx)
	if err != nil {
		return nil, err
	}
	retained, err := area.Retained(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range retained {
		if !slices.Contains(names, entry.Name) {
			names = append(names, entry.Name)
		}
	}
	entries, err := directoryNames(dir, maxMediaDirectory)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !stagedMediaName(entry) {
			continue
		}
		held, _, err := area.stageHeld(entry)
		if err != nil {
			return nil, err
		}
		if held {
			return nil, busy("a media add is acquiring installer media")
		}
	}
	slices.Sort(names)
	return names, nil
}

func (s *Store) openStagingForUninstall(parent *directory, name string, uid uint32, held *[]*os.File) (*os.File, error) {
	fd, err := syscall.Openat(int(parent.file.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, nil
	}
	path := filepath.Join(parent.path, name)
	foreign := diagnostics.NewFailureWithRemediation("controller.uninstall",
		path+" is not a directory Bootwright owns, so uninstall leaves it", "",
		"inspect "+path+" and remove it yourself if nothing uses it, then run bootwright controller uninstall again")
	if err != nil {
		return nil, foreign
	}
	file := os.NewFile(uintptr(fd), path)
	stat, err := statHandle(file)
	if err != nil || stat.Uid != uid || stat.Mode&0022 != 0 {
		file.Close()
		return nil, foreign
	}
	if stat.Dev != parent.identity.Dev {
		file.Close()
		return nil, mountedAside(path)
	}
	if err := s.sameEntry(parent, name, stat); err != nil {
		file.Close()
		return nil, err
	}
	if err := holdTree(file, path); err != nil {
		file.Close()
		return nil, err
	}
	dir := &directory{file: file, path: path, identity: stat}
	names, err := rawNames(dir, maxHostParentNames)
	if err != nil {
		file.Close()
		return nil, err
	}
	for _, entry := range names {
		lock, err := openWithin(dir, entry+"/"+stageLockName, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		listed, err := statHandle(lock)
		if err != nil || listed.Mode&syscall.S_IFMT != syscall.S_IFREG {
			lock.Close()
			continue
		}
		switch err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); {
		case errors.Is(err, syscall.EWOULDBLOCK):
			lock.Close()
			file.Close()
			return nil, busy("a staging extraction in " + path + " is held by another process")
		case err != nil:
			lock.Close()
			file.Close()
			return nil, state("a staging extraction lock cannot be inspected")
		}
		*held = append(*held, lock)
	}
	return file, nil
}

func holdTree(file *os.File, path string) error {
	switch err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); {
	case errors.Is(err, syscall.EWOULDBLOCK):
		return busy("another Bootwright command holds " + path)
	case err != nil:
		return state("a Bootwright tree cannot be locked: " + path)
	}
	return nil
}

func mountedAside(path string) error {
	return diagnostics.NewFailureWithRemediation("controller.uninstall",
		path+" is a mount point, which uninstall cannot rename aside", "",
		"unmount "+path+" and remove its mount entry, keeping the directory beneath it, then run bootwright controller uninstall again")
}

func (s *Store) leftovers(parent *directory, root, staging string, uid, gid uint32, held *[]*os.File) ([]string, error) {
	names, err := hostNames(parent)
	if err != nil {
		return nil, err
	}
	found := []string{}
	for _, name := range names {
		if !identifier(name, "."+root+uninstallSuffix) && !identifier(name, "."+staging+uninstallSuffix) {
			continue
		}
		stat, err := s.entryOf(parent, name)
		if err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFDIR || stat.Uid != uid || stat.Gid != gid || stat.Dev != parent.identity.Dev {
			continue
		}
		tree, err := openWithin(parent, name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
		if err != nil {
			continue
		}
		if opened, err := statHandle(tree); err != nil || !sameIdentity(opened, stat) {
			tree.Close()
			continue
		}
		path := filepath.Join(parent.path, name)
		if err := holdTree(tree, path); err != nil {
			tree.Close()
			return nil, err
		}
		*held = append(*held, tree)
		found = append(found, path)
	}
	slices.Sort(found)
	return found, nil
}

func hostNames(parent *directory) ([]string, error) {
	fd, err := syscall.Openat(int(parent.file.Fd()), ".", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, state("the state root's parent cannot be enumerated")
	}
	listing := os.NewFile(uintptr(fd), parent.path)
	defer listing.Close()
	var names []string
	for {
		part, err := listing.Readdirnames(1024)
		names = append(names, part...)
		if len(names) > maxHostParentNames {
			return nil, state("the state root's parent holds more entries than uninstall reads")
		}
		if errors.Is(err, io.EOF) || err == nil && len(part) == 0 {
			return names, nil
		}
		if err != nil {
			return nil, state("the state root's parent cannot be enumerated")
		}
	}
}

func (s *Store) entryOf(parent *directory, name string) (syscall.Stat_t, error) {
	if s.uninstallEntry != nil {
		return s.uninstallEntry(parent.path, name)
	}
	return listedEntry(parent, name)
}

func (s *Store) sameEntry(parent *directory, name string, identity syscall.Stat_t) error {
	stat, err := s.entryOf(parent, name)
	if errors.Is(err, syscall.EXDEV) {
		return mountedAside(filepath.Join(parent.path, name))
	}
	if err != nil || !sameIdentity(stat, identity) {
		return state("the state root was replaced while uninstall inspected it: " + filepath.Join(parent.path, name))
	}
	return nil
}

func (t *uninstallTransaction) View() prerequisites.UninstallView {
	view := t.view
	view.Leftovers = slices.Clone(view.Leftovers)
	view.Contexts = slices.Clone(view.Contexts)
	view.Media = slices.Clone(view.Media)
	return view
}

func (t *uninstallTransaction) RemoveState(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !t.active {
		return nil, state("uninstall capability is no longer available")
	}
	removed := []string{}
	if t.parent == nil {
		return removed, nil
	}
	for _, path := range t.view.Leftovers {
		if err := removeTree(ctx, t.parent, filepath.Base(path)); err != nil {
			return removed, err
		}
		removed = append(removed, path)
	}
	type aside struct{ name, path string }
	var renamed []aside
	if t.root != nil {
		name, err := t.renameAside(filepath.Base(t.view.Root), t.root.identity)
		if err != nil {
			return removed, err
		}
		renamed = append(renamed, aside{name, t.view.Root})
	}
	if t.staging != nil {
		identity, err := statHandle(t.staging)
		if err != nil {
			return removed, err
		}
		name, err := t.renameAside(filepath.Base(t.view.Staging), identity)
		if err != nil {
			return removed, err
		}
		renamed = append(renamed, aside{name, t.view.Staging})
	}
	if len(renamed) != 0 {
		if err := t.parent.file.Sync(); err != nil {
			return removed, storeFailure("the rename of Bootwright's state could not be made durable", err)
		}
	}
	for _, tree := range renamed {
		if err := removeTree(ctx, t.parent, tree.name); err != nil {
			return removed, err
		}
		removed = append(removed, tree.path)
	}
	return removed, nil
}

func (t *uninstallTransaction) renameAside(name string, identity syscall.Stat_t) (string, error) {
	if err := t.store.sameEntry(t.parent, name, identity); err != nil {
		return "", err
	}
	target, err := t.store.candidate("." + name + uninstallSuffix)
	if err != nil {
		return "", err
	}
	if errno := renameNoReplaceIn(t.parent, name, target); errno != 0 {
		return "", storeFailure("Bootwright's state could not be renamed aside: "+filepath.Join(t.parent.path, name), errno)
	}
	if err := t.store.sameEntry(t.parent, target, identity); err != nil {
		return "", err
	}
	return target, nil
}

func renameNoReplaceIn(parent *directory, oldName, newName string) syscall.Errno {
	oldPointer, err := syscall.BytePtrFromString(oldName)
	if err != nil {
		return syscall.EINVAL
	}
	newPointer, err := syscall.BytePtrFromString(newName)
	if err != nil {
		return syscall.EINVAL
	}
	_, _, errno := syscall.Syscall6(renameat2Trap, parent.file.Fd(), uintptr(unsafe.Pointer(oldPointer)),
		parent.file.Fd(), uintptr(unsafe.Pointer(newPointer)), renameNoReplace, 0)
	runtime.KeepAlive(oldPointer)
	runtime.KeepAlive(newPointer)
	return errno
}

func removeTree(ctx context.Context, parent *directory, name string) error {
	stat, err := listedEntry(parent, name)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil
		}
		return state("a renamed Bootwright tree cannot be inspected: " + filepath.Join(parent.path, name))
	}
	remaining := maxUninstallEntries
	if err := emptyRenamedTree(ctx, parent, name, stat, 0, &remaining); err != nil {
		return err
	}
	if errno := unlinkAt(parent, name, true); errno != 0 && errno != syscall.ENOENT {
		return storeFailure("a renamed Bootwright tree could not be removed: "+filepath.Join(parent.path, name), errno)
	}
	return nil
}

func emptyRenamedTree(ctx context.Context, parent *directory, name string, identity syscall.Stat_t, depth int, remaining *int) error {
	refused := func() error {
		return state("a renamed Bootwright tree holds an entry uninstall cannot remove safely: " + filepath.Join(parent.path, name))
	}
	if depth > maxUninstallDepth || identity.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return refused()
	}
	file, err := openWithin(parent, name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return refused()
	}
	defer file.Close()
	current, err := statHandle(file)
	if err != nil || !sameIdentity(identity, current) {
		return refused()
	}
	dir := &directory{file: file, path: filepath.Join(parent.path, name), identity: current}
	names, err := rawNames(dir, *remaining)
	if err != nil {
		return refused()
	}
	*remaining -= len(names)
	for _, entry := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		stat, err := listedEntry(dir, entry)
		if err != nil || stat.Dev != current.Dev {
			return refused()
		}
		isDirectory := stat.Mode&syscall.S_IFMT == syscall.S_IFDIR
		if isDirectory {
			if err := emptyRenamedTree(ctx, dir, entry, stat, depth+1, remaining); err != nil {
				return err
			}
		}
		if errno := unlinkAt(dir, entry, isDirectory); errno != 0 && errno != syscall.ENOENT {
			return refused()
		}
	}
	return nil
}
