package renderlocal

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

const (
	treeFileMode      = 0o600
	treeDirectoryMode = 0o700
	openDirectory     = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC
)

type treeEntry struct {
	parent    int
	name      string
	directory bool
	device    uint64
	inode     uint64
}

type treeWriter struct {
	owner       Files
	directory   string
	handles     []int
	directories map[string]int
	created     []treeEntry
}

func (f Files) PublishTree(ctx context.Context, directory string, files []rendering.TreeFile) (string, error) {
	if err := checkTree(files); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(directory)
	if err != nil || strings.ContainsRune(directory, 0) {
		return "", refuse("the path "+directory+" cannot be resolved", "pass a path this account can reach")
	}
	writer := &treeWriter{owner: f, directory: absolute, directories: map[string]int{}}
	defer writer.close()
	if err := writer.open(); err != nil {
		writer.discard()
		return "", err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			writer.discard()
			return "", err
		}
		if err := writer.write(file); err != nil {
			writer.discard()
			return "", refuse(file.Path+" cannot be written in "+absolute+": "+err.Error(), "pass a directory this account can write")
		}
		f.pause(file.Path)
	}
	return absolute, nil
}

func (f Files) pause(stage string) {
	if f.step != nil {
		f.step(stage)
	}
}

func (w *treeWriter) open() error {
	existing, missing := w.directory, []string{}
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return refuse(w.directory+" cannot be inspected: "+err.Error(), "pass a directory this account can write")
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
	name := ""
	if len(missing) == 0 {
		existing, name = filepath.Dir(w.directory), filepath.Base(w.directory)
	}
	w.owner.pause("open")
	handle, err := unix.Open(existing, openDirectory, 0)
	if err != nil {
		return refuse(existing+" cannot be opened: "+err.Error(), "pass a directory this account can write")
	}
	w.handles = append(w.handles, handle)
	if len(missing) == 0 {
		return w.adopt(handle, name)
	}
	for _, component := range missing {
		if handle, err = w.makeDirectory(handle, component); err != nil {
			return refuse(w.directory+" cannot be created: "+err.Error(), "pass a directory this account can write")
		}
	}
	w.directories["."] = handle
	return nil
}

func (w *treeWriter) adopt(parent int, name string) error {
	handle, err := unix.Openat(parent, name, openDirectory|unix.O_NOFOLLOW, 0)
	switch {
	case errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR):
		return refuse(w.directory+" is not a directory or is a link, so nothing is written there", "pass a directory that does not exist yet or is empty")
	case err != nil:
		return refuse(w.directory+" cannot be opened: "+err.Error(), "pass a directory this account can write")
	}
	w.handles = append(w.handles, handle)
	var stat unix.Stat_t
	if err := unix.Fstat(handle, &stat); err != nil {
		return refuse(w.directory+" cannot be inspected: "+err.Error(), "pass a directory this account can write")
	}
	if reason := w.owner.foreign(stat); reason != "" {
		return refuse(w.directory+" "+reason+", so a private tree is not written there", "choose a directory that only this account can write")
	}
	empty, err := emptyDirectory(handle)
	if err != nil {
		return refuse(w.directory+" cannot be listed: "+err.Error(), "pass a directory this account can write")
	}
	if !empty {
		return refuse(w.directory+" is not empty, so nothing is written into it", "pass a directory that does not exist yet or is empty")
	}
	w.directories["."] = handle
	return nil
}

func (f Files) foreign(stat unix.Stat_t) string {
	uid := os.Geteuid
	if f.UID != nil {
		uid = f.UID
	}
	if int(stat.Uid) != uid() {
		return "is not owned by this account"
	}
	if uint32(stat.Mode)&0o022 != 0 {
		return "is writable by its group or by others"
	}
	return ""
}

func emptyDirectory(handle int) (bool, error) {
	listing, err := unix.Openat(handle, ".", openDirectory, 0)
	if err != nil {
		return false, err
	}
	directory := os.NewFile(uintptr(listing), ".")
	defer directory.Close()
	names, err := directory.Readdirnames(-1)
	return len(names) == 0, err
}

func (w *treeWriter) makeDirectory(parent int, name string) (int, error) {
	if err := unix.Mkdirat(parent, name, treeDirectoryMode); err != nil {
		return -1, err
	}
	handle, err := unix.Openat(parent, name, openDirectory|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	w.handles = append(w.handles, handle)
	if err := w.record(parent, name, handle, true); err != nil {
		return -1, err
	}
	return handle, unix.Fchmod(handle, treeDirectoryMode)
}

func (w *treeWriter) record(parent int, name string, handle int, directory bool) error {
	var stat unix.Stat_t
	if err := unix.Fstat(handle, &stat); err != nil {
		return err
	}
	w.created = append(w.created, treeEntry{parent: parent, name: name, directory: directory, device: uint64(stat.Dev), inode: stat.Ino})
	return nil
}

func (w *treeWriter) write(file rendering.TreeFile) error {
	parent := w.directories["."]
	for _, directory := range parents(file.Path) {
		if handle, ok := w.directories[directory]; ok {
			parent = handle
			continue
		}
		handle, err := w.makeDirectory(parent, path.Base(directory))
		if err != nil {
			return err
		}
		w.directories[directory], parent = handle, handle
	}
	name := path.Base(file.Path)
	descriptor, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, treeFileMode)
	if err != nil {
		return err
	}
	handle := os.NewFile(uintptr(descriptor), name)
	if err := w.record(parent, name, descriptor, false); err != nil {
		handle.Close()
		return err
	}
	for _, step := range []func() error{
		func() error { _, err := handle.Write(file.Data); return err },
		func() error { return handle.Chmod(treeFileMode) },
	} {
		if err := step(); err != nil {
			handle.Close()
			return err
		}
	}
	return handle.Close()
}

func (w *treeWriter) discard() {
	for index := len(w.created) - 1; index >= 0; index-- {
		entry := w.created[index]
		var stat unix.Stat_t
		if unix.Fstatat(entry.parent, entry.name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil || uint64(stat.Dev) != entry.device || stat.Ino != entry.inode {
			continue
		}
		flags := 0
		if entry.directory {
			flags = unix.AT_REMOVEDIR
		}
		_ = unix.Unlinkat(entry.parent, entry.name, flags)
	}
	w.created = nil
}

func (w *treeWriter) close() {
	for _, handle := range w.handles {
		_ = unix.Close(handle)
	}
	w.handles = nil
}

func checkTree(files []rendering.TreeFile) error {
	seen := map[string]bool{}
	for _, file := range files {
		switch {
		case !fs.ValidPath(file.Path) || file.Path == ".":
			return refuse("the tree path "+file.Path+" is not a plain relative path", "report this; no tree path may be absolute or hold a . or .. element")
		case strings.ContainsRune(file.Path, 0):
			return refuse("a tree path holds a NUL byte", "report this; no tree path may hold one")
		case seen[file.Path]:
			return refuse("the tree path "+file.Path+" is written twice", "report this; each tree path names one file")
		}
		seen[file.Path] = true
	}
	return nil
}

func parents(name string) []string {
	var directories []string
	for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
		directories = append([]string{directory}, directories...)
	}
	return directories
}
