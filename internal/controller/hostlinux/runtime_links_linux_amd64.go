//go:build linux && amd64

package hostlinux

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"golang.org/x/sys/unix"
)

func runtimeDataPath(name string) bool {
	if !runtimeCanonicalPath(name) {
		return false
	}
	for _, root := range []string{"/usr", "/etc/containers", "/etc/selinux/targeted"} {
		if strings.HasPrefix(name, root+"/") {
			return true
		}
	}
	return false
}

func runtimeAliasPath(name string) bool {
	if runtimeDataPath(name) {
		return true
	}
	switch name {
	case "/bin", "/sbin", "/lib", "/lib64", "/etc/containers", "/etc/selinux/targeted":
		return true
	}
	return false
}

func runtimeCanonicalPath(name string) bool {
	return strings.HasPrefix(name, "/") && path.Clean(name) == name && len(name) <= 4096 && !strings.ContainsAny(name, "\x00\r\n\t")
}

func runtimeTargetPath(name, target string) bool {
	if target == "" || len(target) > 4096 || strings.ContainsAny(target, "\x00\r\n\t") {
		return false
	}
	resolved := linkDestination(name, target)
	return runtimeDataPath(resolved) || resolved == "/usr" || resolved == "/etc/containers" || resolved == "/etc/selinux/targeted"
}

func linkDestination(name, target string) string {
	if strings.HasPrefix(target, "/") {
		return path.Clean(target)
	}
	return path.Join(path.Dir(name), target)
}

func (f *heldFilesystem) runtimeLinks(ctx context.Context, requirement prerequisites.RuntimeRequirement) error {
	missing := false
	files := make(map[string]bool, len(requirement.Files))
	for _, file := range requirement.Files {
		files[file.Path] = true
	}
	for _, link := range requirement.Links {
		target, err := f.readLink(ctx, link.Path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				missing = true
				continue
			}
			return err
		}
		if target != link.Target {
			return errEvidence
		}
		destination := linkDestination(link.Path, target)
		for count := 0; f.qualifiedLinks[destination] != ""; count++ {
			if count == 16 {
				return errEvidence
			}
			destination = linkDestination(destination, f.qualifiedLinks[destination])
		}
		if files[destination] {
			continue
		}
		// Directory aliases have no file digest, but their exact target and
		// final trusted directory are still mandatory. Every regular-file
		// alias must terminate in this manifest's separately hashed files.
		file, stat, err := f.openPath(ctx, destination, false)
		if file != nil {
			file.Close()
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				missing = true
				continue
			}
			return err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0022 != 0 {
			return errEvidence
		}
	}
	if missing {
		return os.ErrNotExist
	}
	return nil
}

func (f *heldFilesystem) readLink(ctx context.Context, name string) (string, error) {
	if !runtimeCanonicalPath(name) || name == "/" {
		return "", errEvidence
	}
	parentName := path.Dir(name)
	parentFD := -1
	if parentName == "/" {
		var err error
		parentFD, err = unix.Dup(int(f.root.Fd()))
		if err != nil {
			return "", err
		}
	} else {
		parent, stat, err := f.openPath(ctx, parentName, false)
		if err != nil {
			return "", err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0022 != 0 {
			parent.Close()
			return "", errEvidence
		}
		parentFD, err = unix.Dup(int(parent.Fd()))
		parent.Close()
		if err != nil {
			return "", err
		}
	}
	defer unix.Close(parentFD)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fd, err := unix.Openat(parentFD, path.Base(name), unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	var before, after unix.Stat_t
	var filesystem unix.Statfs_t
	if unix.Fstat(fd, &before) != nil || before.Uid != f.view.owner || before.Mode&unix.S_IFMT != unix.S_IFLNK || unix.Fstatfs(fd, &filesystem) != nil || filesystem.Type == unix.PROC_SUPER_MAGIC {
		return "", errEvidence
	}
	buffer := make([]byte, 4097)
	n, err := unix.Readlinkat(fd, "", buffer)
	if err != nil || n == 0 || n > 4096 || unix.Fstat(fd, &after) != nil || !stable(before, after) {
		return "", errEvidence
	}
	return string(buffer[:n]), ctx.Err()
}
