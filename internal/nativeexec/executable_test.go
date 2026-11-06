package nativeexec

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
	"time"
)

type metadata struct {
	mode fs.FileMode
	uid  uint32
}

func (m metadata) Name() string       { return "entry" }
func (m metadata) Size() int64        { return 0 }
func (m metadata) Mode() fs.FileMode  { return m.mode }
func (m metadata) ModTime() time.Time { return time.Time{} }
func (m metadata) IsDir() bool        { return m.mode.IsDir() }
func (m metadata) Sys() any           { return &syscall.Stat_t{Uid: m.uid} }

func TestQualificationProvesFileAndEveryResolvedAncestor(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		mode       fs.FileMode
		uid        uint32
	}{
		{"writable executable", "/usr/bin/tool", 0o777, 0},
		{"foreign executable", "/usr/bin/tool", 0o755, 1000},
		{"not executable", "/usr/bin/tool", 0o644, 0},
		{"directory executable", "/usr/bin/tool", fs.ModeDir | 0o755, 0},
		{"writable ancestor", "/usr", fs.ModeDir | 0o775, 0},
		{"foreign ancestor", "/usr/bin", fs.ModeDir | 0o755, 1000},
		{"writable root", "/", fs.ModeDir | 0o777, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stat := func(path string) (fs.FileInfo, error) {
				if path == tc.path {
					return metadata{tc.mode, tc.uid}, nil
				}
				if path == "/usr/bin/tool" {
					return metadata{0o755, 0}, nil
				}
				return metadata{fs.ModeDir | 0o755, 0}, nil
			}
			if _, err := qualify("/usr/bin/tool", func(s string) (string, error) { return s, nil }, stat); err == nil {
				t.Fatal("untrusted executable admitted")
			}
		})
	}
	seen := map[string]bool{}
	stat := func(path string) (fs.FileInfo, error) {
		seen[path] = true
		mode := fs.ModeDir | 0o755
		if path == "/usr/bin/tool" {
			mode = 0o755
		}
		return metadata{mode, 0}, nil
	}
	got, err := qualify("/bin/tool", func(string) (string, error) { return "/usr/bin/tool", nil }, stat)
	if err != nil || got != "/usr/bin/tool" || len(seen) != 4 || !seen["/"] {
		t.Fatalf("qualification %q %v, observed %v", got, err, seen)
	}
	if _, err := qualify("relative", func(string) (string, error) { t.Fatal("relative path resolved"); return "", nil }, stat); err == nil {
		t.Fatal("relative executable admitted")
	}
	if _, err := qualify("/missing", func(string) (string, error) { return "", errors.New("missing") }, stat); err == nil {
		t.Fatal("missing executable admitted")
	}
}
