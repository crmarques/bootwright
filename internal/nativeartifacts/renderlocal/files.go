package renderlocal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type Files struct{ UID func() int }

func (Files) Resolve(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", refuse("the path "+path+" cannot be resolved", "pass a path this account can reach")
	}
	existing, missing := absolute, []string{}
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", refuse("the path "+absolute+" cannot be inspected", "pass a path this account can reach")
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", refuse("the path "+absolute+" cannot be resolved", "pass a path this account can reach")
	}
	return filepath.Join(append([]string{real}, missing...)...), nil
}

func (f Files) Publish(ctx context.Context, directory, name string, data []byte, private bool) (string, error) {
	mode, directoryMode := os.FileMode(0o644), os.FileMode(0o755)
	if private {
		mode, directoryMode = 0o600, 0o700
	}
	if info, err := os.Stat(directory); err == nil && !info.IsDir() {
		return "", refuse(directory+" is not a directory", "pass a directory, or a path that does not exist yet")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", refuse(directory+" cannot be inspected: "+err.Error(), "pass a directory this account can write")
	}
	made := absent(directory)
	if err := os.MkdirAll(directory, directoryMode); err != nil {
		removeAll(made)
		return "", refuse(directory+" cannot be created: "+err.Error(), "pass a directory this account can write")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		removeAll(made)
		return "", refuse(directory+" cannot be opened: "+err.Error(), "pass a directory this account can write")
	}
	defer root.Close()
	published := false
	defer func() {
		if !published {
			removeAll(made)
		}
	}()
	fail := func(message, remedy string) (string, error) { return "", refuse(message, remedy) }
	if private {
		info, err := root.Stat(".")
		if err != nil {
			return fail(directory+" cannot be inspected: "+err.Error(), "pass a directory this account can write")
		}
		if reason := f.unsafe(info); reason != "" {
			return fail(directory+" "+reason+", so a sensitive artifact is not written there", "choose an --output-dir that only this account can write")
		}
	}
	target := filepath.Join(directory, name)
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return fail(target+" exists and is not a regular file, so it is not replaced", "remove it or choose another --output-dir")
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fail(target+" cannot be inspected: "+err.Error(), "pass a directory this account can write")
	}
	partial, handle, err := createPartial(root)
	if err != nil {
		return fail(name+" cannot be written in "+directory+": "+err.Error(), "pass a directory this account can write")
	}
	defer func() {
		if !published {
			handle.Close()
			root.Remove(partial)
		}
	}()
	for _, step := range []func() error{
		func() error { _, err := handle.Write(data); return err },
		func() error { return handle.Chmod(mode) },
		handle.Sync,
		handle.Close,
		ctx.Err,
	} {
		if err := step(); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", refuse(name+" cannot be written in "+directory+": "+err.Error(), "pass a directory this account can write")
		}
	}
	if err := root.Rename(partial, name); err != nil {
		return fail(name+" cannot be written in "+directory+": "+err.Error(), "pass a directory this account can write")
	}
	published = true
	synced, err := root.Open(".")
	if err == nil {
		err = synced.Sync()
		synced.Close()
	}
	if err != nil {
		return "", refuse(target+" was written but its directory could not be synced to disk: "+err.Error(), "repeat the command once the filesystem is healthy")
	}
	return target, nil
}

func (f Files) unsafe(info fs.FileInfo) string {
	uid := os.Geteuid
	if f.UID != nil {
		uid = f.UID
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != uid() {
		return "is not owned by this account"
	}
	if info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
		return "is writable by its group or by others"
	}
	return ""
}

func createPartial(root *os.Root) (string, *os.File, error) {
	var last error
	for range 8 {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := ".render-" + hex.EncodeToString(random[:]) + ".part"
		handle, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return name, handle, nil
		}
		last = err
		if !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	return "", nil, last
}

func absent(directory string) []string {
	var made []string
	for current := directory; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(current); err == nil || !errors.Is(err, fs.ErrNotExist) || filepath.Dir(current) == current {
			return made
		}
		made = append(made, current)
	}
}

func removeAll(made []string) {
	for _, directory := range made {
		os.Remove(directory)
	}
}

func refuse(message, remedy string) error {
	return diagnostics.NewFailureWithRemediation("render.publish", message, "", remedy)
}
