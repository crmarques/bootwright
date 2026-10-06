//go:build linux && amd64

package invokerfs

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strconv"
	"sync"
	"syscall"
)

const (
	pathOnly  = 0x200000
	maxName   = 255
	maxPath   = 4096
	maxIssued = 4096
	maxID     = 1<<32 - 2
)

// openAtFlags is everything a caller may ask of OpenAt: a path-only or
// read-only handle, optionally of a directory, never one that blocks.
const openAtFlags = pathOnly | syscall.O_DIRECTORY | syscall.O_NONBLOCK

var (
	errIdentity = errors.New("the invoking account identity is unusable")
	errHelper   = errors.New("the invoking account's file helper failed")
)

type openError struct {
	errno syscall.Errno
	root  bool
}

func (e *openError) Error() string { return "open: " + e.errno.Error() }

func (e *openError) Unwrap() error { return e.errno }

// DeniedToRoot reports a permission denial of an open that ran with root's
// credentials, which only a direct root session performs.
func (e *openError) DeniedToRoot() bool {
	return e.root && (e.errno == syscall.EACCES || e.errno == syscall.EPERM)
}

type link interface {
	open(request, *os.File) (*os.File, error)
	close() error
}

// Session issues descriptors under one credential set. It serializes its
// calls and admits as a parent only a directory descriptor it issued itself.
type Session struct {
	mu     sync.Mutex
	link   link
	issued map[*os.File]struct{}
	closed bool
}

func newSession(l link) *Session {
	return &Session{link: l, issued: map[*os.File]struct{}{}}
}

// Begin opens in-process when this process already runs as the account that
// named the paths, and otherwise starts one bounded helper under the invoking
// account. The session holds ctx until Close; its end kills the helper.
func (o *Opener) Begin(ctx context.Context) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o == nil || os.Geteuid() != 0 {
		return newSession(direct{root: os.Geteuid() == 0}), nil
	}
	if o.account == nil {
		return nil, errIdentity
	}
	account, err := o.account(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	switch {
	case account.UID == 0:
		return newSession(direct{root: true}), nil
	case account.UID > 0 && account.UID <= maxID && account.GID >= 0 && account.GID <= maxID:
		helper, err := spawn(ctx, account, production)
		if err != nil {
			return nil, err
		}
		return newSession(helper), nil
	}
	return nil, errIdentity
}

// Root opens "/" read-only as a directory.
func (s *Session) Root() (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.perform(request{op: opRoot}, nil)
}

// OpenAt opens one name beneath a directory this session issued, never
// following a link at that name.
func (s *Session) OpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	r := request{op: opOpenAt, name: name, flags: flags}
	if !r.valid() {
		return nil, &openError{errno: syscall.EINVAL}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, issued := s.issued[parent]; !issued || parent.Fd() == ^uintptr(0) {
		return nil, &openError{errno: syscall.EINVAL}
	}
	return s.perform(r, parent)
}

// OpenFile opens one absolute path without following a link at its final
// component. A regular file comes back open for reading; anything else comes
// back as a path-only descriptor, so no open routine of it runs.
func (s *Session) OpenFile(path string) (*os.File, error) {
	if len(path) > maxPath {
		return nil, &openError{errno: syscall.ENAMETOOLONG}
	}
	r := request{op: opOpenFile, name: path}
	if !r.valid() {
		return nil, &openError{errno: syscall.EINVAL}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.perform(r, nil)
}

// Close ends the session; a helper it started is reaped before Close returns.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.issued = nil
	return s.link.close()
}

func (s *Session) perform(r request, parent *os.File) (*os.File, error) {
	if s.closed {
		return nil, os.ErrClosed
	}
	file, err := s.link.open(r, parent)
	if err != nil {
		return nil, err
	}
	if err := s.remember(file); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func (s *Session) remember(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if len(s.issued) >= maxIssued {
		for issued := range s.issued {
			if issued.Fd() == ^uintptr(0) {
				delete(s.issued, issued)
			}
		}
	}
	if len(s.issued) >= maxIssued {
		return &openError{errno: syscall.EMFILE}
	}
	s.issued[file] = struct{}{}
	return nil
}

type direct struct{ root bool }

func (d direct) open(r request, parent *os.File) (*os.File, error) {
	descriptor := -1
	if parent != nil {
		descriptor = int(parent.Fd())
	}
	fd, errno := r.perform(descriptor)
	runtime.KeepAlive(parent)
	if errno != 0 {
		return nil, &openError{errno: errno, root: d.root}
	}
	return os.NewFile(uintptr(fd), r.label()), nil
}

func (direct) close() error { return nil }

func openRoot() (int, syscall.Errno) {
	return open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC)
}

func openAt(parent int, name string, flags int) (int, syscall.Errno) {
	for {
		fd, err := syscall.Openat(parent, name, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != syscall.EINTR {
			return fd, errnoOf(err)
		}
	}
}

// openFile proves the named object's type through a path-only handle before
// anything opens it, and reopens only a regular file, through that handle, for
// reading.
func openFile(path string) (int, syscall.Errno) {
	handle, errno := open(path, pathOnly|syscall.O_NOFOLLOW|syscall.O_CLOEXEC)
	if errno != 0 {
		return -1, errno
	}
	var named syscall.Stat_t
	if err := syscall.Fstat(handle, &named); err != nil {
		syscall.Close(handle)
		return -1, errnoOf(err)
	}
	if named.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return handle, 0
	}
	fd, errno := open("/proc/self/fd/"+strconv.Itoa(handle), syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC)
	syscall.Close(handle)
	if errno != 0 {
		return -1, errno
	}
	var opened syscall.Stat_t
	if err := syscall.Fstat(fd, &opened); err != nil || opened.Dev != named.Dev || opened.Ino != named.Ino {
		syscall.Close(fd)
		return -1, syscall.EIO
	}
	return fd, 0
}

func open(path string, flags int) (int, syscall.Errno) {
	for {
		fd, err := syscall.Open(path, flags, 0)
		if err != syscall.EINTR {
			return fd, errnoOf(err)
		}
	}
}

func errnoOf(err error) syscall.Errno {
	if err == nil {
		return 0
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 {
		return errno
	}
	return syscall.EIO
}
