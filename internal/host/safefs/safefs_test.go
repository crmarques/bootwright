package safefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	if err := AtomicWriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", string(got), "hello")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteFileEnsuringDirCreatesNestedDurablePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one", "two", "data")
	if err := WriteFileEnsuringDir(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "hello" {
		t.Fatalf("nested file = %q, %v", got, err)
	}
	for _, path := range []string{filepath.Join(dir, "one"), filepath.Join(dir, "one", "two")} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %s = %+v, %v", path, info, err)
		}
	}
}

func TestWriteNewFileRejectsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	if err := os.WriteFile(path, []byte("preexisting"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewFile(path, []byte("new"), 0o600); err == nil {
		t.Error("WriteNewFile should fail when the target exists")
	}
}

func TestRemoveFileDurable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveFileDurable(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("removed file stat error = %v", err)
	}
	if err := RemoveFileDurable(path); err != nil {
		t.Fatalf("repeated removal: %v", err)
	}
}

func TestRegularFileExists(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if ok, err := RegularFileExists(missing); err != nil || ok {
		t.Fatalf("missing file = %v, %v; want false, nil", ok, err)
	}
	file := filepath.Join(dir, "data")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := RegularFileExists(file); err != nil || !ok {
		t.Fatalf("regular file = %v, %v; want true, nil", ok, err)
	}
	if ok, err := RegularFileExists(dir); err == nil || ok {
		t.Fatalf("directory = %v, %v; want false, error", ok, err)
	}
}
