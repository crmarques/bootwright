//go:build linux && amd64

package contextfs

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"

	"github.com/crmarques/bootwright/internal/nativefs"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	productionLogs     = "/var/log/bootwright"
	logsSuffix         = "-logs"
	logContexts        = "contexts"
	logController      = "controller"
	operationLogs      = "operations"
	currentLink        = "current"
	currentStagePrefix = ".current-"
	maxOperationLogs   = 64
	retainedLogBytes   = 128 << 20
)

var errUnsafeLogEntry = errors.New("a log root entry is not this store's own")

func (s *Store) logsPath() (string, error) {
	root, err := s.rootPath()
	if err != nil {
		return "", err
	}
	path := s.options.Logs
	switch {
	case path == "" && s.options.Root == "":
		path = productionLogs
	case path == "":
		path = root + logsSuffix
	}
	if !canonicalPath(path) || path == "/" || beneath(root, path) || beneath(path, root) {
		return "", state("the log root must be an absolute canonical path outside the state root")
	}
	return path, nil
}

func (s *Store) logOwner() (uint32, uint32) {
	if s.options.Owner != nil && s.options.Root != "" {
		return s.options.Owner.UID, s.options.Owner.GID
	}
	return 0, 0
}

func unsafeLogPath(path string) error {
	return errors.Join(errUnsafeLogEntry, contexts.StateErrorWithRemediation(
		"the log directory "+path+" is not a private directory this store created: it must be a root:root 0700 directory on the log root's device, reached without a symbolic link, and nothing repairs it",
		"remove "+path+" or replace it with such a directory, then repeat the command"))
}

// openLogRoot reaches the log root through directories only, creating missing
// ones private when create is set; a concurrent creator winning is no refusal.
func (s *Store) openLogRoot(ctx context.Context, create bool) (*directory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logs, err := s.logsPath()
	if err != nil {
		return nil, err
	}
	var admit func(*os.File) error
	if create {
		admit = func(*os.File) error { return nil }
	}
	file, err := kernel.WalkAbsolute(logs, admit)
	if errors.Is(err, syscall.EEXIST) {
		file, err = kernel.WalkAbsolute(logs, nil)
	}
	var unreachable *kernel.UnreachableRoot
	if errors.As(err, &unreachable) {
		return nil, unsafeLogPath(unreachable.Component)
	}
	if err != nil {
		return nil, err
	}
	stat, err := kernel.StatHandle(file)
	uid, gid := s.logOwner()
	if err != nil || !kernel.Private(stat, syscall.S_IFDIR, uid, gid) {
		file.Close()
		return nil, unsafeLogPath(logs)
	}
	return &directory{File: file, Path: logs, Identity: stat}, nil
}

func (s *Store) logDirectory(ctx context.Context, parent *directory, name string, create bool) (*directory, error) {
	var child *directory
	var err error
	if create {
		child, err = s.ensureDirectory(ctx, parent, name)
	} else {
		child, err = kernel.OpenDirectory(parent, name)
	}
	if err == nil || errors.Is(err, syscall.ENOENT) || ctx.Err() != nil {
		return child, err
	}
	stat, statErr := listedEntry(parent, name)
	if statErr == nil && (!kernel.Private(stat, syscall.S_IFDIR, parent.Identity.Uid, parent.Identity.Gid) || stat.Dev != parent.Identity.Dev) {
		return nil, unsafeLogPath(filepath.Join(parent.Path, name))
	}
	return nil, err
}

func (s *Store) contextLogs(ctx context.Context, name string, create bool) (*directory, func(), error) {
	root, err := s.openLogRoot(ctx, create)
	if err != nil {
		return nil, nil, err
	}
	container, err := s.logDirectory(ctx, root, logContexts, create)
	if err != nil {
		root.File.Close()
		return nil, nil, err
	}
	dir, err := s.logDirectory(ctx, container, name, create)
	if err != nil {
		container.File.Close()
		root.File.Close()
		return nil, nil, err
	}
	return dir, func() { dir.File.Close(); container.File.Close(); root.File.Close() }, nil
}

func (a *operationArea) logRoot(ctx context.Context, create bool) (*directory, func(), error) {
	parent, release, err := a.store.contextLogs(ctx, a.name, create)
	if err != nil {
		return nil, nil, err
	}
	subtree, err := a.store.logDirectory(ctx, parent, a.subtree, create)
	if err != nil {
		release()
		return nil, nil, err
	}
	return subtree, func() { subtree.File.Close(); release() }, nil
}

type operationLogArea struct{ *operationArea }

var _ operationstore.LogRetention = (*operationLogArea)(nil)

func (a *operationLogArea) MarkCurrent(ctx context.Context, id string) {
	if !reconciliation.ValidOperationID(id) || a.available(ctx, true) != nil {
		return
	}
	dir, release, err := a.store.contextLogs(ctx, a.name, true)
	if err != nil {
		return
	}
	defer release()
	_ = a.store.linkCurrent(ctx, dir, id)
}

// linkCurrent renames a staged link over current, leaving an entry that is no link.
func (s *Store) linkCurrent(ctx context.Context, dir *directory, id string) error {
	if stat, err := listedEntry(dir, currentLink); err == nil && stat.Mode&syscall.S_IFMT != syscall.S_IFLNK {
		return state("the current log link is not a link: " + filepath.Join(dir.Path, currentLink))
	}
	stage, err := s.candidate(currentStagePrefix)
	if err != nil {
		return err
	}
	if err := s.checkpoint(ctx, checkpointCreateFile); err != nil {
		return err
	}
	if err := symlinkAt(path.Join(operationLogs, id), dir, stage); err != nil {
		return err
	}
	if err := syscall.Renameat(int(dir.File.Fd()), stage, int(dir.File.Fd()), currentLink); err != nil {
		unlinkAt(dir, stage, false)
		return err
	}
	return s.syncDirectory(ctx, dir)
}

