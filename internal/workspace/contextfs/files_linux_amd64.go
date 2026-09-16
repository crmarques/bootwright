//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	openat2Trap                   = 437
	renameat2Trap                 = 316
	renameNoReplace               = 1
	pathHandle                    = 0x200000
	resolveBeneathNoLinksNoMounts = 0x08 | 0x04 | 0x02 | 0x01
)

type directory struct {
	file     *os.File
	path     string
	identity syscall.Stat_t
	parent   *directory
	name     string
}

func statHandle(file *os.File) (syscall.Stat_t, error) {
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
		return stat, state("state handle cannot be verified")
	}
	return stat, nil
}

func sameIdentity(a, b syscall.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid
}

func sameFile(a, b syscall.Stat_t) bool {
	return sameIdentity(a, b) && a.Size == b.Size && a.Nlink == b.Nlink && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func private(stat syscall.Stat_t, kind, uid, gid uint32) bool {
	mode := uint32(0600)
	if kind == syscall.S_IFDIR {
		mode = 0700
	}
	return stat.Mode&syscall.S_IFMT == kind && stat.Uid == uid && stat.Gid == gid && stat.Mode&07777 == mode && (kind != syscall.S_IFREG || stat.Nlink == 1)
}

func readableFile(stat syscall.Stat_t, immutable bool, parent *directory) bool {
	if !immutable && stat.Nlink == 0 {
		stat.Nlink = 1
	}
	return private(stat, syscall.S_IFREG, parent.identity.Uid, parent.identity.Gid)
}

func openRelative(parent *directory, name string, flags int, mode uint32) (*os.File, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return nil, state("state path component is invalid")
	}
	return openWithin(parent, name, flags, mode)
}

// resolutionRetries bounds how often a resolution is re-attempted. The race it
// answers is the width of one rename, so a handful of immediate attempts covers
// it and a wedged tree still fails rather than spinning.
const resolutionRetries = 16

// retryResolution re-attempts a resolution the kernel refused with EAGAIN.
// openat2 answers EAGAIN when a rename moved part of the path while it was
// being resolved under RESOLVE_BENEATH: the kernel is asking for a retry, not
// reporting an unsafe path. A caller that treats it as a refusal fails a whole
// operation because a writer happened to be a millisecond early.
func retryResolution(open func() (*os.File, error)) (*os.File, error) {
	for attempt := 0; ; attempt++ {
		file, err := open()
		if !errors.Is(err, syscall.EAGAIN) || attempt == resolutionRetries {
			return file, err
		}
	}
}

func openWithin(parent *directory, name string, flags int, mode uint32) (*os.File, error) {
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, state("state path component is invalid")
	}
	how := struct{ Flags, Mode, Resolve uint64 }{uint64(flags | syscall.O_CLOEXEC | syscall.O_NOFOLLOW), uint64(mode), resolveBeneathNoLinksNoMounts}
	return retryResolution(func() (*os.File, error) {
		fd, _, errno := syscall.Syscall6(openat2Trap, parent.file.Fd(), uintptr(unsafe.Pointer(pointer)), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
		runtime.KeepAlive(pointer)
		if errno != 0 {
			return nil, errno
		}
		return os.NewFile(fd, filepath.Join(parent.path, name)), nil
	})
}

func openDirectory(parent *directory, name string) (*directory, error) {
	file, err := openRelative(parent, name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	stat, err := statHandle(file)
	if err != nil || !private(stat, syscall.S_IFDIR, parent.identity.Uid, parent.identity.Gid) || stat.Dev != parent.identity.Dev {
		file.Close()
		return nil, state("state directory type, owner, permissions or device is unsafe")
	}
	return &directory{file: file, path: filepath.Join(parent.path, name), identity: stat, parent: parent, name: name}, nil
}

func (d *directory) verify() error {
	stat, err := statHandle(d.file)
	if err != nil || !private(stat, syscall.S_IFDIR, d.identity.Uid, d.identity.Gid) || !sameIdentity(d.identity, stat) {
		return state("held state directory changed")
	}
	var reopened *os.File
	if d.parent == nil {
		reopened, err = walkAbsolute(d.path, false)
	} else {
		if err = d.parent.verify(); err != nil {
			return err
		}
		reopened, err = openRelative(d.parent, d.name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	}
	if err != nil {
		return state("state directory location cannot be verified")
	}
	defer reopened.Close()
	current, err := statHandle(reopened)
	if err != nil || !sameIdentity(d.identity, current) {
		return state("state directory location was replaced")
	}
	return nil
}

// Existing ancestors may be system directories; all missing suffixes are
// exclusively created private directories. No component may be a symlink.
func walkAbsolute(path string, create bool) (*os.File, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(fd), "/")
	if path == "/" {
		return parent, nil
	}
	if create {
		probe, err := openWithin(&directory{file: parent, path: "/"}, ".", pathHandle|syscall.O_DIRECTORY, 0)
		if err != nil {
			parent.Close()
			return nil, state("state containment primitives are unavailable")
		}
		probe.Close()
		return createAbsolute(path, parent)
	}
	for _, name := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if err := qualifiedFileSystem(parent); err != nil {
			parent.Close()
			return nil, err
		}
		child, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		parent.Close()
		if err != nil {
			return nil, err
		}
		parent = os.NewFile(uintptr(child), name)
	}
	return parent, nil
}

