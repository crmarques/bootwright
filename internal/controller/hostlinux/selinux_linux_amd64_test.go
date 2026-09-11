//go:build linux && amd64

package hostlinux

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"golang.org/x/sys/unix"
)

func TestRuntimeVerifiesActiveEnforcingPolicyWithoutWrites(t *testing.T) {
	i, requirement := selinuxFixture(t)
	before := snapshot(t, i.view.root)
	observed, err := i.Runtime(context.Background(), requirement)
	if err != nil || !observed.Present || !observed.Ready {
		t.Fatal(observed, err)
	}
	if !reflect.DeepEqual(before, snapshot(t, i.view.root)) {
		t.Fatal("SELinux inspection changed metadata")
	}
}

func TestRuntimeRefusesUnqualifiedOrChangingSELinuxState(t *testing.T) {
	for _, variant := range []string{"missing", "wrong-filesystem", "permissive", "enforce-not-canonical", "empty-policy", "unloaded-policy", "oversized-policy", "mutable-policy", "symlink-policy", "short-status", "odd-sequence", "wrong-version", "reload-during-files", "permissive-during-files"} {
		t.Run(variant, func(t *testing.T) {
			i, requirement := selinuxFixture(t)
			switch variant {
			case "missing":
				must(t, os.Remove(filepath.Join(i.view.root, "sys/fs/selinux/enforce")))
			case "wrong-filesystem":
				i.view.filesystem = nil
			case "permissive":
				replaceSELinuxFile(t, i, "enforce", []byte("0"))
			case "enforce-not-canonical":
				replaceSELinuxFile(t, i, "enforce", []byte("1\n"))
			case "empty-policy":
				replaceSELinuxFile(t, i, "policy", nil)
			case "unloaded-policy":
				replaceSELinuxFile(t, i, "status", selinuxStatus(1, 2, 1, 0))
			case "oversized-policy":
				name := filepath.Join(i.view.root, "sys/fs/selinux/policy")
				must(t, os.Chmod(name, 0644))
				must(t, os.Truncate(name, maxSELinuxPolicyBytes+1))
			case "mutable-policy":
				must(t, os.Chmod(filepath.Join(i.view.root, "sys/fs/selinux/policy"), 0666))
			case "symlink-policy":
				name := filepath.Join(i.view.root, "sys/fs/selinux/policy")
				must(t, os.Rename(name, name+".real"))
				must(t, os.Symlink("policy.real", name))
			case "short-status":
				replaceSELinuxFile(t, i, "status", make([]byte, 19))
			case "odd-sequence":
				replaceSELinuxFile(t, i, "status", selinuxStatus(1, 3, 1, 7))
			case "wrong-version":
				replaceSELinuxFile(t, i, "status", selinuxStatus(2, 2, 1, 7))
			case "reload-during-files", "permissive-during-files":
				i.view.stat = func(name string, _ *unix.Stat_t) {
					if name == "/usr/lib/podman/support" {
						if variant == "reload-during-files" {
							replaceSELinuxFile(t, i, "status", selinuxStatus(1, 4, 1, 8))
						} else {
							replaceSELinuxFile(t, i, "enforce", []byte("0"))
						}
					}
				}
			}
			observed, err := i.Runtime(context.Background(), requirement)
			if err != nil || !observed.Present || observed.Ready {
				t.Fatal("unqualified policy accepted", observed, err)
			}
		})
	}
}

func TestRuntimeSELinuxManifestAndCancellationBoundaries(t *testing.T) {
	i, requirement := selinuxFixture(t)
	requirement.SELinuxMode = "permissive"
	_, err := i.Runtime(context.Background(), requirement)
	assertDiagnostic(t, err, "controller.unsupported")
	i, requirement = selinuxFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	filesystem := i.view.filesystem
	i.view.filesystem = func(name string, actual int64) int64 {
		if name == "/sys/fs/selinux/policy" {
			cancel()
		}
		return filesystem(name, actual)
	}
	_, err = i.Runtime(ctx, requirement)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("policy cancellation lost", err)
	}
}

func selinuxFixture(t *testing.T) (Inspector, prerequisites.RuntimeRequirement) {
	t.Helper()
	i := fixture(t, "btrfs")
	requirement := runtimeFixture(t, i)
	filesystem := i.view.filesystem
	i.view.filesystem = func(name string, actual int64) int64 {
		if name == "/sys/fs/selinux" || strings.HasPrefix(name, "/sys/fs/selinux/") {
			return unix.SELINUX_MAGIC
		}
		return filesystem(name, actual)
	}
	writeFixture(t, i, "/sys/fs/selinux/enforce", "1", 0644)
	writeFixture(t, i, "/sys/fs/selinux/status", string(selinuxStatus(1, 2, 1, 7)), 0444)
	policy := "qualified synthetic SELinux policy"
	writeFixture(t, i, "/sys/fs/selinux/policy", policy, 0444)
	requirement.SELinuxMode = "enforcing"
	return i, requirement
}

func selinuxStatus(version, sequence, enforcing, policyload uint32) []byte {
	data := make([]byte, 20)
	for index, value := range []uint32{version, sequence, enforcing, policyload, 0} {
		binary.LittleEndian.PutUint32(data[index*4:], value)
	}
	return data
}

func replaceSELinuxFile(t *testing.T, i Inspector, name string, data []byte) {
	t.Helper()
	name = filepath.Join(i.view.root, "sys/fs/selinux", name)
	must(t, os.Chmod(name, 0644))
	must(t, os.WriteFile(name, data, 0644))
	must(t, os.Chmod(name, 0444))
}
