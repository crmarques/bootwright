//go:build linux && amd64

package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"golang.org/x/sys/unix"
)

// ExecutionGuard verifies the provided ELF foundation before any private
// Python process can run. Its zero value fixes the installed host root and
// owner; the private view exists only for synthetic filesystem tests.
type ExecutionGuard struct{ view executionView }

type executionView struct {
	root  string
	owner uint32
}

func (guard ExecutionGuard) WithPython(ctx context.Context, area prerequisites.BundleArea, requirement prerequisites.ExecutionRequirement, use func(prerequisites.PythonLaunch, func() error) error) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	requirement.Files = slices.Clone(requirement.Files)
	requirement.Links = slices.Clone(requirement.Links)
	requirement.Preload = slices.Clone(requirement.Preload)
	if area == nil || use == nil || !validExecutionRequirement(requirement) {
		return executionFailure("controller.unsupported", "the private Python execution foundation is incomplete")
	}
	view := guard.view
	if view.root == "" {
		view = executionView{root: "/", owner: 0}
	}
	root, err := openExecutionRoot(view)
	if err != nil {
		return executionFailure("controller.unsupported", "the execution foundation root is unsafe")
	}
	defer root.Close()
	fs := executionFilesystem{root: root, owner: view.owner, links: make(map[string]string, len(requirement.Links))}
	for _, link := range requirement.Links {
		fs.links[link.Path] = link.Target
	}
	lock, err := fs.readLock(ctx, requirement.LockPath)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	active := true
	release := func() error {
		mu.Lock()
		defer mu.Unlock()
		if !active {
			return executionFailure("controller.state", "the dependency execution capability has expired")
		}
		if lock == nil {
			return nil
		}
		err := lock.Close()
		lock = nil
		if err != nil {
			return executionFailure("controller.unknown", "the native package read lock could not be released")
		}
		return nil
	}
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		active = false
		if lock != nil {
			if err := lock.Close(); err != nil && result == nil {
				result = executionFailure("controller.unknown", "the native package read lock could not be released")
			}
		}
	}()
	if err := fs.verify(ctx, requirement); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return executionFailure("controller.unsupported", "the provided execution libraries or loader configuration do not match the qualified foundation")
	}
	launch, bundle, err := openBundleLaunch(ctx, area, requirement, view.owner)
	if err != nil {
		return err
	}
	defer bundle.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := use(launch, release); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return area.Verify(ctx)
}

func (fs executionFilesystem) readLock(ctx context.Context, name string) (*os.File, error) {
	lock, _, err := fs.open(ctx, name, true, false)
	if err != nil {
		return nil, executionFailure("controller.unsupported", "the qualified native package lock is missing or unsafe")
	}
	readLock := unix.Flock_t{Type: unix.F_RDLCK, Whence: 0, Start: 0, Len: 0}
	if err := unix.FcntlFlock(lock.Fd(), unix.F_OFD_SETLK, &readLock); err != nil {
		lock.Close()
		if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EAGAIN) {
			return nil, executionFailure("controller.conflict", "a native package transaction prevents coherent dependency execution")
		}
		return nil, executionFailure("controller.unsupported", "the native package lock cannot protect dependency execution")
	}
	return lock, nil
}

func openBundleLaunch(ctx context.Context, area prerequisites.BundleArea, requirement prerequisites.ExecutionRequirement, owner uint32) (prerequisites.PythonLaunch, *os.File, error) {
	if err := area.Verify(ctx); err != nil {
		return prerequisites.PythonLaunch{}, nil, err
	}
	location, err := area.Location(ctx)
	if err != nil {
		return prerequisites.PythonLaunch{}, nil, err
	}
	if !executionPath(location.Path) || location.Device == 0 || location.Inode == 0 {
		return prerequisites.PythonLaunch{}, nil, executionFailure("controller.identity", "the private dependency bundle location is unverified")
	}
	bundle, err := openExecutionRoot(executionView{root: location.Path, owner: owner})
	if err != nil {
		return prerequisites.PythonLaunch{}, nil, executionFailure("controller.identity", "the private dependency bundle execution directory is unsafe")
	}
	var stat unix.Stat_t
	if unix.Fstat(int(bundle.Fd()), &stat) != nil || uint64(stat.Dev) != location.Device || stat.Ino != location.Inode {
		bundle.Close()
		return prerequisites.PythonLaunch{}, nil, executionFailure("controller.identity", "the private dependency bundle execution directory was replaced")
	}
	launch := prerequisites.PythonLaunch{
		Loader: requirement.Loader,
		Arguments: []string{
			"--inhibit-cache", "--glibc-hwcaps-mask", "",
			"--library-path", filepath.Join(location.Path, "python/lib"),
			"--preload", strings.Join(requirement.Preload, ":"),
			filepath.Join(location.Path, executionPython(requirement)),
		},
		Directory: location.Path,
		Environment: []string{
			"LC_ALL=C.UTF-8", "LANG=C.UTF-8", "HOME=" + location.Path, "OPENSSL_CONF=/dev/null",
		},
	}
	return launch, bundle, nil
}