func createAbsolute(path string, base *os.File) (*os.File, error) {
	type held struct {
		file     *os.File
		name     string
		identity syscall.Stat_t
	}
	initial, err := statHandle(base)
	if err != nil {
		base.Close()
		return nil, err
	}
	chain := []held{{file: base, identity: initial}}
	keep := false
	defer func() {
		for i, entry := range chain {
			if !keep || i != len(chain)-1 {
				entry.file.Close()
			}
		}
	}()
	verify := func() error {
		for i, entry := range chain {
			current, err := statHandle(entry.file)
			if err != nil || !sameIdentity(entry.identity, current) {
				return state("state ancestor changed during creation")
			}
			if i == 0 {
				continue
			}
			fd, err := syscall.Openat(int(chain[i-1].file.Fd()), entry.name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return state("state ancestor was replaced during creation")
			}
			var named syscall.Stat_t
			err = syscall.Fstat(fd, &named)
			syscall.Close(fd)
			if err != nil || !sameIdentity(entry.identity, named) {
				return state("state ancestor was replaced during creation")
			}
		}
		return nil
	}
	for _, name := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if err := verify(); err != nil {
			return nil, err
		}
		parent := chain[len(chain)-1].file
		if err := qualifiedFileSystem(parent); err != nil {
			return nil, err
		}
		fd, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		created := false
		if errors.Is(err, syscall.ENOENT) {
			if err := verify(); err != nil {
				return nil, err
			}
			err = syscall.Mkdirat(int(parent.Fd()), name, 0700)
			if err != nil {
				return nil, err
			}
			created = true
			fd, err = syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		}
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(fd), name)
		if err := qualifiedFileSystem(file); err != nil {
			file.Close()
			return nil, err
		}
		identity, err := statHandle(file)
		if err != nil || created && (!private(identity, syscall.S_IFDIR, uint32(os.Geteuid()), uint32(os.Getegid())) || identity.Mode&0777 != 0700) {
			file.Close()
			return nil, state("created state ancestor is unsafe")
		}
		chain = append(chain, held{file: file, name: name, identity: identity})
		if err := verify(); err != nil {
			return nil, err
		}
		if created {
			if err := parent.Sync(); err != nil {
				return nil, state("state ancestor durability could not be established")
			}
		}
	}
	keep = true
	return chain[len(chain)-1].file, nil
}

func (s *Store) rootPath() (string, error) {
	path := s.options.Root
	if path == "" {
		path = "/var/lib/bootwright"
	}
	if !filepath.IsAbs(path) || len(path) > maxPath || strings.ContainsRune(path, 0) {
		return "", state("state root must be an absolute bounded path")
	}
	path = filepath.Clean(path)
	if !canonicalPath(path) {
		return "", state("state root is not canonical")
	}
	return path, nil
}

func (s *Store) openRoot(ctx context.Context, create bool, inputs []string) (*directory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.rootPath()
	if err != nil {
		return nil, err
	}
	uid, gid := uint32(0), uint32(0)
	if s.options.Owner != nil {
		if s.options.Root == "" {
			return nil, state("test ownership requires an isolated explicit root")
		}
		uid, gid = s.options.Owner.UID, s.options.Owner.GID
	}
	if uint32(os.Geteuid()) != uid || uint32(os.Getegid()) != gid {
		return nil, state("context storage requires root privileges")
	}
	for _, input := range inputs {
		if !canonicalPath(input) || beneath(input, path) {
			return nil, state("state root overlaps admitted input")
		}
	}
	file, err := walkAbsolute(path, create)
	if err != nil {
		return nil, err
	}
	stat, err := statHandle(file)
	if err != nil || !private(stat, syscall.S_IFDIR, uid, gid) {
		file.Close()
		return nil, state("state root type, owner or permissions is unsafe")
	}
	var fs syscall.Statfs_t
	if err = syscall.Fstatfs(int(file.Fd()), &fs); err != nil || !localFilesystem(fs.Type) {
		file.Close()
		return nil, state("state filesystem is not qualified for private durable storage")
	}
	root := &directory{file: file, path: path, identity: stat}
	// Probe the required kernel primitive without creating anything.
	probe, err := openRelative(root, "registry.json", pathHandle, 0)
	if probe != nil {
		probe.Close()
	}
	if err != nil && !errors.Is(err, syscall.ENOENT) {
		file.Close()
		return nil, state("state containment primitives are unavailable or the registry path is unsafe")
	}
	return root, nil
}

