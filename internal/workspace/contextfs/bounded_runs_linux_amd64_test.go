//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func boundedRunName(number int) string { return fmt.Sprintf("run-%06d.output", number) }

// runsRoot is where this store keeps one context's bounded runs. A test plants
// runs there as earlier runs left them.
func runsRoot(t *testing.T, store *Store, name string) string {
	t.Helper()
	root := filepath.Join(store.options.Root+logsSuffix, logContexts, name, runsSubtree)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func plantBoundedRun(t *testing.T, root string, number int) string {
	t.Helper()
	run := filepath.Join(root, boundedRunName(number))
	writePrivate(t, run, []byte("what the adapter printed\n"))
	return run
}

func openRun(t *testing.T, store *Store) string {
	t.Helper()
	ctx := context.Background()
	var opened string
	if err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
		name, err := view.OpenRun(ctx)
		opened = name
		return err
	}); err != nil {
		t.Fatalf("a run did not open: %#v", diagnostics.Of(err))
	}
	return opened
}

func heldNamesOf(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

// Before a run opens, the lowest-numbered runs make room, the new run takes
// the number past the highest, and an entry that is no run is neither counted
// nor removed.
func TestABoundedRunKeepsTheNewestRuns(t *testing.T) {
	store, _ := lifecycleFixture(t)
	root := runsRoot(t, store, "example")
	for number := 1; number <= 20; number++ {
		plantBoundedRun(t, root, number)
	}
	writePrivate(t, filepath.Join(root, "notes"), []byte("an operator's notes\n"))
	if opened := openRun(t, store); opened != boundedRunName(21) {
		t.Fatalf("the run opened %s, want %s", opened, boundedRunName(21))
	}
	want := []string{"notes"}
	for number := 21 - maxBoundedRuns + 1; number <= 21; number++ {
		want = append(want, boundedRunName(number))
	}
	slices.Sort(want)
	if held := heldNamesOf(t, root); !slices.Equal(held, want) {
		t.Fatalf("the runs area holds %v, want the %d newest runs and the notes: %v", held, maxBoundedRuns, want)
	}
}

// Runs opened together under the shared lease take distinct numbers, from
// one upward with no gap.
func TestConcurrentBoundedRunsTakeDistinctSequentialNumbers(t *testing.T) {
	store, _ := lifecycleFixture(t)
	const runs = maxBoundedRuns - 1
	opened := make([]string, runs)
	var group sync.WaitGroup
	for index := range runs {
		group.Go(func() { opened[index] = openRun(t, store) })
	}
	group.Wait()
	want := []string{}
	for number := 1; number <= runs; number++ {
		want = append(want, boundedRunName(number))
	}
	if slices.Sort(opened); !slices.Equal(opened, want) {
		t.Fatalf("concurrent runs opened %v, want %v", opened, want)
	}
}

// However many runs an area holds, the next one opens past them and leaves
// only the newest, so machine power commands keep working however many ran
// before.
func TestBoundedRunsKeepWorkingHoweverManyRan(t *testing.T) {
	store, _ := lifecycleFixture(t)
	root := runsRoot(t, store, "example")
	for number := 1; number <= 2*maxOperationEntries/8; number++ {
		plantBoundedRun(t, root, number)
	}
	opened := openRun(t, store)
	if held := heldNamesOf(t, root); len(held) != maxBoundedRuns || opened != boundedRunName(2*maxOperationEntries/8+1) || held[len(held)-1] != opened {
		t.Fatalf("the runs area holds %d runs ending %v after opening %s", len(held), held[len(held)-1:], opened)
	}
}

// A run another live run holds is never retired, however low its number: it
// keeps everything written to it while newer runs come and go, and is retired
// only once its own callback returned.
func TestARunInProgressIsNeverRetired(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	root := runsRoot(t, store, "example")
	var progress string
	err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
		name, err := view.OpenRun(ctx)
		if err != nil {
			return err
		}
		progress = name
		if err := view.Runs().Append(ctx, name, []byte("before\n")); err != nil {
			return err
		}
		for range maxBoundedRuns + 1 {
			openRun(t, store)
		}
		if err := view.Runs().Append(ctx, name, []byte("after\n")); err != nil {
			return err
		}
		data, found, err := view.Runs().Read(ctx, name, 64)
		if err != nil || !found || string(data) != "before\nafter\n" {
			t.Fatalf("the run in progress holds %q %t (%v), want everything it wrote", data, found, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the run in progress failed: %#v", diagnostics.Of(err))
	}
	openRun(t, store)
	if _, err := os.Lstat(filepath.Join(root, progress)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the lowest run outlived its callback and newer runs (%v)", err)
	}
}

// Retention removes a run only as the store left it: a private, singly linked
// regular file. A run that is a link, is linked elsewhere or holds another
// mode is left exactly as it is, and the new run still opens beside it.
func TestARetiredRunIsRemovedOnlyAsTheStoreLeftIt(t *testing.T) {
	for _, test := range []struct {
		name  string
		leave func(t *testing.T, run string)
	}{
		{"a run that is a link", func(t *testing.T, run string) {
			target := filepath.Join(t.TempDir(), "elsewhere")
			writePrivate(t, target, []byte("not the run's own\n"))
			if err := os.Remove(run); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, run); err != nil {
				t.Fatal(err)
			}
		}},
		{"a run linked elsewhere", func(t *testing.T, run string) {
			if err := os.Link(run, filepath.Join(filepath.Dir(run), "linked")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a run of another mode", func(t *testing.T, run string) {
			if err := os.Chmod(run, 0644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _ := lifecycleFixture(t)
			root := runsRoot(t, store, "example")
			left := plantBoundedRun(t, root, 1)
			for number := 2; number <= maxBoundedRuns; number++ {
				plantBoundedRun(t, root, number)
			}
			test.leave(t, left)
			before, err := os.Lstat(left)
			if err != nil {
				t.Fatal(err)
			}
			opened := openRun(t, store)
			after, err := os.Lstat(left)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("the run was changed (%v)", err)
			}
			held := heldNamesOf(t, root)
			if slices.Contains(held, boundedRunName(2)) || !slices.Contains(held, opened) || opened != boundedRunName(maxBoundedRuns+1) {
				t.Fatalf("the runs area holds %v after opening %s, want the left run, the new one and all but the lowest store-made", held, opened)
			}
		})
	}
}

// The highest number a run takes is the last: an area holding it refuses the
// next run, naming the directory to remove.
func TestBoundedRunNumbersRefuseOnceExhausted(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	root := runsRoot(t, store, "example")
	plantBoundedRun(t, root, 999999)
	err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
		_, err := view.OpenRun(ctx)
		return err
	})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Remediation != "remove "+root+", then repeat the command" {
		t.Fatalf("an exhausted runs area reported %v %#v", err, reported)
	}
}

