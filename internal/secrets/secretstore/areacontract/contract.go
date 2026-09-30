// Package areacontract is the shared contract suite for secretstore.Area.
// Every implementation's tests run it, the in-memory doubles included, so a
// double cannot accept what the Workspace adapter refuses. No production
// package imports it.
package areacontract

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// Open runs use against one store's secret area for a single callback,
// read-only when asked, and fails the test when it cannot. Every call of one
// Open reaches the same area, as successive Workspace callbacks do, and what a
// callback observed or expected ends with it.
type Open func(readOnly bool, use func(secretstore.Area))

// Within provides a fresh store whose secret area is empty.
type Within func(t *testing.T) Open

const (
	record = secretstore.RecordPath
	bound  = 64
)

// Verify holds one implementation to every clause, each against its own store.
func Verify(t *testing.T, within Within) {
	t.Helper()
	for _, clause := range []struct {
		name  string
		check func(*testing.T, Open)
	}{
		{"absent reads report not found", absentReadsReportNotFound},
		{"exclusive writes never replace", exclusiveWritesNeverReplace},
		{"a replacement needs its exact read expectation", replacementNeedsItsExactReadExpectation},
		{"a read refuses a file beyond its maximum", readRefusesAFileBeyondItsMaximum},
		{"entries list each name once in order", entriesListEachNameOnceInOrder},
		{"files and directories never share a path", filesAndDirectoriesNeverShareAPath},
		{"every path holds one or two safe segments", everyPathHoldsOneOrTwoSafeSegments},
		{"a cancelled context changes nothing", cancelledContextChangesNothing},
		{"a read-only area refuses every mutation", readOnlyAreaRefusesEveryMutation},
		{"a committed store record ends every general write", committedStoreRecordEndsEveryGeneralWrite},
		{"a file sync reaches only what the callback observed", fileSyncReachesOnlyWhatTheCallbackObserved},
		{"cleanup removes only what it observed under the exact store record", cleanupRemovesOnlyWhatItObserved},
		{"unpublished cleanup needs an absent store record and exact guards", unpublishedCleanupNeedsAnAbsentRecordAndExactGuards},
	} {
		t.Run(clause.name, func(t *testing.T) {
			open := within(t)
			if open == nil {
				t.Fatal("the runner provided no store")
			}
			clause.check(t, func(readOnly bool, use func(secretstore.Area)) {
				t.Helper()
				provided := false
				open(readOnly, func(area secretstore.Area) {
					provided = true
					use(area)
				})
				if !provided {
					t.Fatal("the runner provided no area")
				}
			})
		})
	}
}

func absentReadsReportNotFound(t *testing.T, open Open) {
	open(false, func(area secretstore.Area) {
		for _, target := range []string{record, "parts/p"} {
			for _, read := range readers(area) {
				data, found, err := read.call(context.Background(), target, bound)
				if err != nil || found || len(data) != 0 {
					t.Fatalf("%s of absent %s = %q %t (%v), want not found and no error", read.name, target, data, found, err)
				}
			}
		}
		lists(t, area, "")
	})
}

func exclusiveWritesNeverReplace(t *testing.T, open Open) {
	ctx := context.Background()
	open(false, func(area secretstore.Area) {
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		for _, target := range []string{"r", "parts/p"} {
			succeeds(t, area.WriteExclusive(ctx, target, []byte("first\n")), "the first exclusive write of "+target)
			refuses(t, area.WriteExclusive(ctx, target, []byte("second\n")), "a second exclusive write of "+target)
			refuses(t, area.PublishExclusive(ctx, target, []byte("second\n")), "an exclusive publication over "+target)
			holds(t, area, target, "first\n")
		}
		succeeds(t, area.PublishExclusive(ctx, "parts/q", []byte("published\n")), "the first exclusive publication")
		refuses(t, area.PublishExclusive(ctx, "parts/q", []byte("second\n")), "a second exclusive publication")
		refuses(t, area.WriteExclusive(ctx, "parts/q", []byte("second\n")), "an exclusive write over a publication")
		holds(t, area, "parts/q", "published\n")
	})
	open(true, func(area secretstore.Area) {
		holds(t, area, "r", "first\n")
		holds(t, area, "parts/p", "first\n")
		holds(t, area, "parts/q", "published\n")
	})
}

