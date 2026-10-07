//go:build linux && amd64

package hostlinux

import (
	"context"
	"errors"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testMachineID = "1234567890abcdef1234567890abcdef"
const testProductID = "12345678-90ab-cdef-1234-567890abcdef"
const testRootUUID = "98765432-10ab-cdef-1234-567890abcdef"

func TestInstalledHostIdentityUsesTrustedLocalEvidence(t *testing.T) {
	for _, kind := range []string{"btrfs", "xfs", "ext4"} {
		t.Run(kind, func(t *testing.T) {
			i := fixture(t, kind)
			before := snapshot(t, i.view.root)
			platform, err := i.Platform(context.Background())
			if err != nil || platform.OS != "fedora" || platform.Release != "43" {
				t.Fatal(platform, err)
			}
			identity, err := i.Identity(context.Background())
			if err != nil || identity.MachineID() != testMachineID || identity.ProductUUID() != testProductID || identity.FilesystemUUID() != testRootUUID {
				t.Fatal("identity observation failed", err)
			}
			after := snapshot(t, i.view.root)
			if len(before) != len(after) {
				t.Fatal("inspection created filesystem state")
			}
			for name, metadata := range before {
				if after[name] != metadata {
					t.Fatal("inspection changed filesystem state")
				}
			}
		})
	}
}

func TestInstalledHostIdentityRefusesUntrustedOrAmbiguousEvidence(t *testing.T) {
	for _, edit := range []func(*testing.T, Inspector){
		func(t *testing.T, i Inspector) { writeFixture(t, i, "/.dockerenv", "", 0644) },
		func(t *testing.T, i Inspector) { must(t, os.Chmod(filepath.Join(i.view.root, "etc/machine-id"), 0666)) },
		func(t *testing.T, i Inspector) { writeFixture(t, i, "/proc/self/status", "NSpid:\t1\t2\n", 0644) },
		func(t *testing.T, i Inspector) { writeFixture(t, i, "/proc/1/cgroup", "0::/libpod-container\n", 0644) },
		func(t *testing.T, i Inspector) {
			writeFixture(t, i, "/proc/self/mountinfo", mountLine("btrfs")+mountLine("btrfs"), 0644)
		},
		func(t *testing.T, i Inspector) {
			must(t, os.Remove(filepath.Join(i.view.root, "proc/1/ns/mnt")))
			writeFixture(t, i, "/proc/1/ns/mnt", "different", 0644)
		},
		func(t *testing.T, i Inspector) {
			must(t, os.Symlink("../../vda4", filepath.Join(i.view.root, "dev/disk/by-uuid/12345678-90ab-cdef-1234-567890abcdef")))
		},
	} {
		i := fixture(t, "btrfs")
		edit(t, i)
		_, err := i.Identity(context.Background())
		assertDiagnostic(t, err, "controller.identity")
	}
}

func TestPlatformRejectsAmbiguousDeclaration(t *testing.T) {
	i := fixture(t, "btrfs")
	writeFixture(t, i, "/usr/lib/os-release", "ID=fedora\nID=rhel\nVERSION_ID=43\n", 0644)
	_, err := i.Platform(context.Background())
	assertDiagnostic(t, err, "controller.unsupported")
}

func TestInspectionCancellationPreservesCause(t *testing.T) {
	i := fixture(t, "btrfs")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, platformErr := i.Platform(ctx)
	_, identityErr := i.Identity(ctx)
	for _, err := range []error{platformErr, identityErr} {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	}
}

func fixture(t *testing.T, kind string) Inspector {
	t.Helper()
	i := Inspector{view: filesystemView{root: t.TempDir(), owner: uint32(os.Getuid()), architecture: "amd64"}}
	i.view.stat = func(name string, stat *unix.Stat_t) {
		if name == "/dev/vda4" {
			stat.Mode = unix.S_IFBLK | 0600
			stat.Rdev = unix.Mkdev(8, 4)
		}
	}
	i.view.filesystem = func(name string, actual int64) int64 {
		switch {
		case name == "/":
			return map[string]int64{"btrfs": unix.BTRFS_SUPER_MAGIC, "xfs": unix.XFS_SUPER_MAGIC, "ext4": unix.EXT4_SUPER_MAGIC}[kind]
		case strings.HasSuffix(name, "/ns/mnt"):
			return unix.NSFS_MAGIC
		case name == "/proc" || strings.HasPrefix(name, "/proc/"):
			return unix.PROC_SUPER_MAGIC
		case name == "/sys" || strings.HasPrefix(name, "/sys/"):
			return unix.SYSFS_MAGIC
		default:
			return actual
		}
	}
	writeFixture(t, i, "/usr/lib/os-release", "ID=fedora\nVERSION_ID=43\n", 0644)
	writeFixture(t, i, "/etc/machine-id", testMachineID+"\n", 0644)
	must(t, os.Symlink("../usr/lib/os-release", filepath.Join(i.view.root, "etc/os-release")))
	writeFixture(t, i, "/sys/devices/virtual/dmi/id/product_uuid", strings.ToUpper(testProductID)+"\n", 0400)
	must(t, os.MkdirAll(filepath.Join(i.view.root, "sys/class/dmi"), 0755))
	must(t, os.Symlink("../../devices/virtual/dmi/id", filepath.Join(i.view.root, "sys/class/dmi/id")))
	writeFixture(t, i, "/proc/self/status", "Name:\ttest\nNSpid:\t100\n", 0644)
	writeFixture(t, i, "/proc/1/cgroup", "0::/init.scope\n", 0644)
	writeFixture(t, i, "/proc/self/mountinfo", mountLine(kind), 0644)
	writeFixture(t, i, "/proc/self/ns/mnt", "namespace", 0644)
	must(t, os.MkdirAll(filepath.Join(i.view.root, "proc/1/ns"), 0755))
	must(t, os.Link(filepath.Join(i.view.root, "proc/self/ns/mnt"), filepath.Join(i.view.root, "proc/1/ns/mnt")))
	writeFixture(t, i, "/dev/vda4", "synthetic block device", 0600)
	must(t, os.MkdirAll(filepath.Join(i.view.root, "dev/disk/by-uuid"), 0755))
	must(t, os.Symlink("../../vda4", filepath.Join(i.view.root, "dev/disk/by-uuid", testRootUUID)))
	must(t, os.MkdirAll(filepath.Join(i.view.root, "run/systemd"), 0755))
	return i
}

func mountLine(kind string) string {
	if kind == "btrfs" {
		return "1 2 0:30 /root / rw - btrfs /dev/vda4 rw,subvol=/root\n"
	}
	return "1 2 8:4 / / rw - " + kind + " /dev/vda4 rw\n"
}

func writeFixture(t *testing.T, i Inspector, name, data string, mode os.FileMode) {
	t.Helper()
	name = filepath.Join(i.view.root, name)
	must(t, os.MkdirAll(filepath.Dir(name), 0755))
	must(t, os.WriteFile(name, []byte(data), mode))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertDiagnostic(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatal("unsafe inspection succeeded")
	}
	diagnostics := diagnostics.Of(err)
	if len(diagnostics) != 1 || diagnostics[0].Code != code {
		t.Fatalf("unexpected diagnostic: %#v", diagnostics)
	}
	for _, value := range []string{testMachineID, testProductID, testRootUUID} {
		if strings.Contains(err.Error(), value) || strings.Contains(diagnostics[0].Message, value) {
			t.Fatal("private evidence leaked in diagnostics")
		}
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	must(t, filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result[name] = info.Mode().String() + info.ModTime().String()
		return nil
	}))
	return result
}

