//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

const (
	maxOperationSegments = 6
	maxOperationEntries  = operationstore.MaxEntries
	maxOperationBytes    = operationstore.MaxBytes
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

// unsafeEntry names the entry that would not open as this store's own and the
// answer that refused it. Without the path, one foreign entry anywhere refuses
// every later operation and names nothing; without the cause, a transient
// answer and a foreign entry read identically.
func unsafeEntry(parent *directory, name string, cause error) error {
	message := "lifecycle operation entry is not this store's own: " + storeEntry(parent, name)
	if text := causeText(cause); text != "" {
		message += ": " + text
	}
	return state(message)
}

// storeEntry names an entry relative to the state root, which is how every
// refusal names one, by the held handles' own names rather than the root's
// location on the host.
func storeEntry(dir *directory, name string) string {
	parts := []string{name}
	for current := dir; current != nil && current.parent != nil; current = current.parent {
		parts = append(parts, current.name)
	}
	slices.Reverse(parts)
	return path.Join(parts...)
}

// heldEntry names a held directory relative to the state root, which is "."
// for the root itself.
func heldEntry(dir *directory) string {
	if dir.parent == nil {
		return "."
	}
	return storeEntry(dir.parent, dir.name)
}

// causeText renders why an entry was refused. A refusal this area raised is a
// diagnostic whose Error is a fixed placeholder, so its message carries the
// reason.
func causeText(cause error) string {
	if cause == nil {
		return ""
	}
	reported := diagnostics.Of(cause)
	if len(reported) == 0 {
		return cause.Error()
	}
	messages := make([]string, 0, len(reported))
	for _, diagnostic := range reported {
		messages = append(messages, diagnostic.Message)
	}
	return strings.Join(messages, "; ")
}

// entryConfirmations bounds how often an entry that will not resolve is re-read
// before it is called foreign.
const entryConfirmations = 3

// resolveEntry reads one listed entry, separating an entry this area does not
// own from one a concurrent publication moved while the walk read the
// directory. Every publication stages a pending name beside its target and
// renames it away while another write may be measuring the whole subtree, so a
// refusal is evidence about the entry only once it reproduces.
func (a *operationArea) resolveEntry(ctx context.Context, dir *directory, name string) (syscall.Stat_t, bool, error) {
	var stat syscall.Stat_t
	for attempt := 0; ; attempt++ {
		child, err := openRelative(dir, name, pathHandle, 0)
		if err == nil {
			stat, err = statHandle(child)
			child.Close()
			if err == nil {
				return stat, true, nil
			}
		}
		if errors.Is(err, syscall.ENOENT) {
			return stat, false, nil
		}
		if attempt == entryConfirmations {
			return stat, false, err
		}
		time.Sleep(time.Millisecond << attempt)
		if err := a.store.checkpoint(ctx, checkpointConfirmOperationEntry); err != nil {
			return stat, false, err
		}
	}
}

// confirmDirectory opens a listed entry as this area's own directory under the
// same confirmation.
func (a *operationArea) confirmDirectory(ctx context.Context, parent *directory, name string) (*directory, error) {
	for attempt := 0; ; attempt++ {
		nested, err := openDirectory(parent, name)
		if err == nil || errors.Is(err, syscall.ENOENT) || attempt == entryConfirmations {
			return nested, err
		}
		time.Sleep(time.Millisecond << attempt)
		if err := a.store.checkpoint(ctx, checkpointConfirmOperationEntry); err != nil {
			return nil, err
		}
	}
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
		// A record named as a stage would be removed by the next lease's
		// collection, so no area creates one.
		if !safeOperationName(part) || stageName(part, true) {
			return nil, state("lifecycle operation path component is unsafe")
		}
	}
	return parts, nil
}

// operationArea confines every effect to one context's operations subtree. It
// is valid only while its owning callback holds the root lock and the context
// lease, and it never interprets the records it publishes.
type operationArea struct {
	store *Store
	// subtree is the one directory of the context's state this area may reach:
	// the operation records a registered lifecycle operation owns, or the runs
	// a bounded operation retains beside them. Neither area can name the other.
	subtree  string
	context  *directory
	name     string
	active   func() bool
	readOnly bool
	// cached is set by a constructor whose area is its subtree's only writer
	// for as long as it lives: a lifecycle transaction's operation area, under
	// the exclusive root lock and the context lease, and an SSH-trust
	// mutation's area, under the exclusive root lock. Such an area measures its
	// subtree once and then counts its own writes. A bounded run's area never
	// caches, because every run of its context writes beside it under the
	// shared lock.
	cached  bool
	measure operationMeasure
}

