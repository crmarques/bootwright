//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	openat2Trap                   = 437
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

func private(stat syscall.Stat_t, kind uint32) bool {
	return stat.Mode&syscall.S_IFMT == kind && stat.Uid == uint32(os.Getuid()) && stat.Mode&0077 == 0 && stat.Mode&07000 == 0 && (kind != syscall.S_IFREG || stat.Nlink == 1)
}

func readableFile(stat syscall.Stat_t, immutable bool) bool {
	if !immutable && stat.Nlink == 0 {
		stat.Nlink = 1
	}
	return private(stat, syscall.S_IFREG)
}

func openRelative(parent *directory, name string, flags int, mode uint32) (*os.File, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return nil, state("state path component is invalid")
	}
	return openWithin(parent, name, flags, mode)
}

func openWithin(parent *directory, name string, flags int, mode uint32) (*os.File, error) {
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, state("state path component is invalid")
	}
	how := struct{ Flags, Mode, Resolve uint64 }{uint64(flags | syscall.O_CLOEXEC | syscall.O_NOFOLLOW), uint64(mode), resolveBeneathNoLinksNoMounts}
	fd, _, errno := syscall.Syscall6(openat2Trap, parent.file.Fd(), uintptr(unsafe.Pointer(pointer)), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
	runtime.KeepAlive(pointer)
	if errno != 0 {
		return nil, errno
	}
	return os.NewFile(fd, filepath.Join(parent.path, name)), nil
}

func openDirectory(parent *directory, name string) (*directory, error) {
	file, err := openRelative(parent, name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	stat, err := statHandle(file)
	if err != nil || !private(stat, syscall.S_IFDIR) || stat.Dev != parent.identity.Dev {
		file.Close()
		return nil, state("state directory type, owner, permissions or device is unsafe")
	}
	return &directory{file: file, path: filepath.Join(parent.path, name), identity: stat, parent: parent, name: name}, nil
}

func (d *directory) verify() error {
	stat, err := statHandle(d.file)
	if err != nil || !private(stat, syscall.S_IFDIR) || !sameIdentity(d.identity, stat) {
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
		if err != nil || created && (!private(identity, syscall.S_IFDIR) || identity.Mode&0777 != 0700) {
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
		if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" && filepath.IsAbs(xdg) {
			path = filepath.Join(xdg, "bootwright")
		} else {
			home, err := accountHome()
			if err != nil {
				return "", err
			}
			path = filepath.Join(home, ".local", "state", "bootwright")
		}
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

func accountHome() (string, error) {
	fd, err := syscall.Open("/etc/passwd", syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", state("local account home is unavailable; select an absolute XDG_STATE_HOME")
	}
	file := os.NewFile(uintptr(fd), "/etc/passwd")
	defer file.Close()
	before, err := statHandle(file)
	if err != nil || before.Mode&syscall.S_IFMT != syscall.S_IFREG || before.Size < 0 || before.Size > 1<<20 {
		return "", state("local account database cannot be verified")
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	after, statErr := statHandle(file)
	if err != nil || statErr != nil || len(data) > 1<<20 || !sameFile(before, after) {
		return "", state("local account database cannot be read safely")
	}
	uid := strconv.Itoa(os.Getuid())
	home := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 7 || fields[2] != uid {
			continue
		}
		if home != "" || !canonicalPath(fields[5]) {
			return "", state("local account home is ambiguous; select an absolute XDG_STATE_HOME")
		}
		home = fields[5]
	}
	if home == "" {
		return "", state("local account home is unavailable; select an absolute XDG_STATE_HOME")
	}
	return home, nil
}

func (s *Store) openRoot(ctx context.Context, create bool, inputs []string) (*directory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.rootPath()
	if err != nil {
		return nil, err
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
	if err != nil || !private(stat, syscall.S_IFDIR) {
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := openRelative(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := statHandle(file)
	if err != nil || !readableFile(before, immutable) || before.Size < 0 || before.Size > int64(maximum) {
		return nil, state("state file type, owner, permissions, links or size is unsafe")
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
		if readErr != nil {
			return nil, state("state file could not be read")
		}
		if len(data) > maximum {
			return nil, state("state file exceeds its byte limit")
		}
	}
	after, err := statHandle(file)
	stable := sameFile(before, after)
	if !immutable {
		// A registry replacement may unlink this complete, already-open old
		// snapshot. Its contents remain immutable even though ctime/nlink change.
		stable = sameIdentity(before, after) && before.Size == after.Size && before.Mtim == after.Mtim && readableFile(after, false)
	}
	if err != nil || !stable || int64(len(data)) != after.Size {
		return nil, state("state file changed during reading")
	}
	if immutable {
		current, err := openRelative(parent, name, pathHandle, 0)
		if err != nil {
			return nil, state("immutable state file was replaced")
		}
		stat, statErr := statHandle(current)
		current.Close()
		if statErr != nil || !sameFile(after, stat) {
			return nil, state("immutable state file was replaced")
		}
	}
	if err := parent.verify(); err != nil {
		return nil, err
	}
	return data, nil
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
	if err != nil || !private(before, syscall.S_IFREG) || before.Mode&0777 != 0600 {
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
	if err != nil || !sameIdentity(before, held) || !private(held, syscall.S_IFREG) || held.Size != int64(size) {
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

func lock(dir *directory) error {
	if err := dir.verify(); err != nil {
		return err
	}
	if err := syscall.Flock(int(dir.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return state("context state is held by another mutator")
	}
	return dir.verify()
}
