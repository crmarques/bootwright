//go:build linux && amd64

package nativelocal

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"golang.org/x/sys/unix"
)

// StagingParent is the private parent of every dependency resolution stage: a
// sibling of the verified store, never inside it, because setup resolves
// before that store may exist and the store admits no entry it does not know.
const StagingParent = "/var/lib/bootwright-staging"

const (
	stageLock         = "lock"
	stageOwner        = "owner"
	maxParentEntries  = 256
	maxStageTreeDepth = 64
	maxStageEntries   = 1 << 20
)

var stageKinds = []string{"resolver", "native", "rpmdb"}

// Staging hands out stages beneath one parent. A stage is a directory named
// for its kind and a suffix unlikely to repeat, holding the lock its
// invocation keeps exclusively for the stage's lifetime and then an owner
// marker, exactly as an adapter job is claimed: the next Stage removes a stage
// whose lock is free, leaves one without its marker, which is still being
// created, and never follows a link or crosses into another filesystem doing
// so.
type Staging struct {
	parent string
	// entries bounds the entries a sweep reads from one stage tree; zero is
	// maxStageEntries.
	entries int
}

var _ prerequisites.Staging = (*Staging)(nil)

func NewStaging(parent string) *Staging { return &Staging{parent: parent} }

func (s *Staging) Stage(ctx context.Context, kind string) (prerequisites.Stage, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.Stage{}, err
	}
	if s == nil || s.parent == "" || !slices.Contains(stageKinds, kind) {
		return prerequisites.Stage{}, failure("native resolver staging is unavailable")
	}
	parent, err := s.openParent()
	if err != nil {
		return prerequisites.Stage{}, err
	}
	defer parent.close()
	if err := parent.sweep(); err != nil {
		return prerequisites.Stage{}, err
	}
	return parent.create(kind)
}

// stagingParent is the held parent: every entry beneath it is reached through
// root, owner is the only identity whose entries are this executable's,
// device is the filesystem a stage may not leave, and entries bounds what
// removing one stage reads.
type stagingParent struct {
	path    string
	root    *os.Root
	owner   uint32
	device  uint64
	noexec  bool
	entries int
}

// openParent creates the parent when it is absent and otherwise requires a
// directory reached without a link, owned by this identity and writable by no
// other, so no one else can place or replace a stage in it.
func (s *Staging) openParent() (*stagingParent, error) {
	owner := uint32(os.Geteuid())
	refuse := func() (*stagingParent, error) {
		return nil, &prerequisites.ScopedFailure{
			Code:       "controller.setup",
			Message:    "the resolution staging directory " + s.parent + " is not a directory this user owns and alone may write",
			Correction: "Remove " + s.parent + " or make it a directory owned by user " + strconv.FormatUint(uint64(owner), 10) + " with mode 0711",
		}
	}
	if err := os.Mkdir(s.parent, 0711); err == nil {
		if err := os.Chmod(s.parent, 0711); err != nil {
			return refuse()
		}
	} else if !errors.Is(err, fs.ErrExist) {
		return nil, &prerequisites.ScopedFailure{Code: "controller.setup", Message: "the resolution staging directory " + s.parent + " cannot be created", Correction: "Restore the parent directory of " + s.parent}
	}
	listed, err := os.Lstat(s.parent)
	if err != nil || !listed.IsDir() {
		return refuse()
	}
	root, err := os.OpenRoot(s.parent)
	if err != nil {
		return refuse()
	}
	opened, err := root.Stat(".")
	stat, ok := statOf(opened, err)
	if !ok || !opened.IsDir() || !os.SameFile(listed, opened) || stat.Uid != owner || stat.Mode&0022 != 0 {
		root.Close()
		return refuse()
	}
	entries := s.entries
	if entries <= 0 {
		entries = maxStageEntries
	}
	parent := &stagingParent{path: s.parent, root: root, owner: owner, device: stat.Dev, entries: entries}
	listing, err := root.Open(".")
	if err != nil {
		root.Close()
		return refuse()
	}
	var filesystem unix.Statfs_t
	err = unix.Fstatfs(int(listing.Fd()), &filesystem)
	listing.Close()
	if err != nil {
		root.Close()
		return refuse()
	}
	parent.noexec = filesystem.Flags&unix.ST_NOEXEC != 0
	return parent, nil
}

func (p *stagingParent) close() { p.root.Close() }

func (p *stagingParent) unswept() error {
	return &prerequisites.ScopedFailure{
		Code:       "controller.setup",
		Message:    "a stale resolution stage in " + p.path + " could not be removed safely",
		Correction: "Remove the stages in " + p.path + " that no process holds",
	}
}

