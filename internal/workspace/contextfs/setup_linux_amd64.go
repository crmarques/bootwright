//go:build linux && amd64

package contextfs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	setupSubtree            = "setup"
	setupLockName           = "lock"
	setupStateName          = "state.json"
	setupRuntimesName       = "runtimes"
	setupCacheName          = "cache"
	setupWheelsName         = "wheels"
	runtimeStagePrefix      = ".stage-"
	runtimeLockName         = ".lock"
	runtimeRecordName       = "runtime.json"
	maxRuntimeRecord        = 64 << 10
	maxSetupEntries         = 32
	maxSetupTreeEntries     = 1 << 18
	maxSetupTreeDepth       = 32
	unrecognizedSetupFormat = "content this build does not recognize"
	setupLockPoll           = 10 * time.Millisecond
	maxSetupLockPoll        = 250 * time.Millisecond
)

var errNoRuntimeLock = errors.New("the runtime holds no launch lock")

var (
	_ prerequisites.SetupStore  = (*Store)(nil)
	_ prerequisites.SetupWaiter = (*Store)(nil)
)

func (s *Store) ReadSetup(ctx context.Context) (prerequisites.SetupView, error) {
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return prerequisites.SetupView{}, nil
	}
	if err != nil {
		return prerequisites.SetupView{}, safeError(err)
	}
	defer root.file.Close()
	if err := admitRegistry(ctx, root); err != nil {
		return prerequisites.SetupView{}, safeError(err)
	}
	dir, err := openDirectory(root, setupSubtree)
	if errors.Is(err, syscall.ENOENT) {
		return prerequisites.SetupView{}, nil
	}
	if err != nil {
		return prerequisites.SetupView{}, safeError(err)
	}
	defer dir.file.Close()
	view, err := readSetupView(ctx, dir)
	return view, safeError(err)
}

// admitRegistry refuses another format's store before setup previews or
// plans anything, as every later registry access would.
func admitRegistry(ctx context.Context, root *directory) error {
	data, err := readBounded(ctx, root, "registry.json", maxRegistry, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	var registry contexts.Registry
	return decodeRegistry(data, maxRegistry, &registry)
}

func readSetupView(ctx context.Context, dir *directory) (prerequisites.SetupView, error) {
	view, err := readSetupState(ctx, dir)
	if err != nil || view.Foreign != "" {
		return view, err
	}
	known, err := recognizedSetup(ctx, dir)
	if err != nil {
		return prerequisites.SetupView{}, err
	}
	if !known {
		return prerequisites.SetupView{Exists: true, Foreign: unrecognizedSetupFormat}, nil
	}
	return view, nil
}

func readSetupState(ctx context.Context, dir *directory) (prerequisites.SetupView, error) {
	data, err := readBounded(ctx, dir, setupStateName, maxSetupState, false)
	if errors.Is(err, syscall.ENOENT) {
		return prerequisites.SetupView{}, nil
	}
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return prerequisites.SetupView{}, canceled
		}
		return prerequisites.SetupView{Exists: true, Foreign: unreadableSetupFormat}, nil
	}
	value, foreign := decodeSetupState(data)
	return prerequisites.SetupView{Exists: true, State: value, Foreign: foreign}, nil
}

func (s *Store) MutateSetup(ctx context.Context, callback func(prerequisites.SetupTransaction) error) error {
	return s.mutateSetup(ctx, callback, false)
}

func (s *Store) AwaitSetup(ctx context.Context, callback func(prerequisites.SetupTransaction) error) error {
	return s.mutateSetup(ctx, callback, true)
}

func (s *Store) mutateSetup(ctx context.Context, callback func(prerequisites.SetupTransaction) error, wait bool) error {
	if callback == nil {
		return state("setup mutation callback is missing")
	}
	if err := s.ensureSetupSubtree(ctx); err != nil {
		return err
	}
	root, err := s.openRoot(ctx, false, nil)
	if err != nil {
		return safeError(err)
	}
	defer root.file.Close()
	if err := admitRegistry(ctx, root); err != nil {
		return safeError(err)
	}
	dir, err := openDirectory(root, setupSubtree)
	if err != nil {
		return safeError(err)
	}
	defer dir.file.Close()
	held, err := lockSetup(ctx, dir, wait)
	if err != nil {
		return err
	}
	defer unlockAndClose(held)
	tx := &setupTransaction{store: s, dir: dir, active: true}
	defer func() { tx.active = false }()
	if err := tx.admit(ctx); err != nil {
		return safeError(err)
	}
	return safeError(callback(tx))
}