func replacementNeedsItsExactReadExpectation(t *testing.T, open Open) {
	ctx := context.Background()
	first, next := []byte("first\n"), []byte("next\n")
	open(false, func(area secretstore.Area) {
		notCommitted(t, area, "r", first, nil, "a replacement with no read expectation")
		if _, _, err := area.Read(ctx, "r", bound); err != nil {
			t.Fatalf("a plain read of absent r failed: %v", err)
		}
		notCommitted(t, area, "r", first, nil, "a replacement after a plain read")
		absent(t, area, "r")
		expects(t, area, "r", "")
		notCommitted(t, area, "r", first, []byte("other\n"), "an expectation of bytes over an absent file")
		absent(t, area, "r")
		committed(t, area, "r", first, nil, "a replacement over the absence it read")
		holds(t, area, "r", "first\n")
		for _, wrong := range [][]byte{nil, []byte("other\n"), []byte("first")} {
			notCommitted(t, area, "r", next, wrong, "a replacement expecting "+string(wrong))
			holds(t, area, "r", "first\n")
		}
		committed(t, area, "r", next, first, "a replacement over the bytes it published")
		holds(t, area, "r", "next\n")
	})
	open(false, func(area secretstore.Area) {
		notCommitted(t, area, "r", first, next, "a replacement expecting what an earlier callback read")
		holds(t, area, "r", "next\n")
	})
}

func readRefusesAFileBeyondItsMaximum(t *testing.T, open Open) {
	ctx := context.Background()
	open(false, func(area secretstore.Area) {
		succeeds(t, area.WriteExclusive(ctx, "r", []byte("12345678")), "a file")
		for _, read := range readers(area) {
			if data, _, err := read.call(ctx, "r", 7); err == nil {
				t.Fatalf("%s bounded at 7 bytes returned %q", read.name, data)
			}
			data, found, err := read.call(ctx, "r", 8)
			if err != nil || !found || string(data) != "12345678" {
				t.Fatalf("%s bounded at the file's length = %q %t (%v)", read.name, data, found, err)
			}
		}
	})
}

func entriesListEachNameOnceInOrder(t *testing.T, open Open) {
	ctx := context.Background()
	open(false, func(area secretstore.Area) {
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		succeeds(t, area.EnsureDirectory(ctx, "keys"), "a second directory")
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "an existing directory")
		succeeds(t, area.WriteExclusive(ctx, "parts/b", []byte("bb")), "a nested file")
		succeeds(t, area.PublishExclusive(ctx, "parts/a", []byte("a")), "a nested publication")
		succeeds(t, area.WriteExclusive(ctx, "r", []byte("rrr")), "a file")
	})
	for _, readOnly := range []bool{false, true} {
		open(readOnly, func(area secretstore.Area) {
			lists(t, area, "",
				secretstore.Entry{Name: "keys", Directory: true},
				secretstore.Entry{Name: "parts", Directory: true},
				secretstore.Entry{Name: "r", Size: 3},
			)
			lists(t, area, "parts", secretstore.Entry{Name: "a", Size: 1}, secretstore.Entry{Name: "b", Size: 2})
			lists(t, area, "keys")
		})
	}
}

