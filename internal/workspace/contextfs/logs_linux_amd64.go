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
	"unsafe"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	productionLogs     = "/var/log/bootwright"
	logsSuffix         = "-logs"
	logContexts        = "contexts"
	logHost            = "host"
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
	file, err := walkAbsolute(logs, admit)
	if errors.Is(err, syscall.EEXIST) {
		file, err = walkAbsolute(logs, nil)
	}
	var unreachable *unreachableRoot
	if errors.As(err, &unreachable) {
		return nil, unsafeLogPath(unreachable.component)
	}
	if err != nil {
		return nil, err
	}
	stat, err := statHandle(file)
	uid, gid := s.logOwner()
	if err != nil || !private(stat, syscall.S_IFDIR, uid, gid) {
		file.Close()
		return nil, unsafeLogPath(logs)
	}
	return &directory{file: file, path: logs, identity: stat}, nil
}

func (s *Store) logDirectory(ctx context.Context, parent *directory, name string, create bool) (*directory, error) {
	var child *directory
	var err error
	if create {
		child, err = s.ensureDirectory(ctx, parent, name)
	} else {
		child, err = openDirectory(parent, name)
	}
	if err == nil || errors.Is(err, syscall.ENOENT) || ctx.Err() != nil {
		return child, err
	}
	stat, statErr := listedEntry(parent, name)
	if statErr == nil && (!private(stat, syscall.S_IFDIR, parent.identity.Uid, parent.identity.Gid) || stat.Dev != parent.identity.Dev) {
		return nil, unsafeLogPath(filepath.Join(parent.path, name))
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
		root.file.Close()
		return nil, nil, err
	}
	dir, err := s.logDirectory(ctx, container, name, create)
	if err != nil {
		container.file.Close()
		root.file.Close()
		return nil, nil, err
	}
	return dir, func() { dir.file.Close(); container.file.Close(); root.file.Close() }, nil
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
	return subtree, func() { subtree.file.Close(); release() }, nil
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
		return state("the current log link is not a link: " + filepath.Join(dir.path, currentLink))
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
	if err := syscall.Renameat(int(dir.file.Fd()), stage, int(dir.file.Fd()), currentLink); err != nil {
		unlinkAt(dir, stage, false)
		return err
	}
	return s.syncDirectory(ctx, dir)
}

func symlinkAt(target string, dir *directory, name string) error {
	targetPointer, err := syscall.BytePtrFromString(target)
	if err != nil {
		return err
	}
	namePointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall(syscall.SYS_SYMLINKAT, uintptr(unsafe.Pointer(targetPointer)), dir.file.Fd(), uintptr(unsafe.Pointer(namePointer)))
	runtime.KeepAlive(targetPointer)
	runtime.KeepAlive(namePointer)
	if errno != 0 {
		return errno
	}
	return nil
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
	names, err := directoryNames(operations, maxOperationEntries)
	if err != nil {
		return err
	}
	logs, total := []retainedLog{}, int64(0)
	for _, name := range names {
		if !reconciliation.ValidOperationID(name) {
			continue
		}
		dir, err := openDirectory(operations, name)
		if err != nil {
			continue
		}
		size := logTreeBytes(dir, 0)
		logs = append(logs, retainedLog{name: name, identity: dir.identity, bytes: size})
		dir.file.Close()
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
	names, err := directoryNames(dir, maxOperationEntries)
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
		if child, err := openDirectory(dir, name); err == nil {
			size += logTreeBytes(child, depth+1)
			child.file.Close()
		}
	}
	return size
}

// removeLogTree removes a log directory holding only the owner's private
// directories on its device, files and links, never followed; anything else
// refuses by its path.
func (s *Store) removeLogTree(ctx context.Context, parent *directory, name string, expected syscall.Stat_t, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	location := filepath.Join(parent.path, name)
	dir, err := openDirectory(parent, name)
	if err != nil || !sameIdentity(dir.identity, expected) || depth > maxOperationSegments {
		if dir != nil {
			dir.file.Close()
		}
		return unsafeLogPath(location)
	}
	defer dir.file.Close()
	names, err := directoryNames(dir, maxOperationEntries)
	if err != nil {
		return unsafeLogPath(location)
	}
	for _, child := range names {
		stat, err := listedEntry(dir, child)
		if errors.Is(err, syscall.ENOENT) {
			continue
		}
		owned := err == nil && stat.Uid == dir.identity.Uid && stat.Gid == dir.identity.Gid && stat.Dev == dir.identity.Dev
		switch {
		case owned && stat.Mode&syscall.S_IFMT == syscall.S_IFDIR:
			err = s.removeLogTree(ctx, dir, child, stat, depth+1)
		case owned && (stat.Mode&syscall.S_IFMT == syscall.S_IFREG || stat.Mode&syscall.S_IFMT == syscall.S_IFLNK):
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
	if unlinkVerified(parent, name, dir.identity, true) != nil {
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
	defer root.file.Close()
	container, err := s.logDirectory(ctx, root, logContexts, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer container.file.Close()
	stat, err := listedEntry(container, name)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if stat.Mode&syscall.S_IFMT == syscall.S_IFLNK && stat.Uid == container.identity.Uid {
		if unlinkVerified(container, name, stat, false) != nil {
			return unsafeLogPath(filepath.Join(container.path, name))
		}
	} else if err := s.removeLogTree(ctx, container, name, stat, 0); err != nil {
		return err
	}
	return s.syncDirectory(ctx, container)
}
