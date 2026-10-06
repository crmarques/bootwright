//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// measureCounter counts the entries the operation area's measuring walk
// reaches while it is switched on.
type measureCounter struct {
	counting bool
	reached  int
}

func countMeasures(store *Store) *measureCounter {
	counter := &measureCounter{counting: true}
	store.fail = func(point string) error {
		if counter.counting && point == string(checkpointMeasureOperationEntry) {
			counter.reached++
		}
		return nil
	}
	return counter
}

// measureProbe compares what a caching area counted with a fresh walk of its
// subtree, which the counter does not count.
func measureProbe(t *testing.T, ctx context.Context, area *operationArea, counter *measureCounter, after string) {
	t.Helper()
	area.measure.mutex.Lock()
	valid, entries, bytes := area.measure.valid, area.measure.entries, area.measure.bytes
	area.measure.mutex.Unlock()
	if !valid {
		t.Fatalf("after %s the area holds no measurement", after)
	}
	counter.counting = false
	defer func() { counter.counting = true }()
	measured, release, err := area.root(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	wantEntries, wantBytes, err := area.scan(ctx, measured, 0)
	if err != nil {
		t.Fatal(err)
	}
	if entries != wantEntries || bytes != wantBytes {
		t.Fatalf("after %s the area counted %d entries and %d bytes, a fresh walk finds %d and %d", after, entries, bytes, wantEntries, wantBytes)
	}
}

func publishEarlierOperation(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		if err := tx.Operations().WriteExclusive(ctx, "earlier.json", []byte("{}\n")); err != nil {
			return err
		}
		return tx.Operations().Append(ctx, "earlier/log.jsonl", []byte("{}\n"))
	}); err != nil {
		t.Fatalf("an earlier operation was not published: %#v", diagnostics.Of(err))
	}
}

// A transaction is its operation area's only writer, so the area walks its
// subtree at the first write and then counts each write it makes itself: the
// directories a write creates, a record's bytes, a replacement's change in
// size, a log's bytes and its creation, and each removal. What it counted is
// what a fresh walk finds.
func TestTheOperationAreaMeasuresItsSubtreeOncePerTransaction(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	publishEarlierOperation(t, store)
	counter := countMeasures(store)
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area := tx.Operations().(*operationArea)
		for index, target := range []string{"first.json", "second.json", "third.json"} {
			before := counter.reached
			if err := area.WriteExclusive(ctx, target, []byte("{}\n")); err != nil {
				return err
			}
			want := 0
			if index == 0 {
				want = 3
			}
			if walked := counter.reached - before; walked != want {
				t.Fatalf("write %d walked %d entries, want %d", index+1, walked, want)
			}
		}
		for _, step := range []struct {
			name string
			run  func() error
		}{
			{"three nested directories", func() error { return area.EnsureDirectory(ctx, "op-1/blocks/state") }},
			{"an exclusive record", func() error { return area.WriteExclusive(ctx, "op-1/record.json", []byte("{\"record\":1}\n")) }},
			{"a replacement of nothing", func() error { return area.Replace(ctx, "op-1/plan.json", []byte("{\"plan\":1}\n"), nil) }},
			{"a replacement of another size", func() error {
				return area.Replace(ctx, "first.json", []byte("{\"longer\":true}\n"), []byte("{}\n"))
			}},
			{"an append to a new log", func() error { return area.Append(ctx, "op-1/log.jsonl", []byte("one\n")) }},
			{"an append to an existing log", func() error { return area.Append(ctx, "op-1/log.jsonl", []byte("two two\n")) }},
			{"a record's removal", func() error { return area.RemoveRecord(ctx, "second.json", []byte("{}\n")) }},
			{"a directory's removal", func() error { return area.RemoveDirectory(ctx, "op-1/blocks/state") }},
		} {
			before := counter.reached
			if err := step.run(); err != nil {
				t.Fatalf("%s failed: %#v", step.name, diagnostics.Of(err))
			}
			if walked := counter.reached - before; walked != 0 {
				t.Fatalf("%s walked %d entries, want none", step.name, walked)
			}
			measureProbe(t, ctx, area, counter, step.name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the transaction failed: %#v", diagnostics.Of(err))
	}
}

// Counting refuses exactly where walking every write did: a write needs one
// entry free and room for its bytes, so the area fills to its bounds and the
// next write refuses, naming the operation area.
func TestTheIncrementalMeasureRefusesAtTheSameBound(t *testing.T) {
	ctx := context.Background()
	const refusal = "lifecycle operation storage has reached its bounds"
	t.Run("entries", func(t *testing.T) {
		store, _ := lifecycleFixture(t)
		root := operationsRoot(t, store, "example")
		for index := range maxOperationEntries - 3 {
			writePrivate(t, filepath.Join(root, fmt.Sprintf("planted-%05d", index)), nil)
		}
		err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
			for index := range 3 {
				if err := tx.Operations().WriteExclusive(ctx, fmt.Sprintf("record-%d.json", index), []byte("{}\n")); err != nil {
					t.Fatalf("write %d of the area's last free entries refused: %#v", index+1, diagnostics.Of(err))
				}
			}
			return tx.Operations().WriteExclusive(ctx, "record-3.json", []byte("{}\n"))
		})
		expectRefusal(t, err, refusal)
	})
	t.Run("bytes", func(t *testing.T) {
		store, _ := lifecycleFixture(t)
		planted := filepath.Join(operationsRoot(t, store, "example"), "planted.output")
		writePrivate(t, planted, nil)
		if err := os.Truncate(planted, maxOperationBytes-10); err != nil {
			t.Fatal(err)
		}
		err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
			area := tx.Operations()
			if err := area.WriteExclusive(ctx, "measured.json", nil); err != nil {
				t.Fatalf("an empty record refused: %#v", diagnostics.Of(err))
			}
			expectRefusal(t, area.Append(ctx, "log.jsonl", make([]byte, 11)), refusal)
			if err := area.Append(ctx, "log.jsonl", make([]byte, 10)); err != nil {
				t.Fatalf("an append that fills the area exactly refused: %#v", diagnostics.Of(err))
			}
			return area.Append(ctx, "log.jsonl", make([]byte, 1))
		})
		expectRefusal(t, err, refusal)
	})
}

// A write that fails may have landed, as a replacement whose rename succeeded
// before its read-back failed has, so the area forgets what it counted and the
// next write walks the subtree again.
func TestAFailedOperationWriteMeasuresAgain(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	publishEarlierOperation(t, store)
	counter := countMeasures(store)
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area := tx.Operations().(*operationArea)
		if err := area.WriteExclusive(ctx, "first.json", []byte("{}\n")); err != nil {
			return err
		}
		injected := errors.New("the replaced record could not be read back")
		fired := false
		store.fail = func(point string) error {
			if point == string(checkpointAfterOperationRename) && !fired {
				fired = true
				return injected
			}
			return nil
		}
		if err := area.Replace(ctx, "earlier.json", []byte("{\"replaced\":true}\n"), []byte("{}\n")); !errors.Is(err, injected) || !fired {
			t.Fatalf("the replacement = %v, fired %t", err, fired)
		}
		counter = countMeasures(store)
		if err := area.WriteExclusive(ctx, "second.json", []byte("{}\n")); err != nil {
			return err
		}
		if counter.reached != 4 {
			t.Fatalf("the write after a failed one walked %d entries, want the 4 the subtree holds", counter.reached)
		}
		measureProbe(t, ctx, area, counter, "a failed replacement")
		return nil
	})
	if err != nil {
		t.Fatalf("the transaction failed: %#v", diagnostics.Of(err))
	}
}