// A run whose file exists but cannot be handed out, because making it durable
// failed or the run was interrupted, leaves no file behind.
func TestARunThatCannotOpenLeavesNoFile(t *testing.T) {
	injected := errors.New("the runs area could not be made durable")
	for _, test := range []struct {
		name string
		fail func(cancel context.CancelFunc) error
		want error
	}{
		{"its file cannot be made durable", func(context.CancelFunc) error { return injected }, injected},
		{"an interrupt once its file exists", func(cancel context.CancelFunc) error {
			cancel()
			return context.Canceled
		}, context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store, _ := lifecycleFixture(t)
			run := filepath.Join(runsRoot(t, store, "example"), boundedRunName(1))
			created, existed := false, false
			var err error
			_ = store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
				store.fail = func(point string) error {
					switch {
					case point == string(checkpointCreateFile):
						created = true
					case created && point == string(checkpointSyncDirectory):
						created = false
						info, _ := os.Lstat(run)
						existed = info != nil
						return test.fail(cancel)
					}
					return nil
				}
				defer func() { store.fail = nil }()
				_, err = view.OpenRun(ctx)
				return err
			})
			if !errors.Is(err, test.want) || !existed {
				t.Fatalf("the run = %v, its file there when it failed %t; want %v once it existed", err, existed, test.want)
			}
			if _, err := os.Lstat(run); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the run that could not open left its file (%v)", err)
			}
		})
	}
}
