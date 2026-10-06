//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func boundedRunName(number int) string { return fmt.Sprintf("run-%032x", number) }

// runsRoot is where this store keeps one context's bounded runs. A test plants
// runs there as earlier runs, or an earlier build, left them.
func runsRoot(t *testing.T, store *Store, name string) string {
	t.Helper()
	root := filepath.Join(store.options.Root, "contexts", name, "state", "runs")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

// plantBoundedRun leaves one run as a finished bounded run leaves it: its
// directory holding only its output, last changed at modified.
func plantBoundedRun(t *testing.T, root, name string, modified time.Time) string {
	t.Helper()
	run := filepath.Join(root, name)
	if err := os.Mkdir(run, 0700); err != nil {
		t.Fatal(err)
	}
	writePrivate(t, filepath.Join(run, lifecycle.RunOutputName), []byte("what the adapter printed\n"))
	if err := os.Chtimes(run, modified, modified); err != nil {
		t.Fatal(err)
	}
	return run
}

func openRun(t *testing.T, store *Store, identity string) {
	t.Helper()
	ctx := context.Background()
	if err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
		return view.OpenRun(ctx, identity)
	}); err != nil {
		t.Fatalf("run %s did not open: %#v", identity, diagnostics.Of(err))
	}
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

func TestBoundedRunRetentionFitsTheByteBound(t *testing.T) {
	if maxBoundedRuns*operationstore.MaxAdapterOutputBytes > operationstore.MaxBytes {
		t.Fatalf("%d runs of %d output bytes each exceed the runs area's %d bytes", maxBoundedRuns, operationstore.MaxAdapterOutputBytes, operationstore.MaxBytes)
	}
}

// Before a run opens, the oldest runs make room, by when each last changed and
// never by its random identity, and an entry that is no run is neither counted
// nor removed, even one older than every run and shaped exactly like one.
func TestABoundedRunKeepsTheNewestRuns(t *testing.T) {
	store, _ := lifecycleFixture(t)
	root := runsRoot(t, store, "example")
	base := time.Now().Add(-48 * time.Hour)
	planted := []string{}
	for index := range 20 {
		name := boundedRunName(100 - index)
		plantBoundedRun(t, root, name, base.Add(time.Duration(index)*time.Hour))
		planted = append(planted, name)
	}
	plantBoundedRun(t, root, "notes", base.Add(-time.Hour))
	opened := boundedRunName(1)
	openRun(t, store, opened)
	want := append(slices.Clone(planted[20-(maxBoundedRuns-1):]), opened, "notes")
	slices.Sort(want)
	if held := heldNamesOf(t, root); !slices.Equal(held, want) {
		t.Fatalf("the runs area holds %v, want the new run, the %d newest planted and the notes: %v", held, maxBoundedRuns-1, want)
	}
	if kept := heldNamesOf(t, filepath.Join(root, "notes")); !slices.Equal(kept, []string{lifecycle.RunOutputName}) {
		t.Fatalf("the notes hold %v, want them untouched", kept)
	}
}

// An area an earlier build filled to its entry bound with power runs no
// longer stops the next one: it opens, keeps its output and leaves only the
// newest runs.
func TestBoundedRunsKeepWorkingPastTheOldEntryWall(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	root := runsRoot(t, store, "example")
	base := time.Now().Add(-time.Hour)
	for number := 1; number <= maxOperationEntries/2; number++ {
		plantBoundedRun(t, root, boundedRunName(number), base)
	}
	opened := boundedRunName(maxOperationEntries)
	target := opened + "/" + lifecycle.RunOutputName
	if err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
		if err := view.OpenRun(ctx, opened); err != nil {
			return err
		}
		if err := view.Runs().WriteExclusive(ctx, target, nil); err != nil {
			return err
		}
		return view.Runs().Append(ctx, target, []byte("powered on\n"))
	}); err != nil {
		t.Fatalf("a power run past the old entry bound failed: %#v", diagnostics.Of(err))
	}
	held := heldNamesOf(t, root)
	if len(held) != maxBoundedRuns || !slices.Contains(held, opened) {
		t.Fatalf("the runs area holds %d runs (%t the new one), want %d with it", len(held), slices.Contains(held, opened), maxBoundedRuns)
	}
	if data, err := os.ReadFile(filepath.Join(root, target)); err != nil || string(data) != "powered on\n" {
		t.Fatalf("the new run kept %q (%v)", data, err)
	}
}

