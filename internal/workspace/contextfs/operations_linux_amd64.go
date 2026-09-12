//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const (
	maxOperationSegments = 6
	maxOperationEntries  = 8192
	maxOperationBytes    = 64 << 20
	maxOperationRecord   = 1 << 20
	maxOperationLog      = 8 << 20
)

// safeOperationName admits only names this area itself can create. Every path
// component is checked before it is used to open anything, so a stored record
// can never widen the tree it lives in.
func safeOperationName(name string) bool {
	if len(name) == 0 || len(name) > 128 || name == "." || name == ".." {
		return false
	}
	if name[0] == '.' || name[0] == '-' || name[len(name)-1] == '.' {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_') {
			return false
		}
	}
	return strings.Count(name, ".") <= 1
}

func operationPath(target string, minimum int) ([]string, error) {
	if target == "" {
		if minimum > 0 {
			return nil, state("lifecycle operation path is empty")
		}
		return nil, nil
	}
	parts := strings.Split(target, "/")
	if len(parts) < minimum || len(parts) > maxOperationSegments {
		return nil, state("lifecycle operation path exceeds its depth bounds")
	}
	for _, part := range parts {
		if !safeOperationName(part) {
			return nil, state("lifecycle operation path component is unsafe")
		}
	}
	return parts, nil
}

// operationArea confines every effect to one context's operations subtree. It
// is valid only while its owning callback holds the root lock and the context
// lease, and it never interprets the records it publishes.
type operationArea struct {
	store    *Store
	context  *directory
	active   func() bool
	readOnly bool
}

func (a *operationArea) available(ctx context.Context, mutation bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.active == nil || !a.active() {
		return state("the lifecycle operation capability has closed")
	}
	if mutation && a.readOnly {
		return state("read-only lifecycle access refuses mutation")
	}
	return a.context.verify()
}

// root opens the operations directory, creating it only for a mutation. A read
// never repairs or initializes state.
func (a *operationArea) root(ctx context.Context, create bool) (*directory, func(), error) {
	runtime, err := openDirectory(a.context, "state")
	if err != nil {
		return nil, nil, state("context state directory is unsafe")
	}
	operations, err := openDirectory(runtime, "operations")
	if errors.Is(err, syscall.ENOENT) && create {
		operations, err = a.store.newDirectory(ctx, runtime, "operations")
	}
	if err != nil {
		runtime.file.Close()
		if errors.Is(err, syscall.ENOENT) {
			return nil, nil, err
		}
		return nil, nil, state("lifecycle operations directory is unsafe")
	}
	return operations, func() { operations.file.Close(); runtime.file.Close() }, nil
}

// descend walks the validated components, returning the parent of the final
// component so every open uses a held, verified handle.
func (a *operationArea) descend(ctx context.Context, parts []string, create bool) (*directory, string, func(), error) {
	operations, closeRoot, err := a.root(ctx, create)
	if err != nil {
		return nil, "", nil, err
	}
	closers := []func(){closeRoot}
	release := func() {
		for index := len(closers) - 1; index >= 0; index-- {
			closers[index]()
		}
	}
	parent := operations
	for _, part := range parts[:max(len(parts)-1, 0)] {
		child, err := openDirectory(parent, part)
		if errors.Is(err, syscall.ENOENT) && create {
			child, err = a.store.newDirectory(ctx, parent, part)
		}
		if err != nil {
			release()
			if errors.Is(err, syscall.ENOENT) {
				return nil, "", nil, err
			}
			return nil, "", nil, state("lifecycle operation directory is unsafe")
		}
		closers = append(closers, func() { child.file.Close() })
		parent = child
	}
	name := ""
	if len(parts) != 0 {
		name = parts[len(parts)-1]
	}
	return parent, name, release, nil
}

