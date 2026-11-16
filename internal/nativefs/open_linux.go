//go:build linux

package nativefs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const PathOnly = unix.O_PATH

const resolutionRetries = 16

const beneath = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV

var ErrInvalidPath = errors.New("path is not absolute, clean and free of NUL bytes")

func OpenRoot() (*os.File, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "/"), nil
}

func OpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	if name != "." && !component(name) {
		return nil, ErrInvalidName
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

type ProcessFiles struct{}

func (ProcessFiles) Root() (*os.File, error) { return OpenRoot() }

func (ProcessFiles) OpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	return OpenAt(parent, name, flags)
}

func (ProcessFiles) Close() error { return nil }

func OpenNoFollow(path string, flags int) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
		return nil, ErrInvalidPath
	}
	how := unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW), Resolve: unix.RESOLVE_NO_SYMLINKS}
	fd, err := retry(func() (int, error) { return unix.Openat2(unix.AT_FDCWD, path, &how) })
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func OpenBeneath(dirfd int, name string, flags int, mode uint32) (int, error) {
	if strings.ContainsRune(name, 0) {
		return -1, ErrInvalidName
	}
	how := unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW), Mode: uint64(mode), Resolve: beneath}
	return retry(func() (int, error) { return unix.Openat2(dirfd, name, &how) })
}

func retry(open func() (int, error)) (int, error) {
	for attempt := 0; ; attempt++ {
		fd, err := open()
		if !errors.Is(err, unix.EAGAIN) || attempt == resolutionRetries {
			return fd, err
		}
	}
}

func RenameNoReplace(dirfd int, oldName, newName string) error {
	if !component(oldName) || !component(newName) {
		return ErrInvalidName
	}
	return unix.Renameat2(dirfd, oldName, dirfd, newName, unix.RENAME_NOREPLACE)
}

func Unlink(dirfd int, name string, directory bool) error {
	if !component(name) {
		return ErrInvalidName
	}
	flags := 0
	if directory {
		flags = unix.AT_REMOVEDIR
	}
	return unix.Unlinkat(dirfd, name, flags)
}

func Symlink(target string, dirfd int, name string) error {
	if !component(name) || target == "" || strings.ContainsRune(target, 0) {
		return ErrInvalidName
	}
	return unix.Symlinkat(target, dirfd, name)
}

func SyncFilesystem(fd int) error { return unix.Syncfs(fd) }
