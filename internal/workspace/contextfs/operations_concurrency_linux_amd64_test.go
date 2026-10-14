//go:build linux && amd64

package contextfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Blocks of one wave create their logs beneath directories none of them has
// made yet, so every writer races every other one to create each shared
// ancestor, starting with the operations subtree itself. Each round releases
// its writers together, and every one of them must succeed: a directory a
// concurrent writer created first is opened as if it had existed.
func TestConcurrentWritersShareTheDirectoriesTheyCreate(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	const rounds, writers = 8, 16
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area := tx.Operations()
		for round := range rounds {
			start := make(chan struct{})
			failures := make([]error, writers)
			var group sync.WaitGroup
			for writer := range writers {
				group.Add(1)
				go func() {
					defer group.Done()
					<-start
					target := fmt.Sprintf("op-%02d/logs/blocks/block-%02d", round, writer)
					if writer%2 == 0 {
						target = fmt.Sprintf("op-%02d/logs/blocks", round)
					}
					failures[writer] = area.EnsureDirectory(ctx, target)
				}()
			}
			close(start)
			group.Wait()
			for writer, err := range failures {
				if err != nil {
					t.Errorf("round %d writer %d: %s", round, writer, causeText(err))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.rootPath()
	if err != nil {
		t.Fatal(err)
	}
	for round := range rounds {
		for writer := 1; writer < writers; writer += 2 {
			stat, err := os.Stat(filepath.Join(root, "contexts", "example", "state", "operations", fmt.Sprintf("op-%02d/logs/blocks/block-%02d", round, writer)))
			if err != nil || !stat.IsDir() || stat.Mode().Perm() != 0700 {
				t.Fatalf("round %d writer %d left %v (%v)", round, writer, stat, err)
			}
		}
	}
}
