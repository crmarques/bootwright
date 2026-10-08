//go:build linux && amd64

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Mutation evidence the store refuses to open as its own refuses an update,
// both deletions, status, apply and destroy naming the context, the entry and
// the exit; the deletion never removes it, and once it is removed by hand the
// acknowledged deletion abandons the context and leaves anything outside the
// store intact.
func TestUnsafeEvidenceIsRefusedNamingItsContextEntryAndExit(t *testing.T) {
	prefix := "[FAIL] context.state: context alpha cannot be verified: contexts/alpha/state/mutation.json: "
	suffix := "; next: remove contexts/alpha/state/mutation.json beneath the state root, which leaves nothing to prove what the context owns, " +
		"then abandon whatever it owns with bootwright context delete --name alpha --purge --allow-orphans; or restore the whole store from a matching backup\n"
	for name, damage := range map[string]func(evidence, outside string) error{
		"permissions": func(evidence, _ string) error { return os.Chmod(evidence, 0644) },
		"symlink": func(evidence, outside string) error {
			if err := os.Rename(evidence, outside); err != nil {
				return err
			}
			return os.Symlink(outside, evidence)
		},
		"hard link": func(evidence, outside string) error { return os.Link(evidence, outside) },
		"fifo": func(evidence, _ string) error {
			if err := os.Remove(evidence); err != nil {
				return err
			}
			return syscall.Mkfifo(evidence, 0600)
		},
		"directory": func(evidence, _ string) error {
			if err := os.Remove(evidence); err != nil {
				return err
			}
			return os.Mkdir(evidence, 0700)
		},
	} {
		t.Run(name, func(t *testing.T) {
			services, repository, input, root := contextFixture(t)
			contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
			evidence := filepath.Join(root, "contexts", "alpha", "state", "mutation.json")
			outside := filepath.Join(filepath.Dir(root), "outside.json")
			original, err := os.ReadFile(evidence)
			if err != nil {
				t.Fatal(err)
			}
			if err := damage(evidence, outside); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{
				{"context", "update", "--name", "alpha", "--input-dir", input, "--yes"},
				{"context", "delete", "--name", "alpha", "--purge", "--yes"},
				{"context", "delete", "--name", "alpha", "--purge", "--allow-orphans", "--yes"},
				{"status", "--context", "alpha"},
				{"apply", "--context", "alpha", "--yes"},
				{"destroy", "--context", "alpha", "--yes"},
			} {
				_, stderr := contextRun(t, services, 1, args...)
				if !strings.HasPrefix(stderr, prefix) || !strings.HasSuffix(stderr, suffix) || strings.Contains(stderr, root) {
					t.Fatalf("%v refused with %q", args, stderr)
				}
			}
			if _, err := os.Lstat(evidence); err != nil {
				t.Fatalf("a refusal removed the unsafe evidence (%v)", err)
			}
			if err := os.Remove(evidence); err != nil {
				t.Fatal(err)
			}
			if out, _ := contextRun(t, services, 0, "context", "delete", "--name", "alpha", "--purge", "--allow-orphans", "--yes"); !strings.Contains(out, "Orphans abandoned  true") {
				t.Fatalf("the acknowledged deletion reported %s", out)
			}
			registry, err := repository.View(context.Background())
			if err != nil || len(registry.Contexts) != 0 {
				t.Fatalf("the registry still holds %+v (%v)", registry.Contexts, err)
			}
			if _, err := os.Stat(filepath.Join(root, "contexts", "alpha")); !os.IsNotExist(err) {
				t.Fatalf("the abandoned context remains (%v)", err)
			}
			if name == "symlink" || name == "hard link" {
				if kept, err := os.ReadFile(outside); err != nil || string(kept) != string(original) {
					t.Fatalf("the out-of-store target changed: %q (%v)", kept, err)
				}
			}
		})
	}
}
