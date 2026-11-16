//go:build linux

package nativefs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestOpenAtOpensOneComponentOnly(t *testing.T) {
	base := t.TempDir()
	write(t, filepath.Join(base, "real", "file"), "data")
	link(t, filepath.Join(base, "real"), filepath.Join(base, "linked"))
	dir := held(t, filepath.Join(base, "real"))
	for _, name := range []string{"", "..", "../linked/file", "sub/file", "/etc", "file\x00"} {
		if file, err := OpenAt(dir, name, syscall.O_RDONLY); !errors.Is(err, ErrInvalidName) {
			if file != nil {
				file.Close()
			}
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{".", "file"} {
		file, err := OpenAt(dir, name, syscall.O_RDONLY)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		file.Close()
	}
}

func TestTheKitRefusesItsZeroValuesWithoutAPanic(t *testing.T) {
	root, dir := publishRoot(t)
	for _, mode := range []os.FileMode{0, os.ModeDir | 0o600, os.ModeSetuid | 0o600} {
		if err := Publish(context.Background(), root, "target", []byte("data"), Publication{Mode: mode}); !errors.Is(err, ErrInvalidMode) {
			t.Errorf("mode %v: %v", mode, err)
		}
	}
	if got := names(t, dir); len(got) != 0 {
		t.Fatalf("a refused publication left %v", got)
	}
	entries := 10
	for _, tree := range []Tree{{Subject: "tree"}, {Subject: "tree", Entries: &entries, MaxDepth: -1}} {
		if err := RemoveTree(root, tree); !errors.Is(err, ErrInvalidTree) {
			t.Errorf("%+v: %v", tree, err)
		}
	}
	write(t, filepath.Join(dir, "file"), "data")
	if _, err := ReadBounded(context.Background(), held(t, filepath.Join(dir, "file")), -1); !errors.Is(err, ErrInvalidBound) {
		t.Fatalf("a negative bound: %v", err)
	}
	if data, err := ReadBounded(context.Background(), held(t, filepath.Join(dir, "file")), 0); err != nil || len(data) != 0 {
		t.Fatalf("a zero bound: %q, %v", data, err)
	}
}
