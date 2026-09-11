//go:build linux && amd64

package bundlelocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"golang.org/x/sys/unix"
)

type executionArea struct {
	prerequisites.BundleArea
	location prerequisites.BundleLocation
	checks   int
	err      error
}

func (area *executionArea) Verify(context.Context) error {
	area.checks++
	return area.err
}

func (area *executionArea) Location(context.Context) (prerequisites.BundleLocation, error) {
	return area.location, area.err
}

type executionFixture struct {
	guard       ExecutionGuard
	requirement prerequisites.ExecutionRequirement
	area        executionArea
	root        string
}

func newExecutionFixture(t *testing.T) executionFixture {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{"usr/lib64", "usr/lib/sysimage/rpm", "etc", "bundle with spaces/python/lib"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0755); err != nil {
			t.Fatal(err)
		}
	}
	requirement := prerequisites.ExecutionRequirement{Loader: "/usr/lib64/ld-linux-x86-64.so.2", LockPath: "/usr/lib/sysimage/rpm/.rpm.lock"}
	for _, name := range []string{requirement.Loader, "/usr/lib64/libc.so.6", "/usr/lib64/libgcc_s-1.so.1"} {
		data := []byte("synthetic qualified ELF " + name)
		digest := sha256.Sum256(data)
		if err := os.WriteFile(filepath.Join(root, name), data, 0555); err != nil {
			t.Fatal(err)
		}
		requirement.Files = append(requirement.Files, prerequisites.InstalledFile{Path: name, SHA256: hex.EncodeToString(digest[:])})
	}
	requirement.Preload = []string{"/usr/lib64/libc.so.6", "/usr/lib64/libgcc_s-1.so.1"}
	requirement.Links = []prerequisites.InstalledLink{{Path: "/lib64", Target: "usr/lib64"}, {Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-1.so.1"}}
	for _, link := range requirement.Links {
		if err := os.Symlink(link.Target, filepath.Join(root, link.Path)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, requirement.LockPath), nil, 0600); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle with spaces")
	var stat unix.Stat_t
	if err := unix.Stat(bundle, &stat); err != nil {
		t.Fatal(err)
	}
	return executionFixture{
		guard: ExecutionGuard{view: executionView{root: root, owner: uint32(os.Geteuid())}}, requirement: requirement, root: root,
		area: executionArea{location: prerequisites.BundleLocation{Path: bundle, Device: uint64(stat.Dev), Inode: stat.Ino}},
	}
}

func executionLock(t *testing.T, fixture executionFixture) *os.File {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(fixture.root, fixture.requirement.LockPath), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func tryExecutionWriteLock(file *os.File) error {
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	return unix.FcntlFlock(file.Fd(), unix.F_SETLK, &lock)
}

func TestExecutionGuardHoldsNativeReadLockAndIsolatesLaunch(t *testing.T) {
	f := newExecutionFixture(t)
	writer := executionLock(t, f)
	t.Setenv("LD_PRELOAD", "/untrusted.so")
	t.Setenv("PYTHONPATH", "/untrusted")
	before := make([]unix.Stat_t, len(f.requirement.Files))
	for i, file := range f.requirement.Files {
		if err := unix.Stat(filepath.Join(f.root, file.Path), &before[i]); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	err := f.guard.WithPython(context.Background(), &f.area, f.requirement, func(launch prerequisites.PythonLaunch, _ func() error) error {
		called = true
		if err := tryExecutionWriteLock(writer); !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
			t.Fatalf("native write lock was not excluded: %v", err)
		}
		want := []string{"--inhibit-cache", "--glibc-hwcaps-mask", "", "--library-path", filepath.Join(f.area.location.Path, "python/lib"), "--preload", strings.Join(f.requirement.Preload, ":"), filepath.Join(f.area.location.Path, "python/bin/python3.13")}
		if launch.Loader != f.requirement.Loader || launch.Directory != f.area.location.Path || !reflect.DeepEqual(launch.Arguments, want) {
			t.Fatal("launch did not preserve exact isolated loader arguments")
		}
		for _, entry := range launch.Environment {
			if strings.HasPrefix(entry, "LD_") || strings.HasPrefix(entry, "PYTHON") || strings.HasPrefix(entry, "PATH=") {
				t.Fatal("ambient execution authority entered private launch")
			}
		}
		return nil
	})
	if err != nil || !called || f.area.checks != 2 {
		t.Fatalf("verified launch failed: %v", err)
	}
	if err := tryExecutionWriteLock(writer); err != nil {
		t.Fatalf("read lock survived callback completion: %v", err)
	}
	for i, file := range f.requirement.Files {
		var after unix.Stat_t
		if err := unix.Stat(filepath.Join(f.root, file.Path), &after); err != nil || !sameExecutionFile(before[i], after) || before[i].Atim != after.Atim {
			t.Fatal("execution verification changed library metadata")
		}
	}
}

func TestExecutionGuardExplicitHandoffExpiresAfterCallback(t *testing.T) {
	f := newExecutionFixture(t)
	writer := executionLock(t, f)
	var retained func() error
	err := f.guard.WithPython(context.Background(), &f.area, f.requirement, func(_ prerequisites.PythonLaunch, release func() error) error {
		retained = release
		if err := release(); err != nil {
			return err
		}
		if err := release(); err != nil {
			return err
		}
		return tryExecutionWriteLock(writer)
	})
	if err != nil || retained == nil || retained() == nil {
		t.Fatalf("scoped native lock handoff failed: %v", err)
	}
}

func TestExecutionGuardEmptyPreloadAndCallbackCancellation(t *testing.T) {
	f := newExecutionFixture(t)
	if err := os.WriteFile(filepath.Join(f.root, "etc/ld.so.preload"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := f.guard.WithPython(ctx, &f.area, f.requirement, func(prerequisites.PythonLaunch, func() error) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("callback cancellation was not preserved: %v", err)
	}
	writer := executionLock(t, f)
	if err := tryExecutionWriteLock(writer); err != nil {
		t.Fatalf("cancellation leaked the native read lock: %v", err)
	}
}

func TestExecutionGuardRefusesActiveNativeTransactionBeforeBundleAccess(t *testing.T) {
	f := newExecutionFixture(t)
	writer := executionLock(t, f)
	if err := tryExecutionWriteLock(writer); err != nil {
		t.Fatal(err)
	}
	err := f.guard.WithPython(context.Background(), &f.area, f.requirement, func(prerequisites.PythonLaunch, func() error) error {
		t.Fatal("execution proceeded during native transaction")
		return nil
	})
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != "controller.conflict" || f.area.checks != 0 {
		t.Fatalf("native transaction conflict was not preserved: %v", err)
	}
}

func TestExecutionGuardRejectsUnqualifiedExecutionAuthority(t *testing.T) {
	for _, scenario := range []string{"changed-library", "writable-parent", "preload", "preload-symlink", "changed-alias", "unlisted-alias", "hardlink", "missing-lock", "lock-symlink", "bundle-replaced", "partial-preload", "duplicate-preload", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			f := newExecutionFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			library := filepath.Join(f.root, f.requirement.Files[1].Path)
			lock := filepath.Join(f.root, f.requirement.LockPath)
			var err error
			switch scenario {
			case "changed-library":
				err = os.Chmod(library, 0644)
				if err == nil {
					err = os.WriteFile(library, []byte("substituted"), 0644)
				}
			case "writable-parent":
				err = os.Chmod(filepath.Join(f.root, "usr/lib64"), 0777)
			case "preload":
				err = os.WriteFile(filepath.Join(f.root, "etc/ld.so.preload"), []byte("/untrusted.so\n"), 0644)
			case "preload-symlink":
				err = os.Symlink("../usr/lib64/libc.so.6", filepath.Join(f.root, "etc/ld.so.preload"))
			case "changed-alias":
				err = os.Remove(filepath.Join(f.root, "lib64"))
				if err == nil {
					err = os.Symlink("/usr/lib64", filepath.Join(f.root, "lib64"))
				}
			case "unlisted-alias":
				err = os.Rename(filepath.Join(f.root, "usr/lib64"), filepath.Join(f.root, "usr/replaced"))
				if err == nil {
					err = os.Symlink("replaced", filepath.Join(f.root, "usr/lib64"))
				}
			case "hardlink":
				err = os.Link(library, filepath.Join(f.root, "extra-link"))
			case "missing-lock":
				err = os.Remove(lock)
			case "lock-symlink":
				err = os.Rename(lock, lock+".target")
				if err == nil {
					err = os.Symlink(".rpm.lock.target", lock)
				}
			case "bundle-replaced":
				f.area.location.Inode++
			case "partial-preload":
				f.requirement.Preload = f.requirement.Preload[:1]
			case "duplicate-preload":
				f.requirement.Preload[1] = f.requirement.Preload[0]
			case "canceled":
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			err = f.guard.WithPython(ctx, &f.area, f.requirement, func(prerequisites.PythonLaunch, func() error) error {
				t.Fatal("unqualified execution reached callback")
				return nil
			})
			if err == nil {
				t.Fatal("unqualified execution was accepted")
			}
		})
	}
}
