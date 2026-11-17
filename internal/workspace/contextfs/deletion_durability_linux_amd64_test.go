//go:build linux && amd64

package contextfs

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A purge removes each directory's entries as one batch and makes that
// directory durable once after it, and once before the entry that identifies
// the context (state/reservation.json, then context.yaml), so that entry is
// never durably gone while another child is not. It syncs a directory once
// for each directory it empties and each such entry rather than once for each
// entry it unlinks.
func TestAPurgeSyncsEachDirectoryOnceAfterItsBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, sources := fixture(t)
	for index := range 12 {
		sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], fmt.Sprintf("part-%02d.yaml", index)), []byte("part: true\n")))
	}
	record := publish(t, store, "alpha", sources)
	entries, emptied := 0, 0
	if err := filepath.WalkDir(filepath.Join(store.options.Root, "contexts", record.Name), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		children, err := os.ReadDir(path)
		entries += len(children)
		if len(children) > 0 {
			emptied++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reached := map[checkpoint]int{}
	store.fail = func(point string) error {
		reached[checkpoint(point)]++
		return nil
	}
	err := store.TransactDeletion(ctx, record.Name, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(ctx, record.Name); err != nil {
			return err
		}
		return tx.Delete(ctx, record)
	})
	store.fail = nil
	if err != nil {
		t.Fatal(err)
	}
	if reached[checkpointBeforeContextUnlink] != entries {
		t.Fatalf("the purge unlinked %d entries, want the context's %d", reached[checkpointBeforeContextUnlink], entries)
	}
	t.Logf("entries %d emptied directories %d directory syncs %d", entries, emptied, reached[checkpointSyncDirectory])
	if outside := reached[checkpointSyncDirectory] - emptied - 2; outside != 5 {
		t.Fatalf("the purge synced a directory %d times for %d entries in %d directories, want once for each directory it empties, once before each of its 2 identifying entries and 5 times outside its walk", reached[checkpointSyncDirectory], entries, emptied)
	}
}
