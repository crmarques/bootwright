//go:build linux && amd64

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// An entry of a context's tree the deletion cannot verify as the context's
// own refuses both deletions naming the context, the entry relative to the
// state root, why, and the exit; the deletion removes nothing, and once the
// entry is removed by hand the deletion completes.
func TestTheDeletionWalkNamesItsContextEntryAndExit(t *testing.T) {
	owner := strconv.Itoa(os.Geteuid()) + ":" + strconv.Itoa(os.Getegid())
	for name, test := range map[string]struct {
		damage    func(t *testing.T, base string) string
		reason    string
		removable bool
	}{
		"secrets symlink": {
			damage: func(t *testing.T, base string) string {
				entry := filepath.Join(base, "secrets", "unexpected")
				if err := os.Symlink(filepath.Join(base, "context.yaml"), entry); err != nil {
					t.Fatal(err)
				}
				return entry
			},
			reason: "it is a symbolic link", removable: true,
		},
		"secret mode": {
			damage: func(t *testing.T, base string) string {
				entry := filepath.Join(base, "secrets", "stray")
				if err := os.WriteFile(entry, []byte("stray"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(entry, 0644); err != nil {
					t.Fatal(err)
				}
				return entry
			},
			reason: "it is a regular file owned by " + owner + " with mode 0644",
		},
		"revisions mode": {
			damage: func(t *testing.T, base string) string {
				entry := filepath.Join(base, "desired-state", "revisions")
				if err := os.Chmod(entry, 0755); err != nil {
					t.Fatal(err)
				}
				return entry
			},
			reason: "it is a directory owned by " + owner + " with mode 0755",
		},
		"revision stray": {
			damage: func(t *testing.T, base string) string {
				revisions, err := os.ReadDir(filepath.Join(base, "desired-state", "revisions"))
				if err != nil || len(revisions) == 0 {
					t.Fatalf("the context holds no revision: %v (%v)", revisions, err)
				}
				entry := filepath.Join(base, "desired-state", "revisions", revisions[0].Name(), "stray")
				if err := os.WriteFile(entry, []byte("stray"), 0600); err != nil {
					t.Fatal(err)
				}
				return entry
			},
			reason: "it is not part of a context's layout", removable: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			services, repository, input, root := contextFixture(t)
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			path := test.damage(t, filepath.Join(root, "contexts", "alpha"))
			entry, err := filepath.Rel(root, path)
			if err != nil {
				t.Fatal(err)
			}
			want := "  error  context.state\n    context alpha cannot be verified: " + entry + ": " + test.reason + "\n    Next: remove " + entry +
				" beneath the state root, which this deletion never removes because it cannot verify it as the context's own, then repeat " +
				"bootwright context delete --name alpha --purge, adding --allow-orphans when the deletion abandons what the context owns; " +
				"or restore the whole store from a matching backup\n"
			for _, args := range [][]string{
				{"context", "delete", "--name", "alpha", "--purge", "--yes"},
				{"context", "delete", "--name", "alpha", "--purge", "--allow-orphans", "--yes"},
			} {
				if _, stderr := contextRun(t, services, 1, args...); humanContent(stderr) != humanContent(want) || strings.Contains(stderr, root) {
					t.Fatalf("%v refused with %q, want %q", args, stderr, want)
				}
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("a refusal removed %s (%v)", entry, err)
			}
			if !test.removable {
				return
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--yes")
			registry, err := repository.View(context.Background())
			if err != nil || len(registry.Contexts) != 0 {
				t.Fatalf("the registry still holds %+v (%v)", registry.Contexts, err)
			}
		})
	}
}

// A state root that is itself a symbolic link is refused as a root of another
// type, as setup reports it, rather than as an unreadable path, with the exit
// an ancestor link has: replacing it with a directory, never the guidance for
// a root nothing repairs.
func TestAStateRootThatIsALinkIsRefusedAsARootOfAnotherType(t *testing.T) {
	owner := strconv.Itoa(os.Geteuid()) + ":" + strconv.Itoa(os.Getegid())
	services, _, input, root := contextFixture(t)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	if err := os.Rename(root, root+"-real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root+"-real", root); err != nil {
		t.Fatal(err)
	}
	want := "  error  context.state\n    the state root is a symbolic link owned by " + owner + " with mode 0777, but it must be a directory owned by " +
		owner + " with mode 0700\n    Next: replace " + root + " with a directory, such as the directory the link names or a mount of that storage, then repeat the command\n"
	if _, stderr := contextRun(t, services, 1, "context", "list"); humanContent(stderr) != humanContent(want) {
		t.Fatalf("context list refused with %q, want %q", stderr, want)
	}
}
