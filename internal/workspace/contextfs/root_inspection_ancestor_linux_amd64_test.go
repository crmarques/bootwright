//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A root behind a symbolic link in its path is one the store refuses to open,
// so the dry run judges it as the store does, however the root itself stands,
// and creates nothing while it does.
func TestTheStateRootInspectionReachesTheRootAsTheStoreOpensIt(t *testing.T) {
	ctx := context.Background()
	for _, present := range []bool{true, false} {
		for _, owner := range []bool{false, true} {
			name := map[bool]string{true: "a present root", false: "an absent root"}[present] + map[bool]string{true: " inspected by its owner", false: " inspected by another process"}[owner]
			t.Run(name, func(t *testing.T) {
				base := t.TempDir()
				real := filepath.Join(base, "real")
				if err := os.Mkdir(real, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, filepath.Join(base, "link")); err != nil {
					t.Fatal(err)
				}
				if present {
					if err := os.Mkdir(filepath.Join(real, "state"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				root := filepath.Join(base, "link", "state")
				store := New(testOptions(root))
				before := listedNames(t, real)
				inspection, err := store.inspectStateRoot(ctx, root, uint32(os.Geteuid()), uint32(os.Getegid()), owner)
				if err != nil {
					t.Fatal(err)
				}
				if inspection.Status != "not-ready" || inspection.Observed != "behind a path every store command refuses to open" {
					t.Fatalf("inspection = %+v, want not-ready behind a path every store command refuses to open", inspection)
				}
				_, viewErr := store.View(ctx)
				if viewErr == nil {
					t.Fatal("the store opened a root behind a symbolic link")
				}
				if got, want := diagnostics.Of(inspection.Refusal), diagnostics.Of(viewErr); len(want) == 0 || !reflect.DeepEqual(got, want) {
					t.Fatalf("refusal = %#v, want the store's own %#v", got, want)
				}
				if after := listedNames(t, real); !reflect.DeepEqual(before, after) {
					t.Fatalf("the inspection changed %s: %v -> %v", real, before, after)
				}
			})
		}
	}
}

func listedNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
