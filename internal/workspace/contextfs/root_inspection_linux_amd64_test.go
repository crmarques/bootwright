//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A setup dry run reports the state root this build would use, read without
// privilege and without changing it: an absent root setup creates, a root
// another process may not list as far as it can see, and every root this
// build cannot use with the refusal each store command gives it (B349).
func TestTheStateRootInspectionNamesARootThisBuildCannotUse(t *testing.T) {
	owner := ownerText(uint32(os.Geteuid()), uint32(os.Getegid()))
	required := "a directory owned by " + owner + " with mode 0700 on a local filesystem"
	for _, test := range []struct {
		name                 string
		lay                  func(t *testing.T, store *Store, sources desiredstate.Sources)
		listable             bool
		observed, status     string
		message, remediation string
	}{
		{name: "an absent root", lay: func(*testing.T, *Store, desiredstate.Sources) {}, listable: true, observed: "absent; setup creates it", status: "ready"},
		{name: "a root this build initialized", lay: func(t *testing.T, store *Store, sources desiredstate.Sources) {
			publish(t, store, "alpha", sources)
		}, listable: true, observed: owner + " 0700", status: "ready"},
		{name: "a root this process may not list", lay: func(t *testing.T, store *Store, _ desiredstate.Sources) {
			if err := os.Mkdir(store.options.Root, 0700); err != nil {
				t.Fatal(err)
			}
		}, observed: owner + " 0700; setup verifies its contents", status: "ready"},
		{name: "a root another process may read", lay: func(t *testing.T, store *Store, _ desiredstate.Sources) {
			if err := os.Mkdir(store.options.Root, 0755); err != nil {
				t.Fatal(err)
			}
		}, listable: true, observed: "a directory owned by " + owner + " with mode 0755", status: "not-ready",
			message: "the state root is a directory owned by " + owner + " with mode 0755, but it must be a directory owned by " + owner + " with mode 0700", remediation: unsafeRootRemediation},
		{name: "a file in place of the root", lay: func(t *testing.T, store *Store, _ desiredstate.Sources) {
			writePrivate(t, store.options.Root, []byte("state\n"))
		}, listable: true, observed: "a regular file owned by " + owner + " with mode 0600", status: "not-ready",
			message: "the state root is a regular file owned by " + owner + " with mode 0600, but it must be a directory owned by " + owner + " with mode 0700", remediation: unsafeRootRemediation},
		{name: "an earlier build's root", lay: func(t *testing.T, store *Store, _ desiredstate.Sources) {
			if err := os.MkdirAll(filepath.Join(store.options.Root, "contexts", "legacy"), 0700); err != nil {
				t.Fatal(err)
			}
			writePrivate(t, filepath.Join(store.options.Root, "contexts.yaml"), []byte("contexts: []\n"))
			writePrivate(t, filepath.Join(store.options.Root, ".bootwright-managed-root"), []byte("managed\n"))
		}, listable: true, observed: owner + " 0700 holding state this build cannot read", status: "not-ready",
			message: missingRegistryMessage, remediation: storeRecoveryRemediation},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, sources := fixture(t)
			test.lay(t, store, sources)
			before := inspectedEntries(t, store.options.Root)
			path, err := store.rootPath()
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := store.inspectStateRoot(context.Background(), path, uint32(os.Geteuid()), uint32(os.Getegid()), test.listable)
			if err != nil {
				t.Fatal(err)
			}
			if inspection.Required != required || inspection.Observed != test.observed || inspection.Status != test.status {
				t.Fatalf("inspection = %+v, want %q %q", inspection, test.observed, test.status)
			}
			reported := diagnostics.Of(inspection.Refusal)
			if test.message == "" {
				if inspection.Refusal != nil {
					t.Fatalf("a usable root refused: %#v", reported)
				}
			} else if len(reported) != 1 || reported[0].Code != "context.state" || reported[0].Message != test.message || reported[0].Remediation != test.remediation {
				t.Fatalf("refusal = %#v, want %q with %q", reported, test.message, test.remediation)
			}
			if after := inspectedEntries(t, store.options.Root); !reflect.DeepEqual(before, after) {
				t.Fatalf("the inspection changed the root:\nbefore: %#v\nafter:  %#v", before, after)
			}
		})
	}
}

// inspectedEntries snapshots the root's parent, and the root itself when it is
// a directory, so a test proves the inspection created and changed nothing.
func inspectedEntries(t *testing.T, root string) [][]rootEntrySnapshot {
	t.Helper()
	entries := [][]rootEntrySnapshot{snapshotRootEntries(t, filepath.Dir(root))}
	if info, err := os.Lstat(root); err == nil && info.IsDir() {
		entries = append(entries, snapshotRootEntries(t, root))
	}
	return entries
}

// The inspection a dry run calls is the owner's own: it judges the root for
// the identity store commands require and lists it only as that identity.
func TestTheStateRootInspectionIsTheStoresOwn(t *testing.T) {
	store, _ := fixture(t)
	if err := os.Mkdir(store.options.Root, 0755); err != nil {
		t.Fatal(err)
	}
	inspection, err := store.InspectStateRoot(context.Background())
	if err != nil || inspection.Status != "not-ready" || inspection.Refusal == nil {
		t.Fatalf("inspection = %+v (%v)", inspection, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.InspectStateRoot(canceled); err == nil {
		t.Fatal("a canceled inspection ran")
	}
}
