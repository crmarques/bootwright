package contextfs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestInstallerLogOpenRefusesUnsafePathsAndEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hosted")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
	const leaf = ".native.test.log"
	open := func(path string) error {
		t.Helper()
		file, _, err := openNativeToolLog(root, filepath.Join(path, leaf), uid, gid)
		if file != nil {
			file.Close()
		} else if err == nil {
			err = os.ErrNotExist
		}
		return err
	}
	if err := open(work); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing log = %v", err)
	}
	log := filepath.Join(work, leaf)
	foreign := filepath.Join(root, "foreign")
	if err := os.WriteFile(foreign, []byte("native log"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []func() error{
		func() error { return os.Symlink(foreign, log) },
		func() error { return os.Link(foreign, log) },
		func() error { return syscall.Mkfifo(log, 0600) },
		func() error {
			if err := os.WriteFile(log, nil, 0600); err != nil {
				return err
			}
			return os.Chmod(log, 0664)
		},
	} {
		if err := entry(); err != nil {
			t.Fatal(err)
		}
		if err := open(work); err == nil {
			t.Fatal("an unsafe file opened")
		}
		if err := os.Remove(log); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(foreign, log); err != nil {
		t.Fatal(err)
	}
	if err := open(work); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(work, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := open(filepath.Join(root, "alias")); err == nil {
		t.Fatal("a symlinked work directory opened")
	}
	if err := os.Chmod(work, 0755); err != nil {
		t.Fatal(err)
	}
	if err := open(work); err == nil {
		t.Fatal("a publicly searchable work directory opened")
	}
	if err := os.Chmod(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0770); err != nil {
		t.Fatal(err)
	}
	if err := open(work); err == nil {
		t.Fatal("a writable hosted root was accepted")
	}
}
