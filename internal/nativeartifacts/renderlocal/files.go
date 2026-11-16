package renderlocal

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/nativefs"
)

type Files struct {
	UID  func() int
	step func(string)
}

func (Files) Resolve(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", refuse("the path "+path+" cannot be resolved", "pass a path this account can reach")
	}
	existing, missing, err := splitExisting(absolute)
	if err != nil {
		return "", refuse("the path "+absolute+" cannot be inspected", "pass a path this account can reach")
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
	err = nativefs.Publish(ctx, root, name, data, nativefs.Publication{Prefix: ".render-", Mode: mode})
	var unsynced *nativefs.UnsyncedError
	switch {
	case errors.As(err, &unsynced):
		published = true
		return "", refuse(target+" was written but its directory could not be synced to disk: "+unsynced.Err.Error(), "repeat the command once the filesystem is healthy")
	case err != nil && ctx.Err() != nil:
		return "", ctx.Err()
	case err != nil:
		return fail(name+" cannot be written in "+directory+": "+err.Error(), "pass a directory this account can write")
	}
	published = true
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

func splitExisting(path string) (string, []string, error) {
	existing, missing := path, []string{}
	for {
		if _, err := os.Lstat(existing); err == nil {
			return existing, missing, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return existing, missing, err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return existing, missing, nil
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
}

func absent(directory string) []string {
	existing, missing, _ := splitExisting(directory)
	var made []string
	for index := len(missing); index > 0; index-- {
		made = append(made, filepath.Join(append([]string{existing}, missing[:index]...)...))
	}
	return made
}

func removeAll(made []string) {
	for _, directory := range made {
		os.Remove(directory)
	}
}

func refuse(message, remedy string) error {
	return diagnostics.NewFailureWithRemediation("render.publish", message, "", remedy)
}
