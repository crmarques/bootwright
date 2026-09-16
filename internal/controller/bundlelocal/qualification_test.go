//go:build controllerqualification && linux && amd64

package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// This explicit qualification gate uses previously acquired publisher artifacts
// named by catalog source ID in BOOTWRIGHT_BUNDLE_FIXTURES. It performs no
// network acquisition or host package effects; all bundle writes stay in the
// test's private scoped area. Normal unit tests exclude this build tag.
func TestQualifiedUnprivilegedBundlePreparationAndReadOnlyReuse(t *testing.T) {
	fixturePath := os.Getenv("BOOTWRIGHT_BUNDLE_FIXTURES")
	if fixturePath == "" {
		t.Fatal("explicit bundle fixture directory is required")
	}
	fixtures, err := os.OpenRoot(fixturePath)
	if err != nil {
		t.Fatal("qualified fixture directory cannot be opened")
	}
	defer fixtures.Close()
	definition, err := (Catalog{}).Select(prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, prerequisites.NativeRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	area := newQualificationArea(t)
	manager := New(ExecutionGuard{})
	// This explicit artifact gate executes as the invoking user. Production
	// ownership and native-lock boundaries are covered by the guard contract
	// tests; this probe qualifies the exact projected artifacts and imports.
	// It does not qualify privileged dependency installation.
	manager.probe = func(ctx context.Context, area prerequisites.BundleArea, definition prerequisites.Definition) error {
		if info, err := os.Stat("/etc/ld.so.preload"); err == nil && (!info.Mode().IsRegular() || info.Size() != 0) || err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("the current host has unqualified loader preload configuration")
		}
		for _, file := range definition.Execution.Files {
			data, err := os.ReadFile(file.Path)
			digest := sha256.Sum256(data)
			if err != nil || hex.EncodeToString(digest[:]) != file.SHA256 {
				return fmt.Errorf("the current host does not match the supplied execution fixture")
			}
		}
		location, err := area.Location(ctx)
		if err != nil {
			return err
		}
		return runImportProbe(ctx, area, prerequisites.PythonLaunch{
			Loader:      definition.Execution.Loader,
			Arguments:   []string{"--inhibit-cache", "--glibc-hwcaps-mask", "", "--library-path", filepath.Join(location.Path, "python/lib"), "--preload", strings.Join(definition.Execution.Preload, ":"), filepath.Join(location.Path, "python/bin/python3.13")},
			Directory:   location.Path,
			Environment: []string{"LC_ALL=C.UTF-8", "LANG=C.UTF-8", "HOME=" + location.Path, "OPENSSL_CONF=/dev/null"},
		})
	}
	manager.fetch = func(ctx context.Context, source prerequisites.DependencySource, _ prerequisites.SetupEgress) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := fixtures.Open(source.ID)
		if err != nil {
			return nil, fmt.Errorf("qualified source %s is unavailable", source.ID)
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, source.Bytes+1))
	}
	var details []string
	progress := func(event prerequisites.ProgressEvent) { details = append(details, event.Detail) }
	if err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, progress); err != nil {
		t.Fatal(err)
	}
	// Each slow phase announces itself: every acquisition, then the projection,
	// its verification and the interpreter probe.
	if len(details) < 4 || !strings.HasPrefix(details[0], "acquiring ") || !strings.Contains(details[0], ", source 1 of ") {
		t.Fatalf("preparation progress = %q", details)
	}
	tail := details[len(details)-3:]
	if !strings.HasPrefix(tail[0], "publishing ") || !strings.HasSuffix(tail[0], " bundle files") || tail[1] != "verifying the published bundle" || tail[2] != "qualifying the private interpreter" {
		t.Fatalf("preparation progress = %q", details)
	}
	before, err := area.Entries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	writes := area.writes
	for _, execute := range []bool{false, true} {
		inspection, err := manager.Inspect(t.Context(), area, definition, execute)
		if err != nil || !inspection.Ready || !inspection.Recoverable {
			t.Fatalf("qualified inspection failed: %+v %v", inspection, err)
		}
	}
	details = nil
	if err := manager.Prepare(t.Context(), area, nil, definition, prerequisites.SetupEgress{}, progress); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(details, []string{"qualifying the private interpreter"}) {
		t.Fatalf("ready bundle progress = %q", details)
	}
	after, err := area.Entries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if area.writes != writes || !slices.Equal(before, after) {
		t.Fatal("read-only inspection or repeated preparation changed the bundle")
	}
	t.Logf("verified %d bundle entries from %d exact publisher artifacts; unprivileged isolated imports and repeat preparation passed", len(after), len(definition.Sources))
}

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
