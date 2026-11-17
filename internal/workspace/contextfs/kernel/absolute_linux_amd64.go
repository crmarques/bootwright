//go:build linux && amd64

package kernel

import (
	"errors"
	"os"
	"strings"
	"syscall"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// Existing ancestors may be system directories; all missing suffixes are
// exclusively created private directories. No component may be a symlink.
func WalkAbsolute(path string, create func(*os.File) error) (*os.File, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(fd), "/")
	if path == "/" {
		return parent, nil
	}
	if create != nil {
		probe, err := OpenWithin(&Directory{File: parent, Path: "/"}, ".", PathHandle|syscall.O_DIRECTORY, 0)
		if err != nil {
			parent.Close()
			return nil, state("state containment primitives are unavailable")
		}
		probe.Close()
		return createAbsolute(path, parent, create)
	}
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, name := range components {
		if err := QualifiedFileSystem(parent); err != nil {
			parent.Close()
			return nil, err
		}
		child, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if err != nil {
			err = unreachableAncestor(parent, name, components[:i+1], err)
			parent.Close()
			return nil, err
		}
		parent.Close()
		parent = os.NewFile(uintptr(child), name)
	}
	return parent, nil
}

// UnreachableRoot is the refusal of a state root the store cannot reach
// because one component of its absolute path, named in full, is a symbolic
// link or not a directory; its status tells the root itself from an ancestor.
type UnreachableRoot struct {
	Component string
	Stat      syscall.Stat_t
	Failure   error
}

func (e *UnreachableRoot) Error() string { return e.Failure.Error() }

func (e *UnreachableRoot) Unwrap() error { return e.Failure }

func unreachableRootRefusal(component string, stat syscall.Stat_t) error {
	return &UnreachableRoot{Component: component, Stat: stat, Failure: contexts.StateErrorWithRemediation(
		"the state root is reached through "+component+", which is "+FileKind(stat.Mode)+
			", but the store reaches its root only through directories and never follows a symbolic link",
		"replace "+component+" with a directory, such as the directory a symbolic link there names or a mount of that storage, then repeat the command")}
}

// unreachableAncestor names the component an open of the root's path refused
// because it is a symbolic link or not a directory; any other error, or one
// whose component cannot be read without following it, passes unchanged.
func unreachableAncestor(parent *os.File, name string, walked []string, err error) error {
	if !errors.Is(err, syscall.ENOTDIR) && !errors.Is(err, syscall.ELOOP) {
		return err
	}
	fd, openErr := syscall.Openat(int(parent.Fd()), name, PathHandle|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if openErr != nil {
		return err
	}
	var stat syscall.Stat_t
	statErr := syscall.Fstat(fd, &stat)
	syscall.Close(fd)
	if statErr != nil || stat.Mode&syscall.S_IFMT == syscall.S_IFDIR {
		return err
	}
	return unreachableRootRefusal("/"+strings.Join(walked, "/"), stat)
}

func createAbsolute(path string, base *os.File, admit func(*os.File) error) (*os.File, error) {
	type held struct {
		file     *os.File
		name     string
		identity syscall.Stat_t
	}
	initial, err := StatHandle(base)
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
			current, err := StatHandle(entry.file)
			if err != nil || !SameIdentity(entry.identity, current) {
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
			if err != nil || !SameIdentity(entry.identity, named) {
				return state("state ancestor was replaced during creation")
			}
		}
		return nil
	}
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, name := range components {
		if err := verify(); err != nil {
			return nil, err
		}
		parent := chain[len(chain)-1].file
		if err := QualifiedFileSystem(parent); err != nil {
			return nil, err
		}
		fd, err := syscall.Openat(int(parent.Fd()), name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		created := false
		if errors.Is(err, syscall.ENOENT) {
			if err := verify(); err != nil {
				return nil, err
			}
			if err := admit(parent); err != nil {
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
			return nil, unreachableAncestor(parent, name, components[:i+1], err)
		}
		file := os.NewFile(uintptr(fd), name)
		if err := QualifiedFileSystem(file); err != nil {
			file.Close()
			return nil, err
		}
		identity, err := StatHandle(file)
		if err != nil || created && (!Private(identity, syscall.S_IFDIR, uint32(os.Geteuid()), uint32(os.Getegid())) || identity.Mode&0777 != 0700) {
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

func LocalFilesystem(kind int64) bool {
	switch kind {
	case 0xef53, 0x58465342, 0x9123683e, 0x01021994, 0x794c7630:
		return true
	}
	return false
}

func QualifiedFileSystem(file *os.File) error {
	var fs syscall.Statfs_t
	if err := syscall.Fstatfs(int(file.Fd()), &fs); err != nil || !LocalFilesystem(fs.Type) {
		return state("state filesystem is not qualified for private durable storage")
	}
	return nil
}
