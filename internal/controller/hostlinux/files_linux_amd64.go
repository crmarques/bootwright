//go:build linux && amd64

package hostlinux

import (
	"context"
	"io"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// filesystemView is an unexported construction seam for synthetic filesystem
// tests. Production construction fixes root/owner/architecture and has no hooks.
type filesystemView struct {
	root         string
	owner        uint32
	architecture string
	stat         func(string, *unix.Stat_t)
	filesystem   func(string, int64) int64
}

type heldFilesystem struct {
	view           filesystemView
	root           *os.File
	qualifiedLinks map[string]string
}

func (v filesystemView) open(ctx context.Context) (*heldFilesystem, error) {
	if ctx.Err() != nil || v.root == "" {
		return nil, errEvidence
	}
	fd, err := unix.Open(v.root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errEvidence
	}
	root := os.NewFile(uintptr(fd), v.root)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != v.owner || stat.Mode&0022 != 0 {
		root.Close()
		return nil, errEvidence
	}
	return &heldFilesystem{view: v, root: root}, nil
}

func (f *heldFilesystem) close() { f.root.Close() }

// openPath follows only root-owned symlinks through held, trusted directories.
// Every directory and final read handle is checked; no path is resolved and
// subsequently reopened through an unheld absolute pathname.
func (f *heldFilesystem) openPath(ctx context.Context, name string, readable bool) (*os.File, unix.Stat_t, error) {
	return f.openPathPolicy(ctx, name, readable, true)
}

func (f *heldFilesystem) openPathPolicy(ctx context.Context, name string, readable, followFinal bool) (*os.File, unix.Stat_t, error) {
	if !strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.ContainsRune(name, 0) || len(name) > 4096 || name == "/" {
		return nil, unix.Stat_t{}, errEvidence
	}
	pending := strings.Split(strings.TrimPrefix(name, "/"), "/")
	resolved := []string{}
	parent, err := unix.Dup(int(f.root.Fd()))
	if err != nil {
		return nil, unix.Stat_t{}, errEvidence
	}
	defer func() { unix.Close(parent) }()
	links := 0
	for len(pending) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, unix.Stat_t{}, err
		}
		part := pending[0]
		pending = pending[1:]
		fd, err := unix.Openat(parent, part, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, unix.Stat_t{}, err
		}
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil {
			unix.Close(fd)
			return nil, unix.Stat_t{}, errEvidence
		}
		current := "/" + strings.Join(append(append([]string{}, resolved...), part), "/")
		if f.view.stat != nil {
			f.view.stat(current, &stat)
		}
		if stat.Uid != f.view.owner {
			unix.Close(fd)
			return nil, unix.Stat_t{}, errEvidence
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
			if len(pending) == 0 && !followFinal {
				if !readable {
					return os.NewFile(uintptr(fd), name), stat, nil
				}
				unix.Close(fd)
				return nil, unix.Stat_t{}, errEvidence
			}
			links++
			target, err := f.linkTarget(fd, current, resolved, links)
			if err != nil {
				return nil, unix.Stat_t{}, err
			}
			pending = append(strings.Split(strings.TrimPrefix(target, "/"), "/"), pending...)
			resolved = nil
			unix.Close(parent)
			parent, err = unix.Dup(int(f.root.Fd()))
			if err != nil {
				return nil, unix.Stat_t{}, errEvidence
			}
			continue
		}
		if len(pending) != 0 {
			if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0022 != 0 {
				unix.Close(fd)
				return nil, unix.Stat_t{}, errEvidence
			}
			unix.Close(parent)
			parent = fd
			resolved = append(resolved, part)
			continue
		}
		if !readable {
			return os.NewFile(uintptr(fd), name), stat, nil
		}
		return f.openReadable(parent, fd, part, name, stat)
	}
	return nil, unix.Stat_t{}, errEvidence
}

func (f *heldFilesystem) linkTarget(fd int, current string, resolved []string, links int) (string, error) {
	var filesystem unix.Statfs_t
	if unix.Fstatfs(fd, &filesystem) != nil || filesystem.Type == unix.PROC_SUPER_MAGIC {
		unix.Close(fd)
		return "", errEvidence
	}
	buffer := make([]byte, 4097)
	// AT_EMPTY_PATH reads this held symlink, so replacement cannot change
	// its target between metadata verification and link acquisition.
	n, linkErr := unix.Readlinkat(fd, "", buffer)
	unix.Close(fd)
	if linkErr != nil || n == 0 || n > 4096 || links > 16 {
		return "", errEvidence
	}
	target := string(buffer[:n])
	if f.qualifiedLinks != nil && f.qualifiedLinks[current] != target {
		return "", errEvidence
	}
	if !strings.HasPrefix(target, "/") {
		target = "/" + strings.Join(resolved, "/") + "/" + target
	}
	target = path.Clean(target)
	if target == "/" {
		return "", errEvidence
	}
	return target, nil
}

func (f *heldFilesystem) openReadable(parent, fd int, part, name string, stat unix.Stat_t) (*os.File, unix.Stat_t, error) {
	if !f.regular(stat) {
		unix.Close(fd)
		return nil, unix.Stat_t{}, errEvidence
	}
	readFD, readErr := unix.Openat(parent, part, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NOATIME, 0)
	if readErr == unix.EPERM {
		// An unprivileged platform read may lack CAP_FOWNER for O_NOATIME.
		readFD, readErr = unix.Openat(parent, part, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
	unix.Close(fd)
	if readErr != nil {
		return nil, unix.Stat_t{}, readErr
	}
	var opened unix.Stat_t
	if unix.Fstat(readFD, &opened) != nil || !stable(stat, opened) {
		unix.Close(readFD)
		return nil, unix.Stat_t{}, errEvidence
	}
	return os.NewFile(uintptr(readFD), name), opened, nil
}

func (f *heldFilesystem) regular(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Uid == f.view.owner && stat.Mode&0022 == 0 && stat.Nlink == 1
}

func (f *heldFilesystem) read(ctx context.Context, name string, maximum int64) ([]byte, error) {
	file, before, err := f.openPath(ctx, name, true)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if before.Size < 0 || before.Size > maximum && f.filesystemType(file, name) != unix.SYSFS_MAGIC {
		return nil, errEvidence
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: file}, maximum+1))
	var after unix.Stat_t
	if err != nil || len(data) > int(maximum) || unix.Fstat(int(file.Fd()), &after) != nil || !stable(before, after) {
		return nil, errEvidence
	}
	return data, nil
}

func stable(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino && before.Mode == after.Mode && before.Uid == after.Uid && before.Gid == after.Gid && before.Nlink == after.Nlink && before.Size == after.Size && before.Mtim == after.Mtim && before.Ctim == after.Ctim
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