func filesAndDirectoriesNeverShareAPath(t *testing.T, open Open) {
	ctx := context.Background()
	open(false, func(area secretstore.Area) {
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		succeeds(t, area.WriteExclusive(ctx, "r", []byte("r\n")), "a file")
		refuses(t, area.WriteExclusive(ctx, "parts", []byte("x\n")), "an exclusive write at a directory")
		refuses(t, area.PublishExclusive(ctx, "parts", []byte("x\n")), "an exclusive publication at a directory")
		refuses(t, area.EnsureDirectory(ctx, "r"), "a directory at a file")
		refuses(t, area.WriteExclusive(ctx, "r/x", []byte("x\n")), "a write beneath a file")
		refuses(t, area.WriteExclusive(ctx, "absent/x", []byte("x\n")), "a write beneath an absent directory")
		for _, read := range readers(area) {
			if data, _, err := read.call(ctx, "parts", bound); err == nil {
				t.Fatalf("%s of a directory returned %q", read.name, data)
			}
		}
		if _, err := area.Entries(ctx, "r"); err == nil {
			t.Fatal("a file was listed as a directory")
		}
		holds(t, area, "r", "r\n")
		lists(t, area, "parts")
	})
}

func everyPathHoldsOneOrTwoSafeSegments(t *testing.T, open Open) {
	ctx := context.Background()
	open(false, func(area secretstore.Area) {
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		succeeds(t, area.WriteExclusive(ctx, "parts/p", []byte("p\n")), "a nested file")
		for _, target := range []string{"", ".", "..", "../r", "/r", "r/", "a//b", "parts/../r", "parts/./p", "parts/p/x", "a b", "r\x00", strings.Repeat("r", 256)} {
			for _, refused := range fileCalls(ctx, area, target) {
				if refused.err == nil {
					t.Fatalf("%s accepted path %q", refused.call, target)
				}
			}
		}
		for _, target := range []string{".", "..", "/parts", "parts/", "parts/p", "a b", "../parts"} {
			for _, refused := range directoryCalls(ctx, area, target) {
				if refused.err == nil {
					t.Fatalf("%s accepted path %q", refused.call, target)
				}
			}
		}
		refuses(t, area.EnsureDirectory(ctx, ""), "a directory at the area itself")
		lists(t, area, "", secretstore.Entry{Name: "parts", Directory: true})
		lists(t, area, "parts", secretstore.Entry{Name: "p", Size: 2})
	})
}

func cancelledContextChangesNothing(t *testing.T, open Open) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	open(false, func(area secretstore.Area) {
		outcomes := append(fileCalls(cancelled, area, "r"), directoryCalls(cancelled, area, "")...)
		outcomes = append(outcomes, cleanupCalls(cancelled, area, "r")...)
		outcomes = append(outcomes, outcome{"EnsureDirectory", area.EnsureDirectory(cancelled, "parts")})
		for _, refused := range outcomes {
			if !errors.Is(refused.err, context.Canceled) {
				t.Fatalf("%s under a cancelled context = %v", refused.call, refused.err)
			}
		}
		lists(t, area, "")
	})
}

func readOnlyAreaRefusesEveryMutation(t *testing.T, open Open) {
	ctx := context.Background()
	open(false, func(area secretstore.Area) {
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		succeeds(t, area.WriteExclusive(ctx, "parts/p", []byte("p\n")), "a nested file")
		expects(t, area, record, "")
		committed(t, area, record, []byte("store\n"), nil, "the store record")
	})
	open(true, func(area secretstore.Area) {
		holds(t, area, "parts/p", "p\n")
		expects(t, area, record, "store\n")
		expects(t, area, "parts/p", "p\n")
		lists(t, area, "parts", secretstore.Entry{Name: "p", Size: 2})
		refuses(t, area.EnsureDirectory(ctx, "keys"), "a read-only directory creation")
		refuses(t, area.WriteExclusive(ctx, "parts/q", []byte("q\n")), "a read-only exclusive write")
		refuses(t, area.PublishExclusive(ctx, "parts/q", []byte("q\n")), "a read-only exclusive publication")
		notCommitted(t, area, record, []byte("next\n"), []byte("store\n"), "a read-only replacement")
		refuses(t, area.Sync(ctx, ""), "a read-only sync")
		for _, refused := range cleanupCalls(ctx, area, "parts/p") {
			refuses(t, refused.err, "a read-only "+refused.call)
		}
	})
	open(false, func(area secretstore.Area) {
		holds(t, area, "parts/p", "p\n")
		holds(t, area, record, "store\n")
		lists(t, area, "", secretstore.Entry{Name: "parts", Directory: true}, secretstore.Entry{Name: record, Size: 6})
		lists(t, area, "parts", secretstore.Entry{Name: "p", Size: 2})
	})
}