func symlinkAt(target string, dir *directory, name string) error {
	err := nativefs.Symlink(target, int(dir.File.Fd()), name)
	runtime.KeepAlive(dir.File)
	return err
}

type retainedLog struct {
	name     string
	identity syscall.Stat_t
	bytes    int64
}

func (a *operationLogArea) Retain(ctx context.Context, keep []string) {
	if a.available(ctx, true) != nil {
		return
	}
	operations, release, err := a.root(ctx, false)
	if err != nil {
		return
	}
	defer release()
	_ = a.store.retainOperationLogs(ctx, operations, keep)
}

// retainOperationLogs removes the oldest log directories but keep, by
// modification time then name, until fewer than maxOperationLogs remain
// within retainedLogBytes; one holding what this store never creates is left.
func (s *Store) retainOperationLogs(ctx context.Context, operations *directory, keep []string) error {
	names, err := kernel.DirectoryNames(operations, maxOperationEntries)
	if err != nil {
		return err
	}
	logs, total := []retainedLog{}, int64(0)
	for _, name := range names {
		if !reconciliation.ValidOperationID(name) {
			continue
		}
		dir, err := kernel.OpenDirectory(operations, name)
		if err != nil {
			continue
		}
		size := logTreeBytes(dir, 0)
		logs = append(logs, retainedLog{name: name, identity: dir.Identity, bytes: size})
		dir.File.Close()
		total += size
	}
	slices.SortFunc(logs, func(x, y retainedLog) int {
		return cmp.Or(cmp.Compare(x.identity.Mtim.Sec, y.identity.Mtim.Sec), cmp.Compare(x.identity.Mtim.Nsec, y.identity.Mtim.Nsec), strings.Compare(x.name, y.name))
	})
	remaining, removed := len(logs), false
	for _, log := range logs {
		if remaining < maxOperationLogs && total <= retainedLogBytes {
			break
		}
		if slices.Contains(keep, log.name) {
			continue
		}
		if s.removeLogTree(ctx, operations, log.name, log.identity, 0) == nil {
			remaining, total, removed = remaining-1, total-log.bytes, true
		}
	}
	if !removed {
		return nil
	}
	return s.syncDirectory(ctx, operations)
}

func logTreeBytes(dir *directory, depth int) int64 {
	names, err := kernel.DirectoryNames(dir, maxOperationEntries)
	if err != nil || depth > maxOperationSegments {
		return 0
	}
	size := int64(0)
	for _, name := range names {
		stat, err := listedEntry(dir, name)
		if err != nil {
			continue
		}
		if stat.Mode&syscall.S_IFMT == syscall.S_IFREG {
			size += stat.Size
			continue
		}
		if child, err := kernel.OpenDirectory(dir, name); err == nil {
			size += logTreeBytes(child, depth+1)
			child.File.Close()
		}
	}
	return size
}

// removeLogTree removes a log directory holding only the owner's private
// directories on its device, private singly linked files and links, never
// followed; anything else refuses by its path.
func (s *Store) removeLogTree(ctx context.Context, parent *directory, name string, expected syscall.Stat_t, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	location := filepath.Join(parent.Path, name)
	dir, err := kernel.OpenDirectory(parent, name)
	if err != nil || !kernel.SameIdentity(dir.Identity, expected) || depth > maxOperationSegments {
		if dir != nil {
			dir.File.Close()
		}
		return unsafeLogPath(location)
	}
	defer dir.File.Close()
	names, err := kernel.DirectoryNames(dir, maxOperationEntries)
	if err != nil {
		return unsafeLogPath(location)
	}
	for _, child := range names {
		stat, err := listedEntry(dir, child)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		owned := err == nil && stat.Uid == dir.Identity.Uid && stat.Gid == dir.Identity.Gid && stat.Dev == dir.Identity.Dev
		switch {
		case owned && stat.Mode&syscall.S_IFMT == syscall.S_IFDIR:
			err = s.removeLogTree(ctx, dir, child, stat, depth+1)
		case owned && (kernel.Private(stat, syscall.S_IFREG, dir.Identity.Uid, dir.Identity.Gid) || stat.Mode&syscall.S_IFMT == syscall.S_IFLNK):
			if unlinkVerified(dir, child, stat, false) != nil {
				err = unsafeLogPath(filepath.Join(location, child))
			}
		default:
			err = unsafeLogPath(filepath.Join(location, child))
		}
		if err != nil {
			return err
		}
	}
	if err := s.syncDirectory(ctx, dir); err != nil {
		return err
	}
	if unlinkVerified(parent, name, dir.Identity, true) != nil {
		return unsafeLogPath(location)
	}
	return nil
}

// removeContextLogs removes a context's log directory, already absent or not.
func (s *Store) removeContextLogs(ctx context.Context, name string) error {
	root, err := s.openLogRoot(ctx, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.File.Close()
	container, err := s.logDirectory(ctx, root, logContexts, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer container.File.Close()
	stat, err := listedEntry(container, name)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if stat.Mode&syscall.S_IFMT == syscall.S_IFLNK && stat.Uid == container.Identity.Uid {
		if unlinkVerified(container, name, stat, false) != nil {
			return unsafeLogPath(filepath.Join(container.Path, name))
		}
	} else if err := s.removeLogTree(ctx, container, name, stat, 0); err != nil {
		return err
	}
	return s.syncDirectory(ctx, container)
}
