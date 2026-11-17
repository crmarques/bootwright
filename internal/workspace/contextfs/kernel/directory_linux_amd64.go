//go:build linux && amd64

package kernel

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/nativefs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func state(message string) error { return contexts.StateError(message) }

const PathHandle = nativefs.PathOnly

type Directory struct {
	File       *os.File
	Path       string
	Identity   syscall.Stat_t
	Parent     *Directory
	Name       string
	OwnsParent bool
}

func (d *Directory) Close() {
	UnlockAndClose(d.File)
	if d.OwnsParent {
		UnlockAndClose(d.Parent.File)
	}
}

func StatHandle(file *os.File) (syscall.Stat_t, error) {
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
		return stat, &IOFailure{Failure: state("state handle cannot be verified")}
	}
	return stat, nil
}

func SameIdentity(a, b syscall.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid
}

func SameFile(a, b syscall.Stat_t) bool {
	return SameIdentity(a, b) && a.Size == b.Size && a.Nlink == b.Nlink && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func Private(stat syscall.Stat_t, kind, uid, gid uint32) bool {
	mode := uint32(0600)
	if kind == syscall.S_IFDIR {
		mode = 0700
	}
	return stat.Mode&syscall.S_IFMT == kind && stat.Uid == uid && stat.Gid == gid && stat.Mode&07777 == mode && (kind != syscall.S_IFREG || stat.Nlink == 1)
}

func ReadableFile(stat syscall.Stat_t, immutable bool, parent *Directory) bool {
	if !immutable && stat.Nlink == 0 {
		stat.Nlink = 1
	}
	return Private(stat, syscall.S_IFREG, parent.Identity.Uid, parent.Identity.Gid)
}

func OpenRelative(parent *Directory, name string, flags int, mode uint32) (*os.File, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") {
		return nil, state("state path component is invalid")
	}
	return OpenWithin(parent, name, flags, mode)
}

func OpenWithin(parent *Directory, name string, flags int, mode uint32) (*os.File, error) {
	if strings.ContainsRune(name, 0) {
		return nil, state("state path component is invalid")
	}
	fd, err := nativefs.OpenBeneath(int(parent.File.Fd()), name, flags, mode)
	runtime.KeepAlive(parent.File)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), filepath.Join(parent.Path, name)), nil
}

func OpenDirectory(parent *Directory, name string) (*Directory, error) {
	file, err := OpenRelative(parent, name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	stat, err := StatHandle(file)
	if err != nil {
		file.Close()
		return nil, err
	}
	if !Private(stat, syscall.S_IFDIR, parent.Identity.Uid, parent.Identity.Gid) || stat.Dev != parent.Identity.Dev {
		file.Close()
		return nil, state("state directory type, owner, permissions or device is unsafe")
	}
	return &Directory{File: file, Path: filepath.Join(parent.Path, name), Identity: stat, Parent: parent, Name: name}, nil
}

func RecordedDirectory(child, parent syscall.Stat_t, inode uint64) bool {
	return inode != 0 && child.Ino == inode && child.Dev == parent.Dev
}

func (d *Directory) Recorded(inode uint64) bool {
	return d.Parent != nil && RecordedDirectory(d.Identity, d.Parent.Identity, inode)
}

func (d *Directory) Verify() error {
	stat, err := StatHandle(d.File)
	if err != nil {
		return err
	}
	if !Private(stat, syscall.S_IFDIR, d.Identity.Uid, d.Identity.Gid) || !SameIdentity(d.Identity, stat) {
		return state("held state directory changed")
	}
	var reopened *os.File
	if d.Parent == nil {
		reopened, err = WalkAbsolute(d.Path, nil)
	} else {
		if err = d.Parent.Verify(); err != nil {
			return err
		}
		reopened, err = OpenRelative(d.Parent, d.Name, syscall.O_RDONLY|syscall.O_DIRECTORY, 0)
	}
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.EXDEV) {
			return state("state directory location cannot be verified")
		}
		return err
	}
	defer reopened.Close()
	current, err := StatHandle(reopened)
	if err != nil {
		return err
	}
	if !SameIdentity(d.Identity, current) {
		return state("state directory location was replaced")
	}
	return nil
}

// IOFailure is a store write or sync the kernel refused. Its diagnostic is
// context.state for every caller; its type lets the secret area report the
// same cause and remedy under its own code.
type IOFailure struct{ Failure error }

func (e *IOFailure) Error() string { return e.Failure.Error() }

func (e *IOFailure) Unwrap() error { return e.Failure }

func FileKind(mode uint32) string {
	switch mode & syscall.S_IFMT {
	case syscall.S_IFDIR:
		return "a directory"
	case syscall.S_IFREG:
		return "a regular file"
	case syscall.S_IFLNK:
		return "a symbolic link"
	}
	return "a special file"
}