func committedStoreRecordEndsEveryGeneralWrite(t *testing.T, open Open) {
	ctx := context.Background()
	open(false, func(area secretstore.Area) {
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		succeeds(t, area.WriteExclusive(ctx, "r", []byte("r\n")), "a file")
		expects(t, area, "r", "r\n")
		expects(t, area, record, "")
		committed(t, area, record, []byte("store\n"), nil, "the store record")
		refuses(t, area.EnsureDirectory(ctx, "keys"), "a directory after the store record")
		refuses(t, area.WriteExclusive(ctx, "parts/p", []byte("p\n")), "an exclusive write after the store record")
		refuses(t, area.PublishExclusive(ctx, "parts/p", []byte("p\n")), "an exclusive publication after the store record")
		notCommitted(t, area, "r", []byte("next\n"), []byte("r\n"), "a replacement after the store record")
		refuses(t, area.Sync(ctx, ""), "a sync after the store record")
		refuses(t, area.SyncFile(ctx, "r"), "a file sync after the store record")
		holds(t, area, record, "store\n")
		holds(t, area, "r", "r\n")
		lists(t, area, "",
			secretstore.Entry{Name: "parts", Directory: true},
			secretstore.Entry{Name: "r", Size: 2},
			secretstore.Entry{Name: record, Size: 6},
		)
		lists(t, area, "parts")
	})
}

func fileSyncReachesOnlyWhatTheCallbackObserved(t *testing.T, open Open) {
	ctx := context.Background()
	open(false, func(area secretstore.Area) {
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		succeeds(t, area.WriteExclusive(ctx, "parts/a", []byte("a\n")), "a nested file")
		succeeds(t, area.WriteExclusive(ctx, "parts/b", []byte("b\n")), "a second nested file")
	})
	open(false, func(area secretstore.Area) {
		refuses(t, area.SyncFile(ctx, "parts/a"), "a file sync of a file this callback never observed")
		refuses(t, area.SyncFile(ctx, "parts/absent"), "a file sync of an absent file")
		lists(t, area, "parts", secretstore.Entry{Name: "a", Size: 2}, secretstore.Entry{Name: "b", Size: 2})
		succeeds(t, area.SyncFile(ctx, "parts/a"), "a file sync of a listed file")
	})
	open(false, func(area secretstore.Area) {
		expects(t, area, "parts/b", "b\n")
		succeeds(t, area.SyncFile(ctx, "parts/b"), "a file sync of a file read for replacement")
		refuses(t, area.SyncFile(ctx, "parts/a"), "a file sync of a file only an earlier callback observed")
	})
}