func (s *Store) ensureSetupSubtree(ctx context.Context) error {
	root, err := s.openRoot(ctx, false, nil)
	if err == nil {
		dir, openErr := openDirectory(root, setupSubtree)
		root.file.Close()
		if openErr == nil {
			dir.file.Close()
			return nil
		}
		err = openErr
	}
	if !errors.Is(err, syscall.ENOENT) {
		return safeError(err)
	}
	return s.Transact(ctx, true, nil, func(base contexts.Transaction) error {
		t := base.(*transaction)
		dir, err := t.store.ensureDirectory(ctx, t.root, setupSubtree)
		if err != nil {
			return err
		}
		dir.file.Close()
		return nil
	})
}

// lockSetup polls rather than blocks when it waits, so cancellation still
// ends the wait.
func lockSetup(ctx context.Context, dir *directory, wait bool) (*os.File, error) {
	file, err := openRelative(dir, setupLockName, syscall.O_RDWR|syscall.O_CREAT, 0600)
	if err != nil {
		return nil, safeError(err)
	}
	stat, err := statHandle(file)
	if err != nil || !private(stat, syscall.S_IFREG, dir.identity.Uid, dir.identity.Gid) || stat.Dev != dir.identity.Dev {
		file.Close()
		return nil, state("setup lock type, owner or permissions is unsafe")
	}
	for delay := setupLockPoll; ; delay = min(2*delay, maxSetupLockPoll) {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			file.Close()
			return nil, state("controller setup lock could not be taken")
		}
		if !wait {
			file.Close()
			return nil, busy("controller setup is held by another command")
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err := dir.verify(); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

type setupTransaction struct {
	store  *Store
	dir    *directory
	view   prerequisites.SetupView
	active bool
}

func (t *setupTransaction) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !t.active {
		return state("setup storage capability is no longer available")
	}
	return t.dir.verify()
}

func (t *setupTransaction) owned(ctx context.Context) error {
	if err := t.available(ctx); err != nil {
		return err
	}
	if t.view.Foreign != "" {
		return state("setup state another build wrote must be discarded first")
	}
	return nil
}

func (t *setupTransaction) admit(ctx context.Context) error {
	view, err := readSetupView(ctx, t.dir)
	if err != nil {
		return err
	}
	t.view = view
	if t.view.Foreign != "" {
		return nil
	}
	return t.sweepStages(ctx)
}

func recognizedSetup(ctx context.Context, dir *directory) (bool, error) {
	names, err := directoryNames(dir, maxSetupEntries)
	if err != nil {
		return false, ctx.Err()
	}
	for _, name := range names {
		switch name {
		case setupLockName, setupStateName:
		case setupRuntimesName, setupCacheName, setupRunsName:
			child, err := openDirectory(dir, name)
			if err != nil {
				return false, ctx.Err()
			}
			child.file.Close()
		default:
			if !stageName(name, false) {
				return false, nil
			}
		}
	}
	return true, nil
}

func (t *setupTransaction) sweepStages(ctx context.Context) error {
	if err := t.store.collectStages(ctx, t.dir, maxSetupEntries, maxSetupState, false, -1); err != nil {
		return err
	}
	runtimes, err := openDirectory(t.dir, setupRuntimesName)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer runtimes.file.Close()
	names, err := rawNames(runtimes, maxSetupTreeEntries)
	if err != nil {
		return err
	}
	removed := false
	for _, name := range names {
		if !strings.HasPrefix(name, runtimeStagePrefix) {
			continue
		}
		if err := t.store.removeSetupEntry(ctx, runtimes, name); err != nil {
			return err
		}
		removed = true
	}
	if !removed {
		return nil
	}
	return t.store.syncDirectory(ctx, runtimes)
}

func (t *setupTransaction) View() prerequisites.SetupView {
	if !t.active {
		return prerequisites.SetupView{}
	}
	view := t.view
	if view.Foreign == "" && view.Exists {
		view.State = normalizedSetupState(view.State)
	}
	return view
}

func (t *setupTransaction) Record(ctx context.Context, value prerequisites.SetupState) error {
	if err := t.owned(ctx); err != nil {
		return err
	}
	data, err := encodeSetupState(value)
	if err != nil {
		return err
	}
	_, err = t.store.publishStage(ctx, t.dir, setupStateName, data, stagedPublication{
		subject: "setup record", suffix: ".json", replace: true, bound: maxSetupState,
	}, checkpointBeforeSetupStateRename, checkpointAfterSetupStateRename)
	if err != nil {
		return err
	}
	recorded, _ := decodeSetupState(data)
	t.view = prerequisites.SetupView{Exists: true, State: recorded}
	return nil
}

func (t *setupTransaction) DiscardForeign(ctx context.Context) error {
	if err := t.available(ctx); err != nil {
		return err
	}
	if t.view.Foreign == "" {
		return state("setup state this build wrote is never discarded")
	}
	held, err := t.claimRuntimes()
	defer func() {
		for _, file := range held {
			file.Close()
		}
	}()
	if err != nil {
		return err
	}
	names, err := rawNames(t.dir, maxSetupTreeEntries)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == setupLockName {
			continue
		}
		if err := t.store.removeSetupEntry(ctx, t.dir, name); err != nil {
			return err
		}
	}
	if err := t.store.syncDirectory(ctx, t.dir); err != nil {
		return err
	}
	t.view = prerequisites.SetupView{}
	return nil
}

