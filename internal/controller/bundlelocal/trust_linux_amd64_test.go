//go:build linux && amd64

package bundlelocal

import (
	"io/fs"
	"syscall"
	"testing"
	"time"
)

type trustEntry struct {
	mode fs.FileMode
	uid  uint32
}

func (e trustEntry) Name() string       { return "entry" }
func (e trustEntry) Size() int64        { return 0 }
func (e trustEntry) Mode() fs.FileMode  { return e.mode }
func (e trustEntry) ModTime() time.Time { return time.Time{} }
func (e trustEntry) IsDir() bool        { return e.mode.IsDir() }
func (e trustEntry) Sys() any           { return &syscall.Stat_t{Uid: e.uid} }

func TestTheTrustBundleAndItsDirectoriesAreRootOwnedWithoutGroupOrOtherWrite(t *testing.T) {
	for _, row := range []struct {
		name      string
		entry     trustEntry
		directory bool
		want      bool
	}{
		{"root directory", trustEntry{fs.ModeDir | 0o755, 0}, true, true},
		{"root bundle", trustEntry{0o444, 0}, false, true},
		{"foreign directory", trustEntry{fs.ModeDir | 0o755, 1000}, true, false},
		{"group-writable bundle", trustEntry{0o664, 0}, false, false},
		{"sticky world-writable root directory", trustEntry{fs.ModeDir | fs.ModeSticky | 0o777, 0}, true, false},
		{"directory where a file belongs", trustEntry{fs.ModeDir | 0o755, 0}, false, false},
		{"link where a file belongs", trustEntry{fs.ModeSymlink | 0o755, 0}, false, false},
	} {
		if got := trustedInfo(row.entry, row.directory); got != row.want {
			t.Errorf("%s: trusted %t, want %t", row.name, got, row.want)
		}
	}
	if trustedInfo(nil, true) {
		t.Error("a missing entry is trusted")
	}
}