// The FIPS mode is the kernel's own flag beneath /proc: 1 is enabled, 0 or a
// flag the kernel does not provide is disabled, and any other value, or a flag
// another account owns, refuses. Reading it changes nothing.
func TestFIPSModeReadsTheKernelFlag(t *testing.T) {
	for name, test := range map[string]struct {
		flag    string
		mode    os.FileMode
		enabled bool
		refused bool
	}{
		"enabled":         {flag: "1\n", mode: 0444, enabled: true},
		"disabled":        {flag: "0\n", mode: 0444},
		"absent":          {},
		"another value":   {flag: "2\n", mode: 0444, refused: true},
		"empty":           {flag: "", mode: 0444, refused: true},
		"writable by all": {flag: "1\n", mode: 0666, refused: true},
	} {
		t.Run(name, func(t *testing.T) {
			i := fixture(t, "xfs")
			if test.mode != 0 {
				writeFixture(t, i, "/proc/sys/crypto/fips_enabled", test.flag, test.mode)
				must(t, os.Chmod(filepath.Join(i.view.root, "proc/sys/crypto/fips_enabled"), test.mode))
			}
			before := snapshot(t, i.view.root)
			enabled, err := i.FIPSMode(context.Background())
			if test.refused {
				assertDiagnostic(t, err, "controller.unsupported")
				return
			}
			if err != nil || enabled != test.enabled {
				t.Fatalf("FIPS mode = %t (%v), want %t", enabled, err, test.enabled)
			}
			if after := snapshot(t, i.view.root); len(after) != len(before) {
				t.Fatal("reading the FIPS mode changed the filesystem")
			}
		})
	}
}