func (t *setupTransaction) claimRuntimes() ([]*os.File, error) {
	runtimes, err := openDirectory(t.dir, setupRuntimesName)
	if err != nil {
		return nil, nil
	}
	defer runtimes.file.Close()
	names, err := rawNames(runtimes, maxSetupTreeEntries)
	if err != nil {
		return nil, err
	}
	held := []*os.File{}
	for _, name := range names {
		file, err := holdRuntimeLock(runtimes, name, syscall.LOCK_EX)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return held, busy("a controller runtime is in use by a running command")
		}
		if err == nil {
			held = append(held, file)
		}
	}
	return held, nil
}

func (t *setupTransaction) OpenRun(ctx context.Context) (string, prerequisites.SetupRun, error) {
	if err := t.owned(ctx); err != nil {
		return "", nil, err
	}
	runs, err := t.store.ensureDirectory(ctx, t.dir, setupRunsName)
	if err != nil {
		return "", nil, unsafeSetupRun(storeEntry(t.dir, setupRunsName), "it cannot be opened as a private directory")
	}
	defer runs.file.Close()
	return t.store.nextSetupRun(ctx, runs)
}

func (t *setupTransaction) WheelPath(ctx context.Context, sha256, filename string) (string, error) {
	if err := t.owned(ctx); err != nil {
		return "", err
	}
	if !validControllerDigest(sha256) || !wheelName(filename) {
		return "", state("cached wheel identity is invalid")
	}
	parent := t.dir
	for _, name := range []string{setupCacheName, setupWheelsName, sha256} {
		child, err := t.store.ensureDirectory(ctx, parent, name)
		if err != nil {
			return "", err
		}
		defer child.file.Close()
		parent = child
	}
	return filepath.Join(parent.path, filename), nil
}

func (t *setupTransaction) PruneWheels(ctx context.Context, keep []string) (int, error) {
	if err := t.owned(ctx); err != nil {
		return 0, err
	}
	for _, digest := range keep {
		if !validControllerDigest(digest) {
			return 0, state("cached wheel identity is invalid")
		}
	}
	cache, err := openDirectory(t.dir, setupCacheName)
	if errors.Is(err, syscall.ENOENT) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer cache.file.Close()
	wheels, err := openDirectory(cache, setupWheelsName)
	if errors.Is(err, syscall.ENOENT) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer wheels.file.Close()
	names, err := rawNames(wheels, maxSetupTreeEntries)
	if err != nil {
		return 0, err
	}
	pruned := 0
	for _, name := range names {
		if slices.Contains(keep, name) {
			continue
		}
		if err := t.store.removeSetupEntry(ctx, wheels, name); err != nil {
			return pruned, err
		}
		pruned++
	}
	if pruned == 0 {
		return 0, nil
	}
	return pruned, t.store.syncDirectory(ctx, wheels)
}