// A run another live run holds is never retired, however old: it keeps its
// directory and everything already in it while newer runs come and go, and is
// retired only once its own callback returned.
func TestARunInProgressIsNeverRetired(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	root := runsRoot(t, store, "example")
	progress := boundedRunName(0x100)
	target := progress + "/" + lifecycle.RunOutputName
	err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
		if err := view.OpenRun(ctx, progress); err != nil {
			return err
		}
		if err := view.Runs().Append(ctx, target, []byte("before\n")); err != nil {
			return err
		}
		old := time.Now().Add(-24 * time.Hour)
		if err := os.Chtimes(filepath.Join(root, progress), old, old); err != nil {
			return err
		}
		for number := 1; number <= maxBoundedRuns+1; number++ {
			openRun(t, store, boundedRunName(number))
		}
		if err := view.Runs().Append(ctx, target, []byte("after\n")); err != nil {
			return err
		}
		data, found, err := view.Runs().Read(ctx, target, 64)
		if err != nil || !found || string(data) != "before\nafter\n" {
			t.Fatalf("the run in progress holds %q %t (%v), want everything it wrote", data, found, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the run in progress failed: %#v", diagnostics.Of(err))
	}
	openRun(t, store, boundedRunName(0x200))
	if _, err := os.Lstat(filepath.Join(root, progress)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the oldest run outlived its callback and newer runs (%v)", err)
	}
}

// Retention removes a run only in the shape a bounded run leaves it: empty or
// holding its own private output. A run holding anything else, or whose output
// is a link or is linked elsewhere, is left exactly as it is, and the new run
// still opens beside it.
func TestARetiredRunIsRemovedOnlyAsTheStoreLeftIt(t *testing.T) {
	for _, test := range []struct {
		name  string
		leave func(t *testing.T, run string)
	}{
		{"a run holding another file", func(t *testing.T, run string) {
			writePrivate(t, filepath.Join(run, "extra"), []byte("something else\n"))
		}},
		{"a run whose output is a link", func(t *testing.T, run string) {
			output := filepath.Join(run, lifecycle.RunOutputName)
			target := filepath.Join(t.TempDir(), "elsewhere")
			writePrivate(t, target, []byte("not the run's own\n"))
			if err := os.Remove(output); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, output); err != nil {
				t.Fatal(err)
			}
		}},
		{"a run whose output is linked elsewhere", func(t *testing.T, run string) {
			if err := os.Link(filepath.Join(run, lifecycle.RunOutputName), filepath.Join(t.TempDir(), "linked")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _ := lifecycleFixture(t)
			root := runsRoot(t, store, "example")
			base := time.Now().Add(-48 * time.Hour)
			for number := 1; number < maxBoundedRuns; number++ {
				plantBoundedRun(t, root, boundedRunName(number), base.Add(time.Duration(number)*time.Hour))
			}
			left := boundedRunName(0x100)
			run := plantBoundedRun(t, root, left, base)
			test.leave(t, run)
			if err := os.Chtimes(run, base, base); err != nil {
				t.Fatal(err)
			}
			before := runShape(t, run)
			opened := boundedRunName(0x200)
			openRun(t, store, opened)
			if after := runShape(t, run); after != before {
				t.Fatalf("the run was changed from %q to %q", before, after)
			}
			held := heldNamesOf(t, root)
			if slices.Contains(held, boundedRunName(1)) || !slices.Contains(held, opened) || len(held) != maxBoundedRuns {
				t.Fatalf("the runs area holds %v, want the left run, the new one and all but the oldest planted", held)
			}
		})
	}
}

// runShape describes what a run directory holds: each entry's name, type,
// size, link count and link target.
func runShape(t *testing.T, run string) string {
	t.Helper()
	entries, err := os.ReadDir(run)
	if err != nil {
		t.Fatalf("the run is gone: %v", err)
	}
	shape := []string{}
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(run, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		link, _ := os.Readlink(filepath.Join(run, entry.Name()))
		shape = append(shape, fmt.Sprintf("%s %v %d %s", entry.Name(), info.Mode(), info.Size(), link))
	}
	return strings.Join(shape, "; ")
}

// The runs area is written beside other runs of its context under the shared
// lock, so it measures every write: a run sees what a run beside it wrote
// since its own last write.
func TestTheRunsAreaMeasuresEveryWrite(t *testing.T) {
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	root := runsRoot(t, store, "example")
	planted := filepath.Join(root, "notes")
	writePrivate(t, planted, nil)
	if err := os.Truncate(planted, maxOperationBytes-100); err != nil {
		t.Fatal(err)
	}
	outer, inner := boundedRunName(1), boundedRunName(2)
	err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
		if err := view.OpenRun(ctx, outer); err != nil {
			return err
		}
		if err := view.Runs().Append(ctx, outer+"/"+lifecycle.RunOutputName, make([]byte, 10)); err != nil {
			return err
		}
		if err := store.RunLifecycle(ctx, "example", func(beside lifecycle.RunView) error {
			if err := beside.OpenRun(ctx, inner); err != nil {
				return err
			}
			return beside.Runs().Append(ctx, inner+"/"+lifecycle.RunOutputName, make([]byte, 85))
		}); err != nil {
			t.Fatalf("the run beside it failed: %#v", diagnostics.Of(err))
		}
		return view.Runs().Append(ctx, outer+"/"+lifecycle.RunOutputName, make([]byte, 10))
	})
	expectRefusal(t, err, "bounded run storage has reached its bounds")
}