func (a *operationArea) Read(ctx context.Context, target string, maximum int) ([]byte, bool, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, false, err
	}
	parts, err := operationPath(target, 1)
	if err != nil {
		return nil, false, err
	}
	parent, name, release, err := a.descend(ctx, parts, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer release()
	if maximum <= 0 || maximum > maxOperationLog {
		maximum = maxOperationLog
	}
	data, err := readBounded(ctx, parent, name, maximum, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (a *operationArea) Entries(ctx context.Context, target string) ([]operationstore.Entry, error) {
	if err := a.available(ctx, false); err != nil {
		return nil, err
	}
	parts, err := operationPath(target, 0)
	if err != nil {
		return nil, err
	}
	parent, release, err := a.directory(ctx, parts, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer release()
	names, err := directoryNames(parent, maxOperationEntries)
	if err != nil {
		return nil, state("lifecycle operation directory cannot be listed")
	}
	entries := make([]operationstore.Entry, 0, len(names))
	for _, name := range names {
		if !safeOperationName(name) {
			return nil, state("lifecycle operations contain an unsupported entry")
		}
		file, err := openRelative(parent, name, pathHandle, 0)
		if err != nil {
			return nil, state("lifecycle operation entry is unsafe")
		}
		stat, err := statHandle(file)
		file.Close()
		if err != nil {
			return nil, state("lifecycle operation entry is unsafe")
		}
		directory := stat.Mode&syscall.S_IFMT == syscall.S_IFDIR
		if !directory && stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
			return nil, state("lifecycle operations contain a special file")
		}
		entries = append(entries, operationstore.Entry{Name: name, Directory: directory, Size: stat.Size})
	}
	slices.SortFunc(entries, func(x, y operationstore.Entry) int { return strings.Compare(x.Name, y.Name) })
	return entries, nil
}

// directory resolves a complete path to its own handle, unlike descend which
// stops at the parent of the final component.
func (a *operationArea) directory(ctx context.Context, parts []string, create bool) (*directory, func(), error) {
	operations, closeRoot, err := a.root(ctx, create)
	if err != nil {
		return nil, nil, err
	}
	closers := []func(){closeRoot}
	release := func() {
		for index := len(closers) - 1; index >= 0; index-- {
			closers[index]()
		}
	}
	parent := operations
	for _, part := range parts {
		child, err := openDirectory(parent, part)
		if errors.Is(err, syscall.ENOENT) && create {
			child, err = a.store.newDirectory(ctx, parent, part)
		}
		if err != nil {
			release()
			if errors.Is(err, syscall.ENOENT) {
				return nil, nil, err
			}
			return nil, nil, state("lifecycle operation directory is unsafe")
		}
		closers = append(closers, func() { child.file.Close() })
		parent = child
	}
	return parent, release, nil
}

func (a *operationArea) EnsureDirectory(ctx context.Context, target string) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := operationPath(target, 0)
	if err != nil {
		return err
	}
	_, release, err := a.directory(ctx, parts, true)
	if err != nil {
		return err
	}
	release()
	return nil
}

func (a *operationArea) WriteExclusive(ctx context.Context, target string, data []byte) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if len(data) > maxOperationRecord {
		return state("lifecycle operation record exceeds its byte limit")
	}
	parts, err := operationPath(target, 1)
	if err != nil {
		return err
	}
	parent, name, release, err := a.descend(ctx, parts, true)
	if err != nil {
		return err
	}
	defer release()
	if err := a.capacity(ctx, len(data)); err != nil {
		return err
	}
	return a.store.writeExclusive(ctx, parent, name, data)
}

