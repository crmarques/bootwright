package inputfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
)

func TestReadFileRejectsDirectoriesAndBoundsWithoutDiscovery(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "context.yaml", "context bytes\n")
	if err := syscall.Mkfifo(filepath.Join(root, "unopened.yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := (Reader{}).ReadFile(context.Background(), path, 14)
	if err != nil || string(got) != "context bytes\n" {
		t.Fatal("single file acquisition failed", string(got), err)
	}
	for _, source := range []string{root, filepath.Join(root, "unopened.yaml")} {
		if _, err := (Reader{}).ReadFile(context.Background(), source, 64); err == nil {
			t.Fatal("non-file source accepted", source)
		}
	}
	if _, err := (Reader{}).ReadFile(context.Background(), path, 4); err == nil {
		t.Fatal("single-file byte bound ignored")
	}
}

func TestReadDirectorySharesBoundedDiscoveryAndPayloadExclusions(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "nested/environment.yaml", "descriptor")
	writeFixture(t, root, "add-ons/_store/example/add-on.yaml", "add-on")
	writeFixture(t, root, "add-ons/_store/example/.bootwright-addon", "example\n")
	for _, name := range []string{"secrets", "playbooks", "roles", "collections", "manifests"} {
		writeFixture(t, root, name+"/sentinel.yaml", "unopened payload")
		if err := syscall.Mkfifo(filepath.Join(root, name, "blocked.yaml"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := (Reader{}).Read(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	got, err := (Reader{}).ReadDirectory(context.Background(), filepath.Join(root, "."))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("directory-only discovery changed acquisition: %#v, %v", got, err)
	}
	if err := os.Truncate(filepath.Join(root, "nested/environment.yaml"), desiredstate.MaxFileBytes+1); err != nil {
		t.Fatal(err)
	}
	got, err = (Reader{}).ReadDirectory(context.Background(), root)
	assertLimit(t, got, err, "YAML file bytes", desiredstate.MaxFileBytes)
}

func TestReadDirectoryRejectsNonDirectoryHandlesBeforeReading(t *testing.T) {
	root := t.TempDir()
	file := writeFixture(t, root, "input.yaml", "unopened descriptor")
	fifo := filepath.Join(root, "blocked.yaml")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	mustLink(t, root, link, true)
	for _, test := range []struct{ path, code string }{
		{file, "input.not-directory"},
		{fifo, "input.not-directory"},
		{link, "input.symlink"},
		{filepath.Join(root, "missing"), "input.not-found"},
	} {
		t.Run(filepath.Base(test.path), func(t *testing.T) {
			result, err := (Reader{}).ReadDirectory(context.Background(), test.path)
			assertFailure(t, result, err, test.code)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := (Reader{}).ReadDirectory(ctx, filepath.Join(root, "missing")); len(result.Files) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquisition = %#v, %v", result, err)
	}
}

func TestReadDirectoryAcceptsExplicitSkippedBasenameAndEmptyRoot(t *testing.T) {
	root := t.TempDir()
	if result, err := (Reader{}).ReadDirectory(context.Background(), root); err != nil || len(result.Files) != 0 || !reflect.DeepEqual(result.Roots, []string{root}) {
		t.Fatalf("empty directory = %#v, %v", result, err)
	}
	file := writeFixture(t, root, "secrets/environment.yaml", "explicit declaration")
	result, err := (Reader{}).ReadDirectory(context.Background(), filepath.Dir(file))
	if err != nil || !reflect.DeepEqual(sourcePaths(result.Files), []string{file}) {
		t.Fatalf("explicit root = %#v, %v", result, err)
	}
}

func TestDirectoryRootCannotBeSubstitutedAfterHandleVerification(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "input")
	writeFixture(t, root, "environment.yaml", "original")
	scan, err := newDiscovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer scan.root.Close()
	handle, identity, err := scan.openPath(root, pathHandle)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if err := scan.rememberIdentity(root, identity); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, filepath.Join(parent, "original")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "environment.yaml", "replacement")
	if err := scan.directory(root, 0); err == nil {
		t.Fatal("a replacement root was enumerated after the original handle was verified")
	}
	if len(scan.files) != 0 {
		t.Fatal("replacement root supplied candidate files")
	}
}
