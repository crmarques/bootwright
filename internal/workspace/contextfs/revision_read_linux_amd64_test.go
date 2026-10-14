//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const missingRevision = "rev-0123456789abcdef0123456789abcdef"

func retainedFixture(t *testing.T) (*Store, contexts.Record, contexts.Record) {
	t.Helper()
	ctx := context.Background()
	store, sources := fixture(t)
	retained := publish(t, store, "example", sources)
	pending, err := reconciliation.Evidence{Operation: reconciliation.MutationPending, Ownership: reconciliation.OwnershipRetained}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		return tx.PublishEvidence(ctx, pending)
	}); err != nil {
		t.Fatal(err)
	}
	sources.Files[0] = desiredstate.NewSourceFile(sources.Files[0].Path(), []byte("version: updated\n"))
	if err := replaceInput(store, retained, sources); err != nil {
		t.Fatal(err)
	}
	selected := requireSelectedInput(t, store, retained, []byte("version: updated\n"), 2)
	if selected.Revision == retained.Revision {
		t.Fatal("the context update selected no new revision")
	}
	return store, retained, selected
}

func readRevision(t *testing.T, store *Store, id string) (desiredstate.Sources, error) {
	t.Helper()
	var sources desiredstate.Sources
	var readErr error
	if err := store.ReadLifecycle(context.Background(), "example", func(view lifecycle.View) error {
		sources, readErr = view.Revision(context.Background(), id)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return sources, readErr
}

func refusedWith(t *testing.T, err error, message string) {
	t.Helper()
	expectState(t, err)
	if got := diagnostics.Of(err)[0].Message; !strings.Contains(got, message) {
		t.Fatalf("refusal %q does not say %q", got, message)
	}
}

func retainedPath(store *Store, record contexts.Record, revision string, names ...string) string {
	return filepath.Join(append([]string{revisionDirectory(store, record), revision}, names...)...)
}

func replaceFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			if err := os.Mkdir(target, 0700); err != nil {
				return err
			}
			return os.Chmod(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestARetainedRevisionReadsItsOwnBytes(t *testing.T) {
	store, retained, _ := retainedFixture(t)
	if err := store.ReadLifecycle(context.Background(), "example", func(view lifecycle.View) error {
		sources, err := view.Revision(context.Background(), retained.Revision)
		if err != nil {
			t.Fatalf("the retained revision did not read: %+v", diagnostics.Of(err))
		}
		if len(sources.Files) != 1 || string(sources.Files[0].Bytes()) != "version: original\n" || sources.Files[0].Path() != view.Inputs().Files[0].Path() {
			t.Fatalf("the retained revision read %+v", sources)
		}
		if string(view.Inputs().Files[0].Bytes()) != "version: updated\n" {
			t.Fatalf("reading a retained revision changed the view's inputs to %q", view.Inputs().Files[0].Bytes())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTheSelectedRevisionReadsAsTheViewsInputs(t *testing.T) {
	store, _, selected := retainedFixture(t)
	if err := store.RunLifecycle(context.Background(), "example", func(view lifecycle.RunView) error {
		sources, err := view.Revision(context.Background(), selected.Revision)
		if err != nil || !reflect.DeepEqual(sources, view.Inputs()) {
			t.Fatalf("the selected revision read %+v (%v), want the view's inputs", sources, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestARevisionReadRefusesAnInvalidOrMissingIdentity(t *testing.T) {
	store, _, _ := retainedFixture(t)
	for id, message := range map[string]string{
		"":              "the input revision identity is invalid",
		"bad":           "the input revision identity is invalid",
		"rev-x/../y":    "the input revision identity is invalid",
		missingRevision: "referenced input revision is missing or unsafe",
	} {
		_, err := readRevision(t, store, id)
		refusedWith(t, err, message)
	}
}

func TestARevisionReadRefusesALinkedOrSubstitutedDirectory(t *testing.T) {
	t.Run("a linked revision directory", func(t *testing.T) {
		store, retained, _ := retainedFixture(t)
		path := retainedPath(store, retained, retained.Revision)
		aside := filepath.Join(filepath.Dir(store.options.Root), "aside")
		if err := os.Rename(path, aside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(aside, path); err != nil {
			t.Fatal(err)
		}
		_, err := readRevision(t, store, retained.Revision)
		refusedWith(t, err, "referenced input revision is missing or unsafe")
	})
	t.Run("a linked blob", func(t *testing.T) {
		store, retained, _ := retainedFixture(t)
		blob := retainedPath(store, retained, retained.Revision, blobName(0))
		aside := filepath.Join(filepath.Dir(store.options.Root), "blob")
		if err := os.Rename(blob, aside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(aside, blob); err != nil {
			t.Fatal(err)
		}
		_, err := readRevision(t, store, retained.Revision)
		refusedWith(t, err, "too many levels of symbolic links")
	})
	t.Run("a context directory substituted while the view is held", func(t *testing.T) {
		store, retained, _ := retainedFixture(t)
		contextPath := filepath.Join(store.options.Root, "contexts", "example")
		if err := store.ReadLifecycle(context.Background(), "example", func(view lifecycle.View) error {
			moved := filepath.Join(store.options.Root, "contexts", "moved")
			if err := os.Rename(contextPath, moved); err != nil {
				t.Fatal(err)
			}
			copyTree(t, moved, contextPath)
			_, err := view.Revision(context.Background(), retained.Revision)
			refusedWith(t, err, "context directory was replaced")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestARevisionReadRefusesAManifestOfAnotherContextOrRevision(t *testing.T) {
	t.Run("another revision", func(t *testing.T) {
		store, retained, selected := retainedFixture(t)
		data, err := os.ReadFile(retainedPath(store, retained, selected.Revision, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		replaceFile(t, retainedPath(store, retained, retained.Revision, "manifest.json"), data)
		_, err = readRevision(t, store, retained.Revision)
		refusedWith(t, err, "input manifest identity is invalid")
	})
	t.Run("another context", func(t *testing.T) {
		store, retained, _ := retainedFixture(t)
		input := filepath.Join(filepath.Dir(store.options.Root), "other-input")
		if err := os.Mkdir(input, 0700); err != nil {
			t.Fatal(err)
		}
		other := publish(t, store, "other", desiredstate.Sources{Roots: []string{input}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile(filepath.Join(input, "environment.yaml"), []byte("version: other\n")),
		}, Markers: []desiredstate.SourceFile{}})
		copyTree(t, retainedPath(store, other, other.Revision), retainedPath(store, retained, other.Revision))
		_, err := readRevision(t, store, other.Revision)
		refusedWith(t, err, "input manifest identity is invalid")
	})
}

func TestARevisionReadRefusesATamperedBlob(t *testing.T) {
	store, retained, _ := retainedFixture(t)
	replaceFile(t, retainedPath(store, retained, retained.Revision, blobName(0)), []byte("version: tampered\n"))
	_, err := readRevision(t, store, retained.Revision)
	refusedWith(t, err, "frozen input length, digest or encoding is inconsistent")
}

func TestARevisionReadRefusesAfterItsCallbackReturned(t *testing.T) {
	store, retained, selected := retainedFixture(t)
	var inspection lifecycle.View
	var bounded lifecycle.RunView
	if err := store.ReadLifecycle(context.Background(), "example", func(view lifecycle.View) error {
		inspection = view
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RunLifecycle(context.Background(), "example", func(view lifecycle.RunView) error {
		bounded = view
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, view := range []lifecycle.View{inspection, bounded} {
		for _, id := range []string{retained.Revision, selected.Revision} {
			_, err := view.Revision(context.Background(), id)
			refusedWith(t, err, "bounded lifecycle capability has closed")
		}
	}
}

type treeEntry struct {
	mode     uint32
	inode    uint64
	size     int64
	modified syscall.Timespec
	changed  syscall.Timespec
	link     string
	data     []byte
}

func storeTree(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	tree := map[string]treeEntry{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		item := treeEntry{mode: stat.Mode, inode: stat.Ino, size: stat.Size, modified: stat.Mtim, changed: stat.Ctim}
		if info.Mode().IsRegular() {
			item.data, err = os.ReadFile(path)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			item.link, err = os.Readlink(path)
		}
		tree[path] = item
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestARevisionReadWritesNothing(t *testing.T) {
	store, retained, selected := retainedFixture(t)
	before := storeTree(t, store.options.Root)
	for _, id := range []string{retained.Revision, selected.Revision, missingRevision, "bad"} {
		_, _ = readRevision(t, store, id)
	}
	if err := store.RunLifecycle(context.Background(), "example", func(view lifecycle.RunView) error {
		_, err := view.Revision(context.Background(), retained.Revision)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	after := storeTree(t, store.options.Root)
	if len(before) != len(after) {
		t.Fatalf("the store held %d entries before the reads and %d after", len(before), len(after))
	}
	for path, entry := range before {
		other, found := after[path]
		if !found || other.mode != entry.mode || other.inode != entry.inode || other.size != entry.size || other.modified != entry.modified || other.changed != entry.changed || other.link != entry.link || !bytes.Equal(other.data, entry.data) {
			t.Fatalf("a revision read changed %s", path)
		}
	}
}