func wheelName(name string) bool {
	return len(name) > len(".whl") && len(name) <= 255 && strings.HasSuffix(name, ".whl") && name[0] != '.' && setupText(name) && !strings.ContainsAny(name, "/ ")
}

type runtimeStage struct {
	owner    *setupTransaction
	name     string
	path     string
	identity syscall.Stat_t
	done     bool
}

func (s *runtimeStage) Path() string { return s.path }

func (t *setupTransaction) StageRuntime(ctx context.Context) (prerequisites.RuntimeStage, error) {
	if err := t.owned(ctx); err != nil {
		return nil, err
	}
	runtimes, err := t.store.ensureDirectory(ctx, t.dir, setupRuntimesName)
	if err != nil {
		return nil, err
	}
	defer runtimes.file.Close()
	for range 16 {
		name, err := t.store.candidate(runtimeStagePrefix)
		if err != nil {
			return nil, err
		}
		stage, err := t.store.newDirectory(ctx, runtimes, name)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		if err != nil {
			return nil, err
		}
		stage.file.Close()
		return &runtimeStage{owner: t, name: name, path: stage.path, identity: stage.identity}, nil
	}
	return nil, state("runtime stage exhausted its collision limit")
}

func (t *setupTransaction) PublishRuntime(ctx context.Context, stage prerequisites.RuntimeStage, digest string) (bool, error) {
	if err := t.owned(ctx); err != nil {
		return false, err
	}
	staged, handed := stage.(*runtimeStage)
	if !handed || staged.owner != t || staged.done {
		return false, state("runtime stage is not one this setup holds")
	}
	if !validControllerDigest(digest) {
		return false, state("runtime identity is invalid")
	}
	runtimes, err := openDirectory(t.dir, setupRuntimesName)
	if err != nil {
		return false, err
	}
	defer runtimes.file.Close()
	dir, err := openDirectory(runtimes, staged.name)
	if err != nil || !sameIdentity(dir.identity, staged.identity) {
		if err == nil {
			dir.file.Close()
		}
		return false, state("runtime stage was replaced")
	}
	defer dir.file.Close()
	if err := sealRuntime(dir); err != nil {
		return false, err
	}
	if err := t.store.checkpoint(ctx, checkpointBeforeSetupRuntimeRename); err != nil {
		return false, err
	}
	if err := syncFilesystem(dir.file); err != nil {
		return false, err
	}
	err = renameNoReplaceAt(runtimes, staged.name, digest)
	if errors.Is(err, syscall.EEXIST) {
		staged.done = true
		if err := t.store.removeSetupEntry(ctx, runtimes, staged.name); err != nil {
			return false, err
		}
		return false, t.store.syncDirectory(ctx, runtimes)
	}
	if err != nil {
		return false, err
	}
	staged.done = true
	return true, t.store.syncDirectory(ctx, runtimes)
}

func (t *setupTransaction) DiscardRuntime(ctx context.Context, stage prerequisites.RuntimeStage) error {
	if err := t.owned(ctx); err != nil {
		return err
	}
	staged, handed := stage.(*runtimeStage)
	if !handed || staged.owner != t {
		return state("runtime stage is not one this setup holds")
	}
	if staged.done {
		return nil
	}
	runtimes, err := openDirectory(t.dir, setupRuntimesName)
	if err != nil {
		return err
	}
	defer runtimes.file.Close()
	staged.done = true
	if err := t.store.removeSetupEntry(ctx, runtimes, staged.name); err != nil {
		return err
	}
	return t.store.syncDirectory(ctx, runtimes)
}

func sealRuntime(dir *directory) error {
	record, err := openRelative(dir, runtimeRecordName, pathHandle, 0)
	if err != nil {
		return state("a runtime is published only with its " + runtimeRecordName)
	}
	stat, err := statHandle(record)
	record.Close()
	if err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Uid != dir.identity.Uid || stat.Dev != dir.identity.Dev || stat.Nlink != 1 || stat.Size < 1 || stat.Size > maxRuntimeRecord {
		return state("a runtime is published only with its " + runtimeRecordName)
	}
	lock, err := openRelative(dir, runtimeLockName, syscall.O_RDONLY|syscall.O_CREAT, 0600)
	if err != nil {
		return state("runtime lock could not be created")
	}
	stat, err = statHandle(lock)
	lock.Close()
	if err != nil || !private(stat, syscall.S_IFREG, dir.identity.Uid, dir.identity.Gid) {
		return state("runtime lock type, owner or permissions is unsafe")
	}
	return nil
}

