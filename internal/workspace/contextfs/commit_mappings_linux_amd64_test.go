//go:build linux && amd64

package contextfs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A commit re-proves the mapping of each context the transaction changed or
// leased; a ready context it left alone was proven when the transaction began
// under the same exclusive lock, so the commit does not read its manifest
// again. The test runs alone so the process's read counter is its own.
func TestACommitDoesNotRereadAnUnchangedContextsManifest(t *testing.T) {
	ctx := context.Background()
	store, sources := fixture(t)
	for index := range 200 {
		sources.Files = append(sources.Files, desiredstate.NewSourceFile(filepath.Join(sources.Roots[0], fmt.Sprintf("part-%03d.yaml", index)), []byte("part: true\n")))
	}
	manifests := int64(0)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		record := publish(t, store, name, sources)
		info, err := os.Stat(filepath.Join(store.options.Root, "contexts", name, "desired-state", "revisions", record.Revision, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		manifests += info.Size()
	}
	var read int64
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		before := processReadBytes(t)
		err := tx.Commit(ctx, tx.Registry())
		read = processReadBytes(t) - before
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("the commit read %d bytes; the unchanged manifests hold %d", read, manifests)
	if read >= manifests {
		t.Fatalf("the commit read %d bytes, at least the %d its unchanged contexts' manifests hold", read, manifests)
	}
}

func processReadBytes(t *testing.T) int64 {
	t.Helper()
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Skipf("the process read counter is unavailable: %v", err)
	}
	for line := range bytes.Lines(data) {
		if value, found := bytes.CutPrefix(line, []byte("rchar: ")); found {
			count, err := strconv.ParseInt(string(bytes.TrimSpace(value)), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return count
		}
	}
	t.Fatal("/proc/self/io has no rchar")
	return 0
}