func validExecutionRequirement(value prerequisites.ExecutionRequirement) bool {
	if value.PythonExecutable != "" && !validPythonExecutable(value.PythonExecutable) {
		return false
	}
	if value.Loader != "/usr/lib64/ld-linux-x86-64.so.2" || value.LockPath != "/usr/lib/sysimage/rpm/.rpm.lock" && value.LockPath != "/var/lib/rpm/.rpm.lock" || len(value.Files) < 2 || len(value.Files) > 32 || len(value.Links) > 64 || len(value.Preload) != len(value.Files)-1 {
		return false
	}
	files := make(map[string]bool, len(value.Files))
	for _, file := range value.Files {
		decoded, err := hex.DecodeString(file.SHA256)
		if !executionLibraryPath(file.Path) || files[file.Path] || err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != file.SHA256 {
			return false
		}
		files[file.Path] = true
	}
	if !files[value.Loader] {
		return false
	}
	links := make(map[string]bool, len(value.Links))
	for _, link := range value.Links {
		if !executionPath(link.Path) || link.Path != "/lib64" && !executionLibraryPath(link.Path) || !executionLinkTarget(link.Path, link.Target) || links[link.Path] || files[link.Path] {
			return false
		}
		links[link.Path] = true
	}
	preload := make(map[string]bool, len(value.Preload))
	for _, name := range value.Preload {
		if !files[name] || name == value.Loader || preload[name] {
			return false
		}
		preload[name] = true
	}
	return true
}

func executionPython(value prerequisites.ExecutionRequirement) string {
	if value.PythonExecutable != "" {
		return value.PythonExecutable
	}
	return "python/bin/python3.13"
}