func cleanupRemovesOnlyWhatItObserved(t *testing.T, open Open) {
	ctx := context.Background()
	store := []byte("store\n")
	open(false, func(area secretstore.Area) {
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		succeeds(t, area.WriteExclusive(ctx, "parts/a", []byte("a\n")), "a nested file")
		succeeds(t, area.WriteExclusive(ctx, "parts/b", []byte("b\n")), "a second nested file")
		expects(t, area, record, "")
		committed(t, area, record, store, nil, "the store record")
	})
	open(false, func(area secretstore.Area) {
		expects(t, area, record, "store\n")
		lists(t, area, "parts", secretstore.Entry{Name: "a", Size: 2}, secretstore.Entry{Name: "b", Size: 2})
		refuses(t, area.Prune(ctx, []byte("other\n"), []string{"parts/a"}), "a cleanup expecting another store record")
		refuses(t, area.Prune(ctx, store, []string{"parts/never"}), "a cleanup of a file it never observed")
		refuses(t, area.Prune(ctx, store, []string{record}), "a cleanup of the store record")
		refuses(t, area.Prune(ctx, store, []string{"parts/a", "parts/a"}), "a cleanup naming a file twice")
		holds(t, area, "parts/a", "a\n")
		succeeds(t, area.Prune(ctx, store, []string{"parts/a"}), "a cleanup of an observed file")
		absent(t, area, "parts/a")
		holds(t, area, "parts/b", "b\n")
		holds(t, area, record, "store\n")
	})
	open(false, func(area secretstore.Area) {
		lists(t, area, "parts", secretstore.Entry{Name: "b", Size: 2})
		refuses(t, area.Prune(ctx, store, []string{"parts/b"}), "a cleanup without this callback's store expectation")
	})
	open(false, func(area secretstore.Area) {
		expects(t, area, record, "store\n")
		refuses(t, area.Prune(ctx, store, []string{"parts/b"}), "a cleanup of a file this callback never observed")
	})
	open(false, func(area secretstore.Area) {
		expects(t, area, record, "store\n")
		expects(t, area, "parts/b", "b\n")
		succeeds(t, area.WriteExclusive(ctx, "parts/c", []byte("c\n")), "a write of the next publication")
		refuses(t, area.Prune(ctx, store, []string{"parts/b"}), "a cleanup while a publication is under way")
	})
	open(true, func(area secretstore.Area) {
		holds(t, area, "parts/b", "b\n")
		holds(t, area, "parts/c", "c\n")
	})
}

func unpublishedCleanupNeedsAnAbsentRecordAndExactGuards(t *testing.T, open Open) {
	ctx := context.Background()
	guard := []byte("guard\n")
	guards := []secretstore.RecordExpectation{{Path: "init", Data: guard}}
	open(false, func(area secretstore.Area) {
		succeeds(t, area.WriteExclusive(ctx, "init", guard), "a guard file")
		succeeds(t, area.EnsureDirectory(ctx, "parts"), "a directory")
		succeeds(t, area.WriteExclusive(ctx, "parts/orphan", []byte("o\n")), "an unpublished file")
	})
	open(false, func(area secretstore.Area) {
		expects(t, area, record, "")
		expects(t, area, "init", "guard\n")
		lists(t, area, "parts", secretstore.Entry{Name: "orphan", Size: 2})
		refuses(t, area.PruneUnpublished(ctx, nil, []string{"parts/orphan"}), "an unguarded cleanup")
		refuses(t, area.PruneUnpublished(ctx, []secretstore.RecordExpectation{{Path: "init", Data: []byte("other\n")}}, []string{"parts/orphan"}), "a cleanup under an inexact guard")
		refuses(t, area.PruneUnpublished(ctx, []secretstore.RecordExpectation{{Path: "absent", Data: guard}}, []string{"parts/orphan"}), "a cleanup under an unread guard")
		refuses(t, area.PruneUnpublished(ctx, guards, []string{"init"}), "a cleanup of its own guard")
		refuses(t, area.PruneUnpublished(ctx, guards, []string{"parts/never"}), "a cleanup of a file it never observed")
		holds(t, area, "parts/orphan", "o\n")
		succeeds(t, area.PruneUnpublished(ctx, guards, []string{"parts/orphan"}), "a guarded cleanup of an observed file")
		absent(t, area, "parts/orphan")
		holds(t, area, "init", "guard\n")
	})
	open(false, func(area secretstore.Area) {
		succeeds(t, area.WriteExclusive(ctx, "parts/late", []byte("l\n")), "a file")
		expects(t, area, record, "")
		committed(t, area, record, []byte("store\n"), nil, "the store record")
	})
	open(false, func(area secretstore.Area) {
		expects(t, area, record, "store\n")
		expects(t, area, "init", "guard\n")
		lists(t, area, "parts", secretstore.Entry{Name: "late", Size: 2})
		refuses(t, area.PruneUnpublished(ctx, guards, []string{"parts/late"}), "an unpublished cleanup once the store record exists")
		holds(t, area, "parts/late", "l\n")
	})
}

