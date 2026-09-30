//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

type contextTreeAction uint8

const (
	inspectContextTree contextTreeAction = iota
	removeContextTree
	syncContextTree
	maxContextEntries = maxSecretEntries + maxOperationEntries + maxRevisions*(desiredstate.MaxFiles+desiredstate.MaxMarkers+3) + 16
)

// maxContextStateEntries bounds what one context's state directory may hold.
// It must admit every contextStateEntry below: a layout verification reading
// fewer would refuse every mutation once the last of them exists.
const maxContextStateEntries = 5

// contextStateEntry names everything one context's runtime state may hold: its
// reservation and mutation records, the operations a registered lifecycle
// operation owns, the runs a bounded operation retains its output in, and the
// SSH host keys this context trusts.
func contextStateEntry(name string) bool {
	return name == "reservation.json" || name == "mutation.json" ||
		name == "operations" || name == "runs" || name == trustSubtree
}

func contextStateDirectory(name string) bool {
	return name == "operations" || name == "runs" || name == trustSubtree
}

// Context traversal accepts only the owned layout. It never follows a link or
// interprets a path supplied by desired state or secret material.
func contextEntryAllowed(path, name string, directory bool) bool {
	switch path {
	case "":
		return directory && (name == "state" || name == "desired-state" || name == "secrets") || !directory && name == "context.yaml"
	case "state":
		if !contextStateEntry(name) {
			return false
		}
		return directory == contextStateDirectory(name)
	case "desired-state":
		return directory && name == "revisions"
	case "desired-state/revisions":
		return directory && identifier(name, "rev-")
	case "secrets":
		_, err := secretPath(name, 1, 1)
		return err == nil
	}
	if strings.HasPrefix(path, "desired-state/revisions/") && strings.Count(path, "/") == 2 {
		if directory {
			return false
		}
		if name == "manifest.json" {
			return true
		}
		if len(name) != 9 || !strings.HasPrefix(name, "file-") {
			return false
		}
		for _, c := range name[5:] {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(path, "secrets/") && strings.Count(path, "/") == 1 {
		_, err := secretPath(name, 1, 1)
		return !directory && err == nil
	}
	// The operations, runs and trust areas own their own naming and bounds, so
	// deletion admits exactly what those areas were allowed to create. Without
	// this a context that ever ran one operation, kept one bounded run or
	// trusted one host key can never be deleted.
	for _, area := range []string{"state/operations", "state/runs", "state/" + trustSubtree} {
		if path == area || strings.HasPrefix(path, area+"/") {
			return safeOperationName(name)
		}
	}
	return false
}

func deletionOrder(path string, names []string) {
	slices.SortFunc(names, func(a, b string) int {
		if a == b {
			return 0
		}
		last := ""
		if path == "" {
			last = "context.yaml"
		}
		if path == "state" {
			last = "reservation.json"
		}
		if a == last {
			return 1
		}
		if b == last {
			return -1
		}
		if path == "" {
			if a == "state" {
				return 1
			}
			if b == "state" {
				return -1
			}
		}
		return strings.Compare(a, b)
	})
}

func contextDirectoryLimit(path string) int {
	switch path {
	case "":
		return 4
	case "state":
		// Exactly what verifyContextLayout admits. A released reservation is an
		// empty record, not a removed file, so a destroyed context always has
		// the first three and a lower bound here would refuse to delete it.
		return maxContextStateEntries
	case "desired-state":
		return 1
	case "desired-state/revisions":
		return maxRevisions
	}
	if path == "secrets" || strings.HasPrefix(path, "secrets/") {
		return maxSecretEntries
	}
	for _, area := range []string{"state/operations", "state/runs", "state/" + trustSubtree} {
		if path == area || strings.HasPrefix(path, area+"/") {
			return maxOperationEntries
		}
	}
	return desiredstate.MaxFiles + desiredstate.MaxMarkers + 1
}

func (s *Store) walkContextTree(ctx context.Context, dir *directory, path string, action contextTreeAction, remaining *int) error {
	return s.walkContextTreeWithRemovalGuard(ctx, dir, path, action, remaining, nil)
}

func (s *Store) walkContextTreeWithRemovalGuard(ctx context.Context, dir *directory, path string, action contextTreeAction, remaining *int, beforeRemove func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	names, err := directoryNames(dir, min(*remaining, contextDirectoryLimit(path)))
	if err != nil {
		return err
	}
	*remaining -= len(names)
	if *remaining < 0 {
		return state("context traversal exceeds its bound")
	}
	deletionOrder(path, names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := openRelative(dir, name, pathHandle, 0)
		if err != nil {
			return state("context traversal entry cannot be verified")
		}
		identity, err := statHandle(file)
		file.Close()
		kind := identity.Mode & syscall.S_IFMT
		if err != nil || identity.Dev != dir.identity.Dev || !private(identity, kind, dir.identity.Uid, dir.identity.Gid) || (kind != syscall.S_IFDIR && kind != syscall.S_IFREG) || !contextEntryAllowed(path, name, kind == syscall.S_IFDIR) {
			return state("context traversal contains unknown or unsafe state")
		}
		if kind == syscall.S_IFDIR {
			if err := s.checkpoint(ctx, checkpointBeforeContextSubtree); err != nil {
				return err
			}
			child, err := openDirectory(dir, name)
			if err != nil {
				return err
			}
			if !sameIdentity(identity, child.identity) {
				child.file.Close()
				return state("context traversal directory was substituted")
			}
			next := name
			if path != "" {
				next = path + "/" + name
			}
			err = s.walkContextTreeWithRemovalGuard(ctx, child, next, action, remaining, beforeRemove)
			child.file.Close()
			if err != nil {
				return err
			}
		} else if action == syncContextTree {
			if err := s.syncVerifiedFile(ctx, dir, name, identity); err != nil {
				return err
			}
		}
		if action == removeContextTree {
			if err := s.checkpoint(ctx, checkpointBeforeContextUnlink); err != nil {
				return err
			}
			if beforeRemove != nil {
				if err := beforeRemove(ctx); err != nil {
					return err
				}
			}
			if err := unlinkVerified(dir, name, identity, kind == syscall.S_IFDIR); err != nil {
				return err
			}
			if err := s.syncDirectory(ctx, dir); err != nil {
				return err
			}
		}
	}
	if action == syncContextTree {
		return s.syncDirectory(ctx, dir)
	}
	return dir.verify()
}

func (s *Store) syncVerifiedFile(ctx context.Context, parent *directory, name string, expected syscall.Stat_t) error {
	file, err := openRelative(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return state("context durability target cannot be verified")
	}
	defer file.Close()
	before, err := statHandle(file)
	if err != nil || !sameFile(expected, before) {
		return state("context durability target was substituted")
	}
	if err := s.checkpoint(ctx, checkpointSyncContextFile); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return state("context file durability could not be established")
	}
	after, err := statHandle(file)
	if err != nil || !sameFile(before, after) {
		return state("context durability target changed")
	}
	current, err := openRelative(parent, name, pathHandle, 0)
	if err != nil {
		return state("context durability target was replaced")
	}
	defer current.Close()
	actual, err := statHandle(current)
	if err != nil || !sameFile(after, actual) {
		return state("context durability target was replaced")
	}
	return parent.verify()
}

func unlinkVerified(parent *directory, name string, expected syscall.Stat_t, directory bool) error {
	if err := parent.verify(); err != nil {
		return err
	}
	file, err := openRelative(parent, name, pathHandle, 0)
	if err != nil {
		return state("context deletion target cannot be verified")
	}
	actual, err := statHandle(file)
	file.Close()
	if err != nil || !sameIdentity(expected, actual) || !directory && !sameFile(expected, actual) {
		return state("context deletion target was substituted")
	}
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	flags := uintptr(0)
	if directory {
		flags = 0x200
	}
	_, _, errno := syscall.Syscall(syscall.SYS_UNLINKAT, parent.file.Fd(), uintptr(unsafe.Pointer(pointer)), flags)
	runtime.KeepAlive(pointer)
	if errno != 0 {
		return state("context deletion target could not be removed")
	}
	return nil
}

func (t *transaction) Delete(ctx context.Context, requested contexts.Record) error {
	if err := t.available(ctx); err != nil {
		return err
	}
	if err := t.checkControllerRecovery(ctx, requested.Name); err != nil {
		return err
	}
	record, err := t.record(requested.Name)
	if err != nil {
		return err
	}
	if record != requested || record.Mode != contexts.Ready && record.Mode != contexts.Deleting && record.Mode != contexts.Initializing {
		return state("context deletion identity changed")
	}
	if record.Mode == contexts.Deleting {
		if err := t.syncIntent(ctx); err != nil {
			return err
		}
	}
	dir, err := t.contextDirectory(ctx, record.Name)
	if errors.Is(err, syscall.ENOENT) && (record.Mode == contexts.Deleting || record.Mode == contexts.Initializing && record.DirectoryInode == 0) {
		if t.container == nil {
			if record.Mode != contexts.Initializing || record.DirectoryInode != 0 {
				return state("deleting context parent is missing")
			}
			if err := t.store.syncDirectory(ctx, t.root); err != nil {
				return err
			}
		} else if err := t.store.syncDirectory(ctx, t.container); err != nil {
			return err
		}
	} else {
		if err != nil {
			return err
		}
		defer dir.file.Close()
		if record.Mode == contexts.Ready || record.Mode == contexts.Initializing {
			if record.DirectoryInode == 0 {
				return state("initializing directory identity is not attributable")
			}
			if record.Mode == contexts.Ready {
				if _, held := t.leases[record.Name]; !held {
					return state("context deletion requires its mutation lease")
				}
				current, err := readMutation(ctx, dir)
				if err != nil || !bytes.Equal(current, t.evidence[record.Name]) {
					return state("context deletion evidence changed after the disposal check")
				}
			}
			remaining := maxContextEntries
			if err := t.store.walkContextTree(ctx, dir, "", inspectContextTree, &remaining); err != nil {
				return err
			}
			record.Mode = contexts.Deleting
			registry := cloneRegistry(t.registry)
			for i := range registry.Contexts {
				if registry.Contexts[i].Name == record.Name {
					registry.Contexts[i] = record
				}
			}
			if err := t.save(ctx, registry); err != nil {
				return err
			}
		}
		if err := t.dropContextClaims(ctx, record.Name); err != nil {
			return err
		}
		remaining := maxContextEntries
		if err := t.store.walkContextTreeWithRemovalGuard(ctx, dir, "", removeContextTree, &remaining, func(ctx context.Context) error { return t.checkControllerRecovery(ctx, record.Name) }); err != nil {
			return err
		}
		if err := t.store.checkpoint(ctx, checkpointBeforeContextRmdir); err != nil {
			return err
		}
		if err := t.checkControllerRecovery(ctx, record.Name); err != nil {
			return err
		}
		if err := unlinkVerified(t.container, record.Name, dir.identity, true); err != nil {
			return err
		}
		if err := t.store.syncDirectory(ctx, t.container); err != nil {
			return err
		}
	}
	registry := cloneRegistry(t.registry)
	if err := t.dropContextClaims(ctx, record.Name); err != nil {
		return err
	}
	registry.Contexts = slices.DeleteFunc(registry.Contexts, func(item contexts.Record) bool { return item.Name == record.Name })
	if err := t.save(ctx, registry); err != nil {
		return err
	}
	t.committed = true
	return nil
}
