//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// The production root holds the only ownership evidence of what its contexts
// realized, so a root on tmpfs, which a reboot empties, or on overlayfs, whose
// upper layer the kernel does not name, refuses context.state before anything
// is published beneath it, and before a missing root is created, naming the
// filesystem and the remedy, and the setup dry run's state-root check reports
// the same refusal. A root on ext4, XFS or Btrfs is used.
func TestAProductionRootOnATransientFilesystemRefusesBeforeAnyPublication(t *testing.T) {
	for _, test := range []struct {
		name  string
		magic int64
	}{
		{"tmpfs", 0x01021994},
		{"overlayfs", 0x794c7630},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, sources := fixture(t)
			store.rootFilesystem = func() int64 { return test.magic }
			path, err := store.rootPath()
			if err != nil {
				t.Fatal(err)
			}
			message := "the state root " + path + " is on " + test.name + ", which may not keep the ownership evidence it holds across a reboot"
			remediation := "place " + path + " on a persistent local filesystem, such as ext4, XFS or Btrfs, then repeat the command"
			refused := func(t *testing.T, err error) {
				t.Helper()
				reported := diagnostics.Of(err)
				if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != message || reported[0].Remediation != remediation {
					t.Fatalf("refusal = %#v, want %q with %q", reported, message, remediation)
				}
			}
			inspect := func(t *testing.T, observed string) {
				t.Helper()
				inspection, err := store.inspectStateRoot(context.Background(), path, uint32(os.Geteuid()), uint32(os.Getegid()), true)
				if err != nil {
					t.Fatal(err)
				}
				if inspection.Status != "not-ready" || inspection.Observed != observed {
					t.Fatalf("inspection = %+v, want not-ready %q", inspection, observed)
				}
				refused(t, inspection.Refusal)
			}
			inspect(t, "absent, beneath a directory on "+test.name)
			err = store.Transact(context.Background(), true, sources.Roots, func(tx contexts.Transaction) error {
				t.Fatal("a transaction ran over a production root on " + test.name)
				return nil
			})
			refused(t, err)
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("the refused transaction left the state root behind (%v)", err)
			}
			inspect(t, "absent, beneath a directory on "+test.name)
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			refused(t, store.Transact(context.Background(), true, sources.Roots, func(contexts.Transaction) error {
				t.Fatal("a transaction ran over an existing production root on " + test.name)
				return nil
			}))
			if entries, err := os.ReadDir(path); err != nil || len(entries) != 0 {
				t.Fatalf("the refused root holds %v (%v)", entries, err)
			}
			inspect(t, "on "+test.name)
		})
	}
	t.Run("ext4", func(t *testing.T) {
		store, sources := fixture(t)
		store.rootFilesystem = func() int64 { return 0xef53 }
		publish(t, store, "alpha", sources)
		path, err := store.rootPath()
		if err != nil {
			t.Fatal(err)
		}
		inspection, err := store.inspectStateRoot(context.Background(), path, uint32(os.Geteuid()), uint32(os.Getegid()), true)
		if err != nil || inspection.Status != "ready" || inspection.Refusal != nil {
			t.Fatalf("inspection of a root on ext4 = %+v, %v", inspection, err)
		}
	})
}

// An isolated test root injected at composition may lie on tmpfs, as /tmp does
// on many hosts.
func TestAnInjectedTestRootOnTmpfsIsUsed(t *testing.T) {
	var filesystem syscall.Statfs_t
	if err := syscall.Statfs("/tmp", &filesystem); err != nil || filesystem.Type != 0x01021994 {
		t.Skip("/tmp is not tmpfs here")
	}
	base, err := os.MkdirTemp("/tmp", "bootwright-contextfs-")
	if err != nil {
		t.Skipf("/tmp is not writable here: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	input := filepath.Join(base, "input")
	if err := os.Mkdir(input, 0700); err != nil {
		t.Fatal(err)
	}
	store := New(testOptions(filepath.Join(base, "state")))
	publish(t, store, "alpha", desiredstate.Sources{Roots: []string{input}, Files: []desiredstate.SourceFile{desiredstate.NewSourceFile(filepath.Join(input, "environment.yaml"), []byte("version: original\n"))}, Markers: []desiredstate.SourceFile{}})
}