// Replace publishes atomically after proving the destination still holds
// exactly expected, so a concurrent or mistaken write is refused rather than
// silently overwritten. A nil expectation requires an absent destination.
func (a *operationArea) Replace(ctx context.Context, target string, data, expected []byte) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	if len(data) > maxOperationRecord {
		return state("lifecycle operation record exceeds its byte limit")
	}
	parts, err := operationPath(target, 1)
	if err != nil {
		return err
	}
	parent, name, release, err := a.descend(ctx, parts, true)
	if err != nil {
		return err
	}
	defer release()
	if err := a.capacity(ctx, len(data)); err != nil {
		return err
	}
	if expected == nil {
		return a.store.writeExclusive(ctx, parent, name, data)
	}
	var pending string
	for range 16 {
		candidate, err := a.store.candidate("pending-")
		if err != nil {
			return err
		}
		pending = candidate + ".json"
		err = a.store.writeExclusive(ctx, parent, pending, data)
		if errors.Is(err, syscall.EEXIST) {
			pending = ""
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if pending == "" {
		return state("lifecycle operation publication exhausted its collision limit")
	}
	staged, stagedIdentity, err := readBoundedIdentity(ctx, parent, pending, maxOperationRecord, true)
	if err != nil || !bytes.Equal(staged, data) {
		return state("staged lifecycle record changed before publication")
	}
	if err := a.store.checkpoint(ctx, "before-operation-rename"); err != nil {
		return err
	}
	current, _, err := readBoundedIdentity(ctx, parent, name, maxOperationRecord, false)
	if err != nil || !bytes.Equal(current, expected) {
		return state("lifecycle record changed before publication")
	}
	recheck, recheckIdentity, err := readBoundedIdentity(ctx, parent, pending, maxOperationRecord, true)
	if err != nil || !bytes.Equal(recheck, data) || !sameFile(stagedIdentity, recheckIdentity) {
		return state("staged lifecycle record was substituted")
	}
	if err := parent.verify(); err != nil {
		return err
	}
	if err := syscall.Renameat(int(parent.file.Fd()), pending, int(parent.file.Fd()), name); err != nil {
		return state("lifecycle record could not be atomically published")
	}
	if err := a.store.checkpoint(ctx, "after-operation-rename"); err != nil {
		return err
	}
	published, _, err := readBoundedIdentity(ctx, parent, name, maxOperationRecord, false)
	if err != nil || !bytes.Equal(published, data) {
		return state("published lifecycle record is unsafe")
	}
	return a.store.syncDirectory(ctx, parent)
}

// Append is the only non-atomic effect here: a log is troubleshooting material
// whose partial tail is acceptable, never operation evidence.
func (a *operationArea) Append(ctx context.Context, target string, data []byte) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := operationPath(target, 1)
	if err != nil {
		return err
	}
	parent, name, release, err := a.descend(ctx, parts, true)
	if err != nil {
		return err
	}
	defer release()
	if err := a.capacity(ctx, len(data)); err != nil {
		return err
	}
	if err := a.store.checkpoint(ctx, "append-operation-log"); err != nil {
		return err
	}
	file, err := openRelative(parent, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_APPEND, 0600)
	if err != nil {
		return state("private operation log could not be opened")
	}
	defer file.Close()
	stat, err := statHandle(file)
	if err != nil || !private(stat, syscall.S_IFREG, parent.identity.Uid, parent.identity.Gid) || stat.Mode&0777 != 0600 {
		return state("private operation log is unsafe")
	}
	if stat.Size+int64(len(data)) > maxOperationLog {
		return state("private operation log exceeds its byte limit")
	}
	if _, err := file.Write(data); err != nil {
		return state("private operation log could not be written")
	}
	if err := file.Sync(); err != nil {
		return state("private operation log durability could not be established")
	}
	return nil
}

func (a *operationArea) Sync(ctx context.Context, target string) error {
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := operationPath(target, 0)
	if err != nil {
		return err
	}
	parent, release, err := a.directory(ctx, parts, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	return a.store.syncDirectory(ctx, parent)
}

// capacity bounds the subtree before an allocation, so a runaway operation
// cannot consume the store its own recovery evidence lives in.
func (a *operationArea) capacity(ctx context.Context, additional int) error {
	operations, release, err := a.root(ctx, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	entries, bytes, err := a.scan(ctx, operations, 0)
	if err != nil {
		return err
	}
	if entries+1 > maxOperationEntries || bytes+int64(additional) > maxOperationBytes {
		return state("lifecycle operation storage has reached its bounds")
	}
	return nil
}

func (a *operationArea) scan(ctx context.Context, dir *directory, depth int) (int, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if depth > maxOperationSegments {
		return 0, 0, state("lifecycle operation storage exceeds its depth bound")
	}
	names, err := directoryNames(dir, maxOperationEntries)
	if err != nil {
		return 0, 0, state("lifecycle operation storage cannot be measured")
	}
	entries, total := 0, int64(0)
	for _, name := range names {
		entries++
		child, err := openRelative(dir, name, pathHandle, 0)
		if err != nil {
			return 0, 0, state("lifecycle operation entry is unsafe")
		}
		stat, statErr := statHandle(child)
		child.Close()
		if statErr != nil {
			return 0, 0, state("lifecycle operation entry is unsafe")
		}
		if stat.Mode&syscall.S_IFMT == syscall.S_IFDIR {
			nested, err := openDirectory(dir, name)
			if err != nil {
				return 0, 0, state("lifecycle operation directory is unsafe")
			}
			childEntries, childBytes, err := a.scan(ctx, nested, depth+1)
			nested.file.Close()
			if err != nil {
				return 0, 0, err
			}
			entries += childEntries
			total += childBytes
			continue
		}
		total += stat.Size
	}
	return entries, total, nil
}