func validPythonExecutable(value string) bool {
	if !strings.HasPrefix(value, "python/bin/python3.") || len(value) > 24 {
		return false
	}
	minor := strings.TrimPrefix(value, "python/bin/python3.")
	if minor == "" {
		return false
	}
	for _, c := range minor {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func executionPath(name string) bool {
	return strings.HasPrefix(name, "/") && name != "/" && path.Clean(name) == name && len(name) <= 4096 && !strings.ContainsAny(name, "\x00\r\n\t:;$")
}

func executionLibraryPath(name string) bool {
	return executionPath(name) && strings.HasPrefix(name, "/usr/lib64/")
}

func executionLinkTarget(name, target string) bool {
	if target == "" || len(target) > 4096 || strings.ContainsAny(target, "\x00\r\n\t :") {
		return false
	}
	destination := target
	if !path.IsAbs(destination) {
		destination = path.Join(path.Dir(name), destination)
	}
	return destination == "/usr/lib64" || executionLibraryPath(destination)
}

func openExecutionRoot(view executionView) (*os.File, error) {
	fd, err := unix.Open(view.root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != view.owner || stat.Mode&0022 != 0 {
		unix.Close(fd)
		return nil, unix.EPERM
	}
	return os.NewFile(uintptr(fd), view.root), nil
}

type executionFilesystem struct {
	root  *os.File
	owner uint32
	links map[string]string
}

func (fs executionFilesystem) open(ctx context.Context, name string, readable, followFinal bool) (*os.File, unix.Stat_t, error) {
	if !executionPath(name) {
		return nil, unix.Stat_t{}, unix.EINVAL
	}
	pending := strings.Split(strings.TrimPrefix(name, "/"), "/")
	resolved := []string{}
	parent, err := unix.Dup(int(fs.root.Fd()))
	if err != nil {
		return nil, unix.Stat_t{}, err
	}
	defer func() { unix.Close(parent) }()
	links := 0
	for len(pending) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, unix.Stat_t{}, err
		}
		component := pending[0]
		pending = pending[1:]
		fd, err := unix.Openat(parent, component, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, unix.Stat_t{}, err
		}
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil || stat.Uid != fs.owner {
			unix.Close(fd)
			return nil, unix.Stat_t{}, unix.EPERM
		}
		current := "/" + strings.Join(append(slices.Clone(resolved), component), "/")
		if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
			if len(pending) == 0 && !followFinal {
				if !readable {
					return os.NewFile(uintptr(fd), name), stat, nil
				}
				unix.Close(fd)
				return nil, unix.Stat_t{}, unix.ELOOP
			}
			buffer := make([]byte, 4097)
			n, err := unix.Readlinkat(fd, "", buffer)
			unix.Close(fd)
			links++
			if err != nil || n == 0 || n > 4096 || links > 16 || fs.links[current] != string(buffer[:n]) {
				return nil, unix.Stat_t{}, unix.ELOOP
			}
			target := string(buffer[:n])
			if !path.IsAbs(target) {
				target = path.Join(path.Dir(current), target)
			}
			pending = append(strings.Split(strings.TrimPrefix(path.Clean(target), "/"), "/"), pending...)
			resolved = nil
			unix.Close(parent)
			parent, err = unix.Dup(int(fs.root.Fd()))
			if err != nil {
				return nil, unix.Stat_t{}, err
			}
			continue
		}
		if len(pending) != 0 {
			if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0022 != 0 {
				unix.Close(fd)
				return nil, unix.Stat_t{}, unix.EPERM
			}
			unix.Close(parent)
			parent = fd
			resolved = append(resolved, component)
			continue
		}
		if !readable {
			return os.NewFile(uintptr(fd), name), stat, nil
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0022 != 0 || stat.Nlink != 1 {
			unix.Close(fd)
			return nil, unix.Stat_t{}, unix.EPERM
		}
		readFD, err := unix.Openat(parent, component, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOATIME|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if err != nil {
			return nil, unix.Stat_t{}, err
		}
		var opened unix.Stat_t
		if unix.Fstat(readFD, &opened) != nil || !sameExecutionFile(stat, opened) {
			unix.Close(readFD)
			return nil, unix.Stat_t{}, unix.ESTALE
		}
		return os.NewFile(uintptr(readFD), name), opened, nil
	}
	return nil, unix.Stat_t{}, unix.EINVAL
}

func sameExecutionFile(before, after unix.Stat_t) bool {
	return before.Dev == after.Dev && before.Ino == after.Ino && before.Mode == after.Mode && before.Uid == after.Uid && before.Gid == after.Gid && before.Nlink == after.Nlink && before.Size == after.Size && before.Mtim == after.Mtim && before.Ctim == after.Ctim
}

func (fs executionFilesystem) verify(ctx context.Context, requirement prerequisites.ExecutionRequirement) error {
	preload, stat, err := fs.open(ctx, "/etc/ld.so.preload", true, false)
	if preload != nil {
		preload.Close()
	}
	if err != nil && !errors.Is(err, unix.ENOENT) || err == nil && stat.Size != 0 {
		return unix.EPERM
	}
	files := make(map[string]bool, len(requirement.Files))
	var total int64
	for _, approved := range requirement.Files {
		file, before, err := fs.open(ctx, approved.Path, true, false)
		if err != nil {
			return err
		}
		if before.Size <= 0 || before.Size > 128<<20 || total > 512<<20-before.Size {
			file.Close()
			return unix.EFBIG
		}
		total += before.Size
		digest := sha256.New()
		_, err = io.Copy(digest, io.LimitReader(executionReader{ctx, file}, before.Size+1))
		var after unix.Stat_t
		statErr := unix.Fstat(int(file.Fd()), &after)
		file.Close()
		if err != nil || statErr != nil || !sameExecutionFile(before, after) || hex.EncodeToString(digest.Sum(nil)) != approved.SHA256 {
			return unix.ESTALE
		}
		files[approved.Path] = true
	}
	for _, approved := range requirement.Links {
		file, before, err := fs.open(ctx, approved.Path, false, false)
		if err != nil {
			return err
		}
		buffer := make([]byte, 4097)
		n, readErr := unix.Readlinkat(int(file.Fd()), "", buffer)
		var after unix.Stat_t
		statErr := unix.Fstat(int(file.Fd()), &after)
		file.Close()
		if readErr != nil || statErr != nil || n == 0 || n > 4096 || before.Mode&unix.S_IFMT != unix.S_IFLNK || !sameExecutionFile(before, after) || string(buffer[:n]) != approved.Target {
			return unix.ESTALE
		}
		destination := approved.Target
		if !path.IsAbs(destination) {
			destination = path.Join(path.Dir(approved.Path), destination)
		}
		for count := 0; fs.links[destination] != ""; count++ {
			if count == 16 {
				return unix.ELOOP
			}
			target := fs.links[destination]
			if !path.IsAbs(target) {
				target = path.Join(path.Dir(destination), target)
			}
			destination = target
		}
		if !files[destination] && destination != "/usr/lib64" {
			return unix.EPERM
		}
	}
	return ctx.Err()
}

type executionReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader executionReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

func executionFailure(code, message string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", "Restore the qualified host execution foundation before retrying setup or preflight.")
}