// operationMeasure is what a caching area last measured of its subtree, and
// which measurement that was. A write that succeeds counts its own change into
// the measurement it was admitted against. A write that fails may still have
// landed, and one a later measurement overlapped may already be in it, so
// either discards the measurement and the next write measures again. The
// mutex is held because an attempt's adapter output flushes beside the
// engine's own writes.
type operationMeasure struct {
	mutex      sync.Mutex
	valid      bool
	generation uint64
	entries    int
	bytes      int64
}

// generation names the measurement a write that calls no capacity check is
// counted into.
func (a *operationArea) generation() uint64 {
	if !a.cached {
		return 0
	}
	a.measure.mutex.Lock()
	defer a.measure.mutex.Unlock()
	return a.measure.generation
}

// settle counts one successful write's change into the measurement it was
// admitted against, or discards the measurement another one replaced since.
func (a *operationArea) settle(generation uint64, entries int, bytes int64) {
	if !a.cached {
		return
	}
	a.measure.mutex.Lock()
	defer a.measure.mutex.Unlock()
	if generation != a.measure.generation {
		a.measure.valid = false
		return
	}
	if a.measure.valid {
		a.measure.entries += entries
		a.measure.bytes += bytes
	}
}

// forget discards the measurement after a write that failed, because a failed
// publication may have landed.
func (a *operationArea) forget(err error) {
	if err == nil || !a.cached {
		return
	}
	a.measure.mutex.Lock()
	defer a.measure.mutex.Unlock()
	a.measure.valid = false
}

// newDirectory creates one directory of this subtree and counts it as an
// entry.
func (a *operationArea) newDirectory(ctx context.Context, parent *directory, name string) (*directory, error) {
	generation := a.generation()
	child, err := a.store.newDirectory(ctx, parent, name)
	if err != nil {
		a.forget(err)
		return nil, err
	}
	a.settle(generation, 1, 0)
	return child, nil
}

// storage names this area's subtree in a refusal at its bounds, since three
// areas share them.
func (a *operationArea) storage() string {
	switch a.subtree {
	case runsSubtree:
		return "bounded run storage"
	case trustSubtree:
		return "SSH trust storage"
	}
	return "lifecycle operation storage"
}

// Location names this area's subtree on the host. It is built for a human to
// read, never opened through: every access still descends from the held,
// verified handles above.
func (a *operationArea) Location() string {
	root, err := a.store.rootPath()
	if err != nil || a.name == "" || a.subtree == "" {
		return ""
	}
	return filepath.Join(root, "contexts", a.name, "state", a.subtree)
}

// Reference names the same subtree relative to the state root, the form a
// structured result names a log in.
func (a *operationArea) Reference() string {
	if a.name == "" || a.subtree == "" {
		return ""
	}
	return path.Join("contexts", a.name, "state", a.subtree)
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

// root opens this area's own subtree, creating it only for a mutation. A read
// never repairs or initializes state.
func (a *operationArea) root(ctx context.Context, create bool) (*directory, func(), error) {
	if !safeOperationName(a.subtree) {
		return nil, nil, state("lifecycle area subtree is unsafe")
	}
	runtime, err := openDirectory(a.context, "state")
	if err != nil {
		return nil, nil, state("context state directory is unsafe")
	}
	operations, err := openDirectory(runtime, a.subtree)
	if errors.Is(err, syscall.ENOENT) && create {
		operations, err = a.store.newDirectory(ctx, runtime, a.subtree)
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
			child, err = a.newDirectory(ctx, parent, part)
		}
		if err != nil {
			release()
			if errors.Is(err, syscall.ENOENT) {
				return nil, "", nil, err
			}
			return nil, "", nil, unsafeEntry(parent, part, err)
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
			return nil, unsafeEntry(parent, name, errors.New("its name is not one this area creates"))
		}
		stat, present, err := a.resolveEntry(ctx, parent, name)
		if err != nil {
			return nil, unsafeEntry(parent, name, err)
		}
		if !present {
			continue
		}
		directory := stat.Mode&syscall.S_IFMT == syscall.S_IFDIR
		if !directory && stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
			return nil, unsafeEntry(parent, name, errors.New("it is neither a regular file nor a directory"))
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
			child, err = a.newDirectory(ctx, parent, part)
		}
		if err != nil {
			release()
			if errors.Is(err, syscall.ENOENT) {
				return nil, nil, err
			}
			return nil, nil, unsafeEntry(parent, part, err)
		}
		closers = append(closers, func() { child.file.Close() })
		parent = child
	}
	return parent, release, nil
}