// sweep removes every stage of every kind whose lock is free. The parent holds
// stages only, so a parent holding more entries than the bound refuses rather
// than being read without limit.
func (p *stagingParent) sweep() error {
	names, err := p.names()
	if err != nil {
		if errors.Is(err, errParentBound) {
			return &prerequisites.ScopedFailure{
				Code:       "controller.setup",
				Message:    p.path + " holds more than " + strconv.Itoa(maxParentEntries) + " entries",
				Correction: "Remove the entries in " + p.path + " that no process holds",
			}
		}
		return p.unswept()
	}
	for _, name := range names {
		if !stageName(name) {
			continue
		}
		if _, err := p.reap(name, nil); err != nil {
			return p.unswept()
		}
	}
	return nil
}

var errParentBound = errors.New("staging parent entry bound")

func (p *stagingParent) names() ([]string, error) {
	listing, err := p.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer listing.Close()
	var names []string
	for {
		part, err := listing.Readdirnames(maxParentEntries)
		names = append(names, part...)
		if len(names) > maxParentEntries {
			return nil, errParentBound
		}
		if errors.Is(err, io.EOF) {
			slices.Sort(names)
			return names, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// create claims a new stage: the directory exclusively, its lock before
// anything else, then the owner marker, and only then the traversal the
// unprivileged helper needs to reach what the stage will hold.
func (p *stagingParent) create(kind string) (prerequisites.Stage, error) {
	unavailable := &prerequisites.ScopedFailure{
		Code:       "controller.setup",
		Message:    "a resolution stage cannot be created in " + p.path,
		Correction: "Free space on the filesystem holding " + p.path + " and keep it a directory owned by user " + strconv.FormatUint(uint64(p.owner), 10) + " with mode 0711",
	}
	var name string
	for attempt := 0; ; attempt++ {
		name = kind + "-" + stageSuffix()
		err := p.root.Mkdir(name, 0700)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || attempt == 3 {
			return prerequisites.Stage{}, unavailable
		}
	}
	stage, stat, ok := p.directory(name)
	if !ok {
		return prerequisites.Stage{}, unavailable
	}
	defer stage.Close()
	lock, err := stage.OpenFile(stageLock, os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		_ = p.root.Remove(name)
		return prerequisites.Stage{}, unavailable
	}
	discard := func() (prerequisites.Stage, error) {
		remaining := p.entries
		if emptyStage(stage, stat.Dev, 0, &remaining, stageOwner, stageLock) == nil {
			_ = p.root.Remove(name)
		}
		lock.Close()
		return prerequisites.Stage{}, unavailable
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return discard()
	}
	marker, err := stage.OpenFile(stageOwner, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err == nil {
		_, err = marker.WriteString(kind + " " + strconv.Itoa(os.Getpid()) + "\n")
		if closeErr := marker.Close(); err == nil {
			err = closeErr
		}
	}
	if err == nil {
		err = p.root.Chmod(name, 0711)
	}
	held, statErr := lock.Stat()
	if err != nil || statErr != nil {
		return discard()
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			p.release(name, lock, held)
		})
	}
	return prerequisites.Stage{Path: filepath.Join(p.path, name), Noexec: p.noexec, Release: release}, nil
}

// release removes the stage while its own lock is still held, so no sweep
// contends for it meanwhile, and only then lets the lock go. A stage it cannot
// remove whole is left for the next sweep.
func (p *stagingParent) release(name string, lock *os.File, held os.FileInfo) {
	defer lock.Close()
	root, err := os.OpenRoot(p.path)
	if err != nil {
		return
	}
	defer root.Close()
	reopened := &stagingParent{path: p.path, root: root, owner: p.owner, device: p.device, entries: p.entries}
	_, _ = reopened.reap(name, held)
}

type stageState int

const (
	stageSkipped stageState = iota
	stageHeld
	stageRemoved
)

// reap removes one stage when no other open file description holds its lock.
// held, when set, is the lock this invocation holds, and the stage's lock must
// still be that file. An entry that is not this identity's stage directory, or
// whose lock or marker is not a private regular file, is neither followed nor
// removed; neither is a stage without its marker.
func (p *stagingParent) reap(name string, held os.FileInfo) (stageState, error) {
	stage, stat, ok := p.directory(name)
	if !ok {
		return stageSkipped, nil
	}
	defer stage.Close()
	marker, _, ok := p.file(stage, stageOwner)
	if !ok {
		return stageSkipped, nil
	}
	marker.Close()
	lock, opened, ok := p.file(stage, stageLock)
	if !ok {
		return stageSkipped, nil
	}
	defer lock.Close()
	if held != nil {
		if !os.SameFile(held, opened) {
			return stageSkipped, nil
		}
	} else {
		switch err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); {
		case errors.Is(err, syscall.EWOULDBLOCK):
			return stageHeld, nil
		case err != nil:
			return stageSkipped, err
		}
	}
	remaining := p.entries
	if err := emptyStage(stage, stat.Dev, 0, &remaining, stageOwner, stageLock); err != nil {
		return stageSkipped, err
	}
	if err := p.root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return stageSkipped, err
	}
	return stageRemoved, nil
}