func (t *setupTransaction) Runtimes(ctx context.Context) ([]string, error) {
	if err := t.owned(ctx); err != nil {
		return nil, err
	}
	runtimes, err := openDirectory(t.dir, setupRuntimesName)
	if errors.Is(err, syscall.ENOENT) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer runtimes.file.Close()
	names, err := rawNames(runtimes, maxSetupTreeEntries)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	digests := []string{}
	for _, name := range names {
		if validControllerDigest(name) {
			digests = append(digests, name)
		}
	}
	return digests, nil
}

func (t *setupTransaction) RetireRuntime(ctx context.Context, digest string) (bool, error) {
	if err := t.owned(ctx); err != nil {
		return false, err
	}
	if !validControllerDigest(digest) {
		return false, state("runtime identity is invalid")
	}
	runtimes, err := openDirectory(t.dir, setupRuntimesName)
	if errors.Is(err, syscall.ENOENT) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer runtimes.file.Close()
	held, err := holdRuntimeLock(runtimes, digest, syscall.LOCK_EX)
	switch {
	case errors.Is(err, syscall.ENOENT):
		return true, nil
	case errors.Is(err, syscall.EWOULDBLOCK):
		return false, nil
	case errors.Is(err, errNoRuntimeLock):
		// No launch can hold a runtime whose lock is gone.
	case err != nil:
		return false, err
	default:
		defer held.Close()
	}
	withdrawn, err := t.store.candidate(runtimeStagePrefix)
	if err != nil {
		return false, err
	}
	if err := renameNoReplaceAt(runtimes, digest, withdrawn); err != nil {
		return false, state("runtime could not be withdrawn before its removal")
	}
	if err := t.store.syncDirectory(ctx, runtimes); err != nil {
		return false, err
	}
	if err := t.store.removeSetupEntry(ctx, runtimes, withdrawn); err != nil {
		return false, err
	}
	return true, t.store.syncDirectory(ctx, runtimes)
}

func holdRuntimeLock(runtimes *directory, digest string, how int) (*os.File, error) {
	dir, err := openDirectory(runtimes, digest)
	if err != nil {
		return nil, err
	}
	defer dir.file.Close()
	file, err := openRelative(dir, runtimeLockName, syscall.O_RDONLY, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, errNoRuntimeLock
	}
	if err != nil {
		return nil, err
	}
	stat, err := statHandle(file)
	if err != nil || !private(stat, syscall.S_IFREG, dir.identity.Uid, dir.identity.Gid) {
		file.Close()
		return nil, state("runtime lock type, owner or permissions is unsafe")
	}
	if err := syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	if err := dir.verify(); err != nil {
		file.Close()
		return nil, syscall.EWOULDBLOCK
	}
	return file, nil
}

type runtimeHold struct {
	once sync.Once
	file *os.File
	path string
}

func (h *runtimeHold) Path() string { return h.path }

func (h *runtimeHold) Release() { h.once.Do(func() { h.file.Close() }) }

func (s *Store) HoldRuntime(ctx context.Context, digest string) (prerequisites.RuntimeHold, error) {
	if !validControllerDigest(digest) {
		return nil, state("runtime identity is invalid")
	}
	root, err := s.openRoot(ctx, false, nil)
	if err != nil {
		return nil, runtimeUnavailable(err)
	}
	defer root.file.Close()
	setup, err := openDirectory(root, setupSubtree)
	if err != nil {
		return nil, runtimeUnavailable(err)
	}
	defer setup.file.Close()
	runtimes, err := openDirectory(setup, setupRuntimesName)
	if err != nil {
		return nil, runtimeUnavailable(err)
	}
	defer runtimes.file.Close()
	file, err := holdRuntimeLock(runtimes, digest, syscall.LOCK_SH)
	if err != nil {
		return nil, runtimeUnavailable(err)
	}
	if err := runtimeNamed(runtimes, digest); err != nil {
		file.Close()
		return nil, err
	}
	return &runtimeHold{file: file, path: filepath.Join(runtimes.path, digest)}, nil
}

