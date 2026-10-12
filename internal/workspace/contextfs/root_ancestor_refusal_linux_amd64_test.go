//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A state root reached through an ancestor that is a symbolic link or not a
// directory is refused naming that ancestor's absolute path, what it is and
// the exit, whether the store reads or creates its root, and nothing is
// created behind it. A root that is itself a link is a root of another type,
// refused exactly as setup's inspection refuses it, with the exit to replace
// it with a directory.
func TestAStateRootBehindALinkNamesTheAncestorAndItsExit(t *testing.T) {
	ctx := context.Background()
	uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
	for kind, prepare := range map[string]func(t *testing.T, base string) string{
		"a symbolic link": func(t *testing.T, base string) string {
			if err := os.Mkdir(filepath.Join(base, "real"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(base, "link")
		},
		"a regular file": func(t *testing.T, base string) string {
			if err := os.WriteFile(filepath.Join(base, "file"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(base, "file")
		},
	} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			ancestor := prepare(t, base)
			store := New(testOptions(filepath.Join(ancestor, "state")))
			want := []diagnostics.Diagnostic{{
				Severity: "error", Code: "context.state",
				Message: "the state root is reached through " + ancestor + ", which is " + kind +
					", but the store reaches its root only through directories and never follows a symbolic link",
				Remediation: "replace " + ancestor + " with a directory, such as the directory a symbolic link there names or a mount of that storage, then repeat the command",
			}}
			_, viewErr := store.View(ctx)
			_, openErr := store.openRoot(ctx, true, nil)
			for name, err := range map[string]error{"view": viewErr, "create": openErr} {
				if got := diagnostics.Of(err); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s refused with %#v, want %#v", name, got, want)
				}
			}
			if real := filepath.Join(base, "real"); kind == "a symbolic link" {
				if names := listedNames(t, real); len(names) != 0 {
					t.Fatalf("the store created %v behind the link", names)
				}
			}
		})
	}
	t.Run("the root itself", func(t *testing.T) {
		base := t.TempDir()
		if err := os.Mkdir(filepath.Join(base, "real"), 0700); err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(base, "link")
		if err := os.Symlink(filepath.Join(base, "real"), root); err != nil {
			t.Fatal(err)
		}
		var lstat syscall.Stat_t
		if err := syscall.Lstat(root, &lstat); err != nil {
			t.Fatal(err)
		}
		store := New(testOptions(root))
		_, viewErr := store.View(ctx)
		want := diagnostics.Of(unsafeRoot(lstat, uid, gid))
		if len(want) == 1 {
			want[0].Remediation = "replace " + root + " with a directory, such as the directory the link names or a mount of that storage, then repeat the command"
		}
		if got := diagnostics.Of(viewErr); len(want) != 1 || !reflect.DeepEqual(got, want) {
			t.Fatalf("view refused with %#v, want %#v", got, want)
		}
		inspection, err := store.inspectStateRoot(ctx, root, uid, gid, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := diagnostics.Of(inspection.Refusal); !reflect.DeepEqual(got, want) {
			t.Fatalf("the inspection refused with %#v, want the store's %#v", got, want)
		}
	})
}