func (a *operationArea) EnsureDirectory(ctx context.Context, target string) (err error) {
	defer func() { a.forget(err) }()
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

// RemoveDirectory removes one empty directory of this subtree and syncs its
// parent. The kernel refuses a directory that holds anything, so a record is
// never removed through it, and one already absent is left so.
func (a *operationArea) RemoveDirectory(ctx context.Context, target string) (err error) {
	defer func() { a.forget(err) }()
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := operationPath(target, 1)
	if err != nil {
		return err
	}
	parent, name, release, err := a.descend(ctx, parts, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	child, err := openDirectory(parent, name)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return unsafeEntry(parent, name, err)
	}
	identity := child.identity
	child.file.Close()
	generation := a.generation()
	if err := unlinkVerified(parent, name, identity, true); err != nil {
		return state("lifecycle operation directory could not be removed: " + storeEntry(parent, name))
	}
	if err := a.store.syncDirectory(ctx, parent); err != nil {
		return err
	}
	a.settle(generation, -1, 0)
	return nil
}

// RemoveRecord unlinks one record of this subtree only while it is the same
// file that held exactly expected when read, and syncs its parent. A directory
// or any other entry refuses as a read does, and a record already absent is
// left so.
func (a *operationArea) RemoveRecord(ctx context.Context, target string, expected []byte) (err error) {
	defer func() { a.forget(err) }()
	if err := a.available(ctx, true); err != nil {
		return err
	}
	parts, err := operationPath(target, 1)
	if err != nil {
		return err
	}
	parent, name, release, err := a.descend(ctx, parts, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	current, identity, err := readBoundedIdentity(ctx, parent, name, maxOperationLog, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return state("lifecycle operation record changed before its removal: " + storeEntry(parent, name))
	}
	generation := a.generation()
	if err := unlinkVerified(parent, name, identity, false); err != nil {
		return state("lifecycle operation record could not be removed: " + storeEntry(parent, name))
	}
	if err := a.store.syncDirectory(ctx, parent); err != nil {
		return err
	}
	a.settle(generation, -1, -int64(len(current)))
	return nil
}

func (a *operationArea) WriteExclusive(ctx context.Context, target string, data []byte) (err error) {
	defer func() { a.forget(err) }()
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
	generation, err := a.capacity(ctx, len(data))
	if err != nil {
		return err
	}
	if err := a.store.writeExclusiveAtomic(ctx, parent, name, data, false); err != nil {
		return err
	}
	a.settle(generation, 1, int64(len(data)))
	return nil
}

// Replace publishes atomically after proving the destination still holds
// exactly expected, so a concurrent or mistaken write is refused rather than
// silently overwritten. A nil expectation requires an absent destination.
func (a *operationArea) Replace(ctx context.Context, target string, data, expected []byte) (err error) {
	defer func() { a.forget(err) }()
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
	generation, err := a.capacity(ctx, len(data))
	if err != nil {
		return err
	}
	if expected == nil {
		if err := a.store.writeExclusiveAtomic(ctx, parent, name, data, false); err != nil {
			return err
		}
		a.settle(generation, 1, int64(len(data)))
		return nil
	}
	_, err = a.store.publishStage(ctx, parent, name, data, stagedPublication{
		subject: "lifecycle record", suffix: ".json", replace: true, bound: maxOperationRecord,
		renameFailure: "lifecycle record could not be atomically published",
		prove: func(ctx context.Context) error {
			current, _, err := readBoundedIdentity(ctx, parent, name, maxOperationRecord, false)
			if err != nil || !bytes.Equal(current, expected) {
				return state("lifecycle record changed before publication")
			}
			return nil
		},
	}, checkpointBeforeOperationRename, checkpointAfterOperationRename)
	if err != nil {
		return err
	}
	a.settle(generation, 0, int64(len(data))-int64(len(expected)))
	return nil
}

// Append is the only non-atomic effect here: a log is troubleshooting material
// whose partial tail is acceptable, never operation evidence.
func (a *operationArea) Append(ctx context.Context, target string, data []byte) (err error) {
	defer func() { a.forget(err) }()
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
	generation, err := a.capacity(ctx, len(data))
	if err != nil {
		return err
	}
	if err := a.store.checkpoint(ctx, checkpointAppendOperationLog); err != nil {
		return err
	}
	file, created, err := openLog(parent, name)
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
	entries := 0
	if created {
		entries = 1
	}
	a.settle(generation, entries, int64(len(data)))
	return nil
}

// openLog opens a log for appending and reports whether this call created it,
// which a caching area counts as one more entry. A log another writer created
// first is appended to as if it had existed.
func openLog(parent *directory, name string) (*os.File, bool, error) {
	file, err := openRelative(parent, name, syscall.O_WRONLY|syscall.O_APPEND, 0)
	if !errors.Is(err, syscall.ENOENT) {
		return file, false, err
	}
	file, err = openRelative(parent, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_APPEND, 0600)
	if errors.Is(err, syscall.EEXIST) {
		file, err = openRelative(parent, name, syscall.O_WRONLY|syscall.O_APPEND, 0)
		return file, false, err
	}
	return file, err == nil, err
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
// cannot consume the store its own recovery evidence lives in. It reports the
// measurement the write it admits counts its change into.
func (a *operationArea) capacity(ctx context.Context, additional int) (uint64, error) {
	entries, bytes, generation, err := a.measured(ctx)
	if errors.Is(err, syscall.ENOENT) {
		return generation, nil
	}
	if err != nil {
		return generation, err
	}
	if entries+1 > maxOperationEntries || bytes+int64(additional) > maxOperationBytes {
		return generation, state(a.storage() + " has reached its bounds")
	}
	return generation, nil
}

// measured is the subtree's size: scanned for every write of an area that
// does not cache, and scanned once and then counted for one that does.
func (a *operationArea) measured(ctx context.Context) (int, int64, uint64, error) {
	if !a.cached {
		entries, bytes, err := a.measureSubtree(ctx)
		return entries, bytes, 0, err
	}
	a.measure.mutex.Lock()
	defer a.measure.mutex.Unlock()
	if a.measure.valid {
		return a.measure.entries, a.measure.bytes, a.measure.generation, nil
	}
	entries, bytes, err := a.measureSubtree(ctx)
	if err != nil {
		return 0, 0, a.measure.generation, err
	}
	a.measure.generation++
	a.measure.valid, a.measure.entries, a.measure.bytes = true, entries, bytes
	return entries, bytes, a.measure.generation, nil
}

func (a *operationArea) measureSubtree(ctx context.Context) (int, int64, error) {
	operations, release, err := a.root(ctx, false)
	if err != nil {
		return 0, 0, err
	}
	defer release()
	return a.scan(ctx, operations, 0)
}

// scan measures the subtree, and one entry this store does not own refuses the
// write it measures for. Resolution is confirmed first, because a listed name
// legitimately moves under this walk.
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
		if err := a.store.checkpoint(ctx, checkpointMeasureOperationEntry); err != nil {
			return 0, 0, err
		}
		stat, present, err := a.resolveEntry(ctx, dir, name)
		if err != nil {
			return 0, 0, unsafeEntry(dir, name, err)
		}
		if !present {
			continue
		}
		entries++
		if stat.Mode&syscall.S_IFMT == syscall.S_IFDIR {
			nested, err := a.confirmDirectory(ctx, dir, name)
			if errors.Is(err, syscall.ENOENT) {
				entries--
				continue
			}
			if err != nil {
				return 0, 0, unsafeEntry(dir, name, err)
			}
			childEntries, childBytes, err := a.scan(ctx, nested, depth+1)
			nested.file.Close()
			if err != nil && vanished(dir, name) {
				entries--
				continue
			}
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

// vanished reports whether a directory whose walk failed is gone from its name,
// as a run another run's retention removed under the shared lock is. Its walk
// failed only because it went, so it is absent, as a directory gone before the
// walk opened it is; one replaced at its name has not vanished and still
// refuses.
func vanished(parent *directory, name string) bool {
	entry, err := openRelative(parent, name, pathHandle, 0)
	if err == nil {
		entry.Close()
	}
	return errors.Is(err, syscall.ENOENT)
}