// directory opens one stage through the held parent: the entry listed without
// following a link must be the handle opened, a directory of this identity no
// other may write, on the parent's filesystem.
func (p *stagingParent) directory(name string) (*os.Root, *syscall.Stat_t, bool) {
	listed, err := p.root.Lstat(name)
	if err != nil || !listed.IsDir() {
		return nil, nil, false
	}
	stage, err := p.root.OpenRoot(name)
	if err != nil {
		return nil, nil, false
	}
	opened, err := stage.Stat(".")
	stat, ok := statOf(opened, err)
	if !ok || !opened.IsDir() || !os.SameFile(listed, opened) || stat.Uid != p.owner || stat.Mode&0022 != 0 || stat.Dev != p.device {
		stage.Close()
		return nil, nil, false
	}
	return stage, stat, true
}

// file opens a stage's lock or marker: a private regular file of this
// identity with one link, never through a link.
func (p *stagingParent) file(stage *os.Root, name string) (*os.File, os.FileInfo, bool) {
	listed, err := stage.Lstat(name)
	if err != nil || !listed.Mode().IsRegular() {
		return nil, nil, false
	}
	file, err := stage.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, false
	}
	opened, err := file.Stat()
	stat, ok := statOf(opened, err)
	if !ok || !opened.Mode().IsRegular() || stat.Nlink != 1 || stat.Uid != p.owner || stat.Mode&0077 != 0 || !os.SameFile(listed, opened) {
		file.Close()
		return nil, nil, false
	}
	return file, opened, true
}

func statOf(info os.FileInfo, err error) (*syscall.Stat_t, bool) {
	if err != nil {
		return nil, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return stat, ok
}

var stageSequence atomic.Uint64

// stageSuffix names a new stage. Only this identity may create an entry in the
// parent, so a name need only be unlikely to repeat, and the exclusive create
// refuses one that does.
func stageSuffix() string {
	seed := binary.BigEndian.AppendUint64(nil, uint64(os.Getpid()))
	seed = binary.BigEndian.AppendUint64(seed, uint64(time.Now().UnixNano()))
	seed = binary.BigEndian.AppendUint64(seed, stageSequence.Add(1))
	digest := sha256.Sum256(seed)
	return hex.EncodeToString(digest[:8])
}

// stageName reports a name Stage gives: a known kind and sixteen lowercase
// hexadecimal digits.
func stageName(name string) bool {
	kind, suffix, found := strings.Cut(name, "-")
	return found && slices.Contains(stageKinds, kind) && len(suffix) == 16 && strings.Trim(suffix, "0123456789abcdef") == ""
}

// emptyStage removes everything beneath one verified stage. A link is removed
// as itself and never followed, a directory is entered only through a handle
// that is the entry listed, and an entry on another filesystem refuses rather
// than being emptied. The last names go after every other entry, in order.
func emptyStage(dir *os.Root, device uint64, depth int, remaining *int, last ...string) error {
	if depth > maxStageTreeDepth {
		return errors.New("resolution stage depth bound")
	}
	names, err := stageEntries(dir, remaining)
	if err != nil {
		return err
	}
	slices.SortStableFunc(names, func(a, b string) int { return slices.Index(last, a) - slices.Index(last, b) })
	for _, name := range names {
		listed, err := dir.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if stat, ok := listed.Sys().(*syscall.Stat_t); !ok || stat.Dev != device {
			return errors.New("resolution stage crosses a filesystem")
		}
		if listed.IsDir() {
			if err := emptyStageChild(dir, name, listed, device, depth, remaining); err != nil {
				return err
			}
		}
		if err := dir.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func emptyStageChild(parent *os.Root, name string, listed os.FileInfo, device uint64, depth int, remaining *int) error {
	child, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	defer child.Close()
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(listed, opened) {
		return errors.New("resolution stage entry changed")
	}
	return emptyStage(child, device, depth+1, remaining)
}

func stageEntries(dir *os.Root, remaining *int) ([]string, error) {
	listing, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	defer listing.Close()
	var names []string
	for {
		part, err := listing.Readdirnames(1024)
		if *remaining -= len(part); *remaining < 0 {
			return nil, errors.New("resolution stage entry bound")
		}
		names = append(names, part...)
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