// runtimeNamed requires the held runtime's record to name its own digest, so
// a runtime renamed into another's place is never launched as that one.
func runtimeNamed(runtimes *directory, digest string) error {
	dir, err := openDirectory(runtimes, digest)
	if err != nil {
		return runtimeUnavailable(err)
	}
	defer dir.file.Close()
	file, err := openRelative(dir, runtimeRecordName, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return state("runtime " + digest[:12] + " holds no readable " + runtimeRecordName)
	}
	defer file.Close()
	stat, err := statHandle(file)
	if err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Uid != dir.identity.Uid || stat.Dev != dir.identity.Dev || stat.Nlink != 1 || stat.Size > maxRuntimeRecord {
		return state("runtime " + digest[:12] + " holds no readable " + runtimeRecordName)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRuntimeRecord+1))
	var record struct {
		Runtime struct {
			Digest string `json:"digest"`
		} `json:"runtime"`
	}
	if err != nil || len(data) > maxRuntimeRecord || json.Unmarshal(data, &record) != nil || record.Runtime.Digest != digest {
		return state("runtime " + digest[:12] + " holds a " + runtimeRecordName + " that names another runtime")
	}
	return nil
}

func runtimeUnavailable(err error) error {
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, errNoRuntimeLock) {
		return prerequisites.ErrRuntimeUnavailable
	}
	return safeError(err)
}

func (s *Store) removeSetupEntry(ctx context.Context, parent *directory, name string) error {
	stat, err := listedEntry(parent, name)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil || !removableSetupEntry(stat, parent.identity) {
		return state("setup entry is not this store's own: " + storeEntry(parent, name))
	}
	isDirectory := stat.Mode&syscall.S_IFMT == syscall.S_IFDIR
	if isDirectory {
		remaining := maxSetupTreeEntries
		if err := emptySetupTree(ctx, parent, name, stat, parent.identity, 0, &remaining); err != nil {
			return err
		}
	}
	if errno := unlinkAt(parent, name, isDirectory); errno != 0 {
		return state("setup entry could not be removed: " + storeEntry(parent, name))
	}
	return nil
}

func emptySetupTree(ctx context.Context, parent *directory, name string, identity, owner syscall.Stat_t, depth int, remaining *int) error {
	if depth > maxSetupTreeDepth {
		return state("setup tree exceeds its depth bound")
	}
	file, err := openWithin(parent, name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return state("setup tree directory cannot be opened without following a link")
	}
	defer file.Close()
	current, err := statHandle(file)
	if err != nil || !sameIdentity(identity, current) {
		return state("setup tree directory was substituted")
	}
	dir := &directory{file: file, path: filepath.Join(parent.path, name), identity: current}
	names, err := rawNames(dir, *remaining)
	if err != nil {
		return err
	}
	*remaining -= len(names)
	for _, entry := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		stat, err := listedEntry(dir, entry)
		if err != nil || !removableSetupEntry(stat, owner) {
			return state("setup tree holds content this store did not write")
		}
		isDirectory := stat.Mode&syscall.S_IFMT == syscall.S_IFDIR
		if isDirectory {
			if err := emptySetupTree(ctx, dir, entry, stat, owner, depth+1, remaining); err != nil {
				return err
			}
		}
		if errno := unlinkAt(dir, entry, isDirectory); errno != 0 {
			return state("setup tree entry could not be removed")
		}
	}
	return nil
}

func removableSetupEntry(stat, owner syscall.Stat_t) bool {
	kind := stat.Mode & syscall.S_IFMT
	return (kind == syscall.S_IFDIR || kind == syscall.S_IFREG || kind == syscall.S_IFLNK) && stat.Uid == owner.Uid && stat.Dev == owner.Dev
}

func rawNames(dir *directory, maximum int) ([]string, error) {
	file, err := openWithin(dir, ".", syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, state("setup tree cannot be enumerated")
	}
	defer file.Close()
	names := []string{}
	for {
		part, err := file.Readdirnames(1024)
		names = append(names, part...)
		if len(names) > maximum {
			return nil, state("setup tree exceeds its entry bound")
		}
		if errors.Is(err, io.EOF) || err == nil && len(part) == 0 {
			return names, nil
		}
		if err != nil {
			return nil, state("setup tree cannot be enumerated")
		}
	}
}
