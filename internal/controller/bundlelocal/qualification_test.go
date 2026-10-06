//go:build controllerqualification && linux && amd64

package bundlelocal

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// qualificationArea is the private scoped bundle area the explicit
// qualification gates prepare into as the invoking user.
type qualificationArea struct {
	root     *os.Root
	location prerequisites.BundleLocation
	writes   int
}

func newQualificationArea(t *testing.T) *qualificationArea {
	t.Helper()
	location := t.TempDir()
	root, err := os.OpenRoot(location)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	info, err := os.Stat(location)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	return &qualificationArea{root: root, location: prerequisites.BundleLocation{Path: location, Device: uint64(stat.Dev), Inode: stat.Ino}}
}

func (a *qualificationArea) Read(ctx context.Context, name string, limit int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := a.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if len(data) > limit {
		return nil, fmt.Errorf("qualified read limit exceeded")
	}
	return data, err
}
func (a *qualificationArea) Write(ctx context.Context, name string, data []byte, executable bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	mode := fs.FileMode(0600)
	if executable {
		mode = 0700
	}
	file, err := a.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	a.writes++
	return nil
}
func (a *qualificationArea) EnsureDirectory(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.root.MkdirAll(name, 0700); err != nil {
		return err
	}
	a.writes++
	return nil
}
func (a *qualificationArea) Entries(ctx context.Context) ([]prerequisites.BundleEntry, error) {
	var entries []prerequisites.BundleEntry
	err := fs.WalkDir(a.root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected non-regular qualified entry")
		}
		entries = append(entries, prerequisites.BundleEntry{Path: filepath.ToSlash(name), Executable: !info.IsDir() && info.Mode()&0111 != 0, Size: info.Size(), Directory: info.IsDir()})
		return nil
	})
	return entries, err
}
func (a *qualificationArea) Verify(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(a.location.Path)
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if uint64(stat.Dev) != a.location.Device || stat.Ino != a.location.Inode {
		return fmt.Errorf("qualified root identity changed")
	}
	return nil
}
func (a *qualificationArea) Location(ctx context.Context) (prerequisites.BundleLocation, error) {
	if err := a.Verify(ctx); err != nil {
		return prerequisites.BundleLocation{}, err
	}
	return a.location, nil
}