func localFilesystem(kind int64) bool {
	switch kind {
	case 0xef53, 0x58465342, 0x9123683e, 0x01021994, 0x794c7630:
		return true
	}
	return false
}

func qualifiedFileSystem(file *os.File) error {
	var fs syscall.Statfs_t
	if err := syscall.Fstatfs(int(file.Fd()), &fs); err != nil || !localFilesystem(fs.Type) {
		return state("state filesystem is not qualified for private durable storage")
	}
	return nil
}

func readBounded(ctx context.Context, parent *directory, name string, maximum int, immutable bool) ([]byte, error) {
	data, _, err := readBoundedIdentity(ctx, parent, name, maximum, immutable)
	return data, err
}

func readBoundedIdentity(ctx context.Context, parent *directory, name string, maximum int, immutable bool) ([]byte, syscall.Stat_t, error) {
	if err := ctx.Err(); err != nil {
		return nil, syscall.Stat_t{}, err
	}
	file, err := openRelative(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, syscall.Stat_t{}, err
	}
	defer file.Close()
	before, err := statHandle(file)
	if err != nil || !readableFile(before, immutable, parent) || before.Size < 0 || before.Size > int64(maximum) {
		return nil, syscall.Stat_t{}, state("state file type, owner, permissions, links or size is unsafe")
	}
	data := make([]byte, 0, int(before.Size))
	buffer := make([]byte, min(32768, maximum+1))
	for len(data) <= maximum {
		if err := ctx.Err(); err != nil {
			return nil, syscall.Stat_t{}, err
		}
		n, readErr := file.Read(buffer[:min(len(buffer), maximum+1-len(data))])
		data = append(data, buffer[:n]...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, syscall.Stat_t{}, state("state file could not be read")
		}
		if len(data) > maximum {
			return nil, syscall.Stat_t{}, state("state file exceeds its byte limit")
		}
	}
	after, err := statHandle(file)
	stable := sameFile(before, after)
	if !immutable {
		// A registry replacement may unlink this complete, already-open old
		// snapshot. Its contents remain immutable even though ctime/nlink change.
		stable = sameIdentity(before, after) && before.Size == after.Size && before.Mtim == after.Mtim && readableFile(after, false, parent)
	}
	if err != nil || !stable || int64(len(data)) != after.Size {
		return nil, syscall.Stat_t{}, state("state file changed during reading")
	}
	if immutable {
		current, err := openRelative(parent, name, pathHandle, 0)
		if err != nil {
			return nil, syscall.Stat_t{}, state("immutable state file was replaced")
		}
		stat, statErr := statHandle(current)
		current.Close()
		if statErr != nil || !sameFile(after, stat) {
			return nil, syscall.Stat_t{}, state("immutable state file was replaced")
		}
	}
	if err := parent.verify(); err != nil {
		return nil, syscall.Stat_t{}, err
	}
	return data, after, nil
}

func (s *Store) newDirectory(ctx context.Context, parent *directory, name string) (*directory, error) {
	if err := s.checkpoint(ctx, "mkdir"); err != nil {
		return nil, err
	}
	if err := parent.verify(); err != nil {
		return nil, err
	}
	if err := syscall.Mkdirat(int(parent.file.Fd()), name, 0700); err != nil {
		return nil, err
	}
	child, err := openDirectory(parent, name)
	if err != nil {
		return nil, err
	}
	if child.identity.Mode&0777 != 0700 {
		child.file.Close()
		return nil, state("new state directory does not have private creation permissions")
	}
	if err = s.syncDirectory(ctx, parent); err != nil {
		child.file.Close()
		return nil, err
	}
	return child, nil
}

func (s *Store) ensureDirectory(ctx context.Context, parent *directory, name string) (*directory, error) {
	child, err := openDirectory(parent, name)
	if !errors.Is(err, syscall.ENOENT) {
		return child, err
	}
	return s.newDirectory(ctx, parent, name)
}

func (s *Store) syncDirectory(ctx context.Context, dir *directory) error {
	if err := s.checkpoint(ctx, "sync-directory"); err != nil {
		return err
	}
	if err := dir.verify(); err != nil {
		return err
	}
	if err := dir.file.Sync(); err != nil {
		return state("state directory durability could not be established")
	}
	return nil
}