// A run that leaves its name while a write's measure walks inside it, as one
// another run's retention removes does, is absent from that measure, as a run
// gone before the walk reached it is, so the write goes on. A run replaced at
// its name meanwhile still refuses.
func TestARunGoneWhileTheRunsAreaIsMeasuredIsAbsent(t *testing.T) {
	injected := errors.New("the walk inside the run failed")
	moveAway := func(t *testing.T, run string) {
		if err := os.Rename(run, filepath.Join(filepath.Dir(run), "moved")); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name  string
		leave func(t *testing.T, run string) error
		want  string
	}{
		{"removed", func(t *testing.T, run string) error {
			if err := os.RemoveAll(run); err != nil {
				t.Fatal(err)
			}
			return injected
		}, ""},
		{"moved away", func(t *testing.T, run string) error {
			moveAway(t, run)
			return nil
		}, ""},
		{"replaced at its name", func(t *testing.T, run string) error {
			moveAway(t, run)
			if err := os.Mkdir(run, 0700); err != nil {
				t.Fatal(err)
			}
			return nil
		}, "lifecycle operation storage cannot be measured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store, _ := lifecycleFixture(t)
			gone := plantBoundedRun(t, runsRoot(t, store, "example"), boundedRunName(1), time.Now().Add(-time.Hour))
			if err := os.Mkdir(filepath.Join(gone, "nested"), 0700); err != nil {
				t.Fatal(err)
			}
			opened := boundedRunName(2)
			target := opened + "/" + lifecycle.RunOutputName
			left := false
			err := store.RunLifecycle(ctx, "example", func(view lifecycle.RunView) error {
				if err := view.OpenRun(ctx, opened); err != nil {
					return err
				}
				reached := 0
				store.fail = func(point string) error {
					if point != string(checkpointMeasureOperationEntry) {
						return nil
					}
					if reached++; reached != 2 {
						return nil
					}
					left = true
					return test.leave(t, gone)
				}
				defer func() { store.fail = nil }()
				return view.Runs().WriteExclusive(ctx, target, nil)
			})
			if !left {
				t.Fatal("the measure never walked inside the planted run")
			}
			if test.want != "" {
				expectRefusal(t, err, test.want)
				return
			}
			if err != nil {
				t.Fatalf("the write beside a run that went failed: %v %#v", err, diagnostics.Of(err))
			}
			if _, err := os.Lstat(filepath.Join(runsRoot(t, store, "example"), target)); err != nil {
				t.Fatalf("the write beside a run that went kept nothing: %v", err)
			}
		})
	}
}

// A run whose directory exists but cannot be handed out, because making it
// durable failed or the run was interrupted, leaves no directory behind.
func TestARunThatCannotOpenLeavesNoDirectory(t *testing.T) {
	injected := errors.New("the runs area could not be made durable")
	for _, test := range []struct {
		name string
		fail func(cancel context.CancelFunc) error
		want error
	}{
		{"its directory cannot be made durable", func(context.CancelFunc) error { return injected }, injected},
		{"an interrupt once its directory exists", func(cancel context.CancelFunc) error {
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
					case point == string(checkpointMkdir):
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
				err = view.OpenRun(ctx, filepath.Base(run))
				return err
			})
			if !errors.Is(err, test.want) || !existed {
				t.Fatalf("the run = %v, its directory there when it failed %t; want %v once it existed", err, existed, test.want)
			}
			if _, err := os.Lstat(run); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the run that could not open left its directory (%v)", err)
			}
		})
	}
}