type outcome struct {
	call string
	err  error
}

type reader struct {
	name string
	call func(context.Context, string, int) ([]byte, bool, error)
}

func readers(area secretstore.Area) []reader {
	return []reader{{"Read", area.Read}, {"ReadMutable", area.ReadMutable}}
}

func fileCalls(ctx context.Context, area secretstore.Area, target string) []outcome {
	_, _, read := area.Read(ctx, target, bound)
	_, _, mutable := area.ReadMutable(ctx, target, bound)
	_, replace := area.Replace(ctx, target, []byte("x\n"), nil)
	return []outcome{
		{"Read", read},
		{"ReadMutable", mutable},
		{"WriteExclusive", area.WriteExclusive(ctx, target, []byte("x\n"))},
		{"PublishExclusive", area.PublishExclusive(ctx, target, []byte("x\n"))},
		{"Replace", replace},
	}
}

func directoryCalls(ctx context.Context, area secretstore.Area, target string) []outcome {
	_, entries := area.Entries(ctx, target)
	return []outcome{
		{"Entries", entries},
		{"Sync", area.Sync(ctx, target)},
	}
}

func cleanupCalls(ctx context.Context, area secretstore.Area, target string) []outcome {
	return []outcome{
		{"SyncFile", area.SyncFile(ctx, target)},
		{"Prune", area.Prune(ctx, []byte("store\n"), []string{target})},
		{"PruneUnpublished", area.PruneUnpublished(ctx, []secretstore.RecordExpectation{{Path: "init", Data: []byte("guard\n")}}, []string{target})},
	}
}

func succeeds(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s failed: %v", what, err)
	}
}

func refuses(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s succeeded", what)
	}
}

func committed(t *testing.T, area secretstore.Area, target string, data, expected []byte, what string) {
	t.Helper()
	outcome, err := area.Replace(context.Background(), target, data, expected)
	if err != nil || outcome != secretstore.Committed {
		t.Fatalf("%s = %s (%v), want committed", what, outcome, err)
	}
}

func notCommitted(t *testing.T, area secretstore.Area, target string, data, expected []byte, what string) {
	t.Helper()
	outcome, err := area.Replace(context.Background(), target, data, expected)
	if err == nil || outcome != secretstore.NotCommitted {
		t.Fatalf("%s = %s (%v), want a refusal that committed nothing", what, outcome, err)
	}
}

func expects(t *testing.T, area secretstore.Area, target, want string) {
	t.Helper()
	data, found, err := area.ReadMutable(context.Background(), target, bound)
	if err != nil || found != (want != "") || string(data) != want {
		t.Fatalf("mutable read of %s = %q %t (%v), want %q", target, data, found, err, want)
	}
}

func holds(t *testing.T, area secretstore.Area, target, want string) {
	t.Helper()
	data, found, err := area.Read(context.Background(), target, bound)
	if err != nil || !found || string(data) != want {
		t.Fatalf("read of %s = %q %t (%v), want %q", target, data, found, err, want)
	}
}

func absent(t *testing.T, area secretstore.Area, target string) {
	t.Helper()
	data, found, err := area.Read(context.Background(), target, bound)
	if err != nil || found {
		t.Fatalf("read of %s = %q %t (%v), want it absent", target, data, found, err)
	}
}

func lists(t *testing.T, area secretstore.Area, target string, want ...secretstore.Entry) {
	t.Helper()
	entries, err := area.Entries(context.Background(), target)
	if err != nil {
		t.Fatalf("entries of %q failed: %v", target, err)
	}
	compared := slices.Clone(entries)
	for index := range compared {
		if compared[index].Directory {
			compared[index].Size = 0
		}
	}
	if !slices.Equal(compared, want) {
		t.Fatalf("entries of %q = %+v, want %+v", target, entries, want)
	}
}