func (s *Store) writeExclusive(ctx context.Context, parent *directory, name string, data []byte) error {
	if err := s.checkpoint(ctx, "create-file"); err != nil {
		return err
	}
	if err := parent.verify(); err != nil {
		return err
	}
	file, err := openRelative(parent, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	before, err := statHandle(file)
	if err != nil || !private(before, syscall.S_IFREG, parent.identity.Uid, parent.identity.Gid) || before.Mode&0777 != 0600 {
		return state("new state file is unsafe")
	}
	size := len(data)
	for len(data) > 0 {
		if err := s.checkpoint(ctx, "write-file"); err != nil {
			return err
		}
		n, err := file.Write(data[:min(len(data), 32768)])
		if err != nil || n == 0 {
			return state("state file could not be written")
		}
		data = data[n:]
	}
	if err := s.checkpoint(ctx, "sync-file"); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return state("state file durability could not be established")
	}
	held, err := statHandle(file)
	if err != nil || !sameIdentity(before, held) || !private(held, syscall.S_IFREG, parent.identity.Uid, parent.identity.Gid) || held.Size != int64(size) {
		return state("new state file changed during publication")
	}
	current, err := openRelative(parent, name, pathHandle, 0)
	if err != nil {
		return state("new state file was replaced")
	}
	after, statErr := statHandle(current)
	current.Close()
	if statErr != nil || !sameFile(held, after) {
		return state("new state file was replaced")
	}
	return s.syncDirectory(ctx, parent)
}

func (s *Store) writeExclusiveAtomic(ctx context.Context, parent *directory, name string, data []byte) error {
	var pending string
	written := false
	for range 16 {
		candidate, err := s.candidate("pending-")
		if err != nil {
			return err
		}
		pending = candidate
		err = s.writeExclusive(ctx, parent, pending, data)
		if errors.Is(err, syscall.EEXIST) {
			continue
		}
		if err != nil {
			return err
		}
		written = true
		break
	}
	if !written {
		return state("immutable state staging exhausted its collision limit")
	}
	staged, stagedIdentity, err := readBoundedIdentity(ctx, parent, pending, len(data), true)
	stagedMatches := bytes.Equal(staged, data)
	clear(staged)
	if err != nil || !stagedMatches {
		return state("staged immutable state changed before publication")
	}
	if err := s.checkpoint(ctx, "before-secret-immutable-rename"); err != nil {
		return err
	}
	current, currentIdentity, err := readBoundedIdentity(ctx, parent, pending, len(data), true)
	currentMatches := bytes.Equal(current, data)
	clear(current)
	if err != nil || !currentMatches || !sameFile(stagedIdentity, currentIdentity) {
		return state("staged immutable state changed before publication")
	}
	if err := renameNoReplaceAt(parent, pending, name); err != nil {
		return err
	}
	published, publishedIdentity, err := readBoundedIdentity(ctx, parent, name, len(data), true)
	publishedMatches := bytes.Equal(published, data)
	clear(published)
	if err != nil || !publishedMatches || !sameIdentity(stagedIdentity, publishedIdentity) {
		return state("published immutable state is unsafe")
	}
	if err := s.checkpoint(ctx, "after-secret-immutable-rename"); err != nil {
		return err
	}
	return s.syncDirectory(ctx, parent)
}

func renameNoReplaceAt(parent *directory, oldName, newName string) error {
	if err := parent.verify(); err != nil {
		return err
	}
	if len(oldName) == 0 || len(oldName) > 255 || oldName == "." || oldName == ".." || strings.ContainsAny(oldName, "/\x00") {
		return state("staged immutable state name is invalid")
	}
	if len(newName) == 0 || len(newName) > 255 || newName == "." || newName == ".." || strings.ContainsAny(newName, "/\x00") {
		return state("immutable state name is invalid")
	}
	oldPointer, err := syscall.BytePtrFromString(oldName)
	if err != nil {
		return state("staged immutable state name is invalid")
	}
	newPointer, err := syscall.BytePtrFromString(newName)
	if err != nil {
		return state("immutable state name is invalid")
	}
	_, _, errno := syscall.Syscall6(
		renameat2Trap,
		parent.file.Fd(), uintptr(unsafe.Pointer(oldPointer)),
		parent.file.Fd(), uintptr(unsafe.Pointer(newPointer)),
		renameNoReplace, 0,
	)
	runtime.KeepAlive(oldPointer)
	runtime.KeepAlive(newPointer)
	if errno != 0 {
		return errno
	}
	// Publication is complete once renameat2 succeeds. Callers verify the
	// published entry and parent before reporting success.
	return nil
}

func lock(dir *directory) error {
	if err := dir.verify(); err != nil {
		return err
	}
	if err := syscall.Flock(int(dir.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return state("context state is held by another mutator")
	}
	return dir.verify()
}

func lockShared(dir *directory) error {
	if err := dir.verify(); err != nil {
		return err
	}
	if err := syscall.Flock(int(dir.file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return state("context storage is busy")
	}
	return nil
}
