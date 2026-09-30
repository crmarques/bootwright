// Package areacontract is the shared contract suite for operationstore.Area.
// Every implementation's tests run it, the in-memory doubles included, so a
// double cannot accept what the Workspace adapter refuses. No production
// package imports it.
package areacontract

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// Within provides one fresh, empty, writable area to use and fails the test
// when it cannot.
type Within func(t *testing.T, use func(operationstore.Area))

// Verify holds one implementation to every clause, each against its own area.
func Verify(t *testing.T, within Within) {
	t.Helper()
	for _, clause := range []struct {
		name  string
		check func(*testing.T, operationstore.Area)
	}{
		{"absent read reports not found", absentReadReportsNotFound},
		{"exclusive write keeps the first bytes", exclusiveWriteKeepsTheFirstBytes},
		{"nil expectation requires absence", nilExpectationRequiresAbsence},
		{"expectation requires the exact bytes", expectationRequiresTheExactBytes},
		{"append creates then extends in order", appendCreatesThenExtendsInOrder},
		{"entries list each name once in order", entriesListEachNameOnceInOrder},
		{"a removal takes only an empty directory", removalTakesOnlyAnEmptyDirectory},
		{"read refuses a record beyond its maximum", readRefusesARecordBeyondItsMaximum},
		{"sync names a directory never a record", syncNamesADirectoryNeverARecord},
		{"records and directories never share a path", recordsAndDirectoriesNeverShareAPath},
		{"every path stays inside the area", everyPathStaysInsideTheArea},
		{"a cancelled context changes nothing", cancelledContextChangesNothing},
	} {
		t.Run(clause.name, func(t *testing.T) {
			provided := false
			within(t, func(area operationstore.Area) {
				provided = true
				clause.check(t, area)
			})
			if !provided {
				t.Fatal("the runner provided no area")
			}
		})
	}
}

func absentReadReportsNotFound(t *testing.T, area operationstore.Area) {
	for _, target := range []string{"r.json", "p/f.json"} {
		data, found, err := area.Read(context.Background(), target, 64)
		if err != nil || found || len(data) != 0 {
			t.Fatalf("read of absent %s = %q %t (%v), want not found and no error", target, data, found, err)
		}
	}
}

func exclusiveWriteKeepsTheFirstBytes(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	succeeds(t, area.WriteExclusive(ctx, "r.json", []byte("first\n")), "the first exclusive write")
	if err := area.WriteExclusive(ctx, "r.json", []byte("second\n")); err == nil {
		t.Fatal("a second exclusive write replaced an existing record")
	}
	holds(t, area, "r.json", "first\n")
}

func nilExpectationRequiresAbsence(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	succeeds(t, area.Replace(ctx, "r.json", []byte("first\n"), nil), "a replacement over absence")
	holds(t, area, "r.json", "first\n")
	if err := area.Replace(ctx, "r.json", []byte("second\n"), nil); err == nil {
		t.Fatal("a nil expectation replaced a present record")
	}
	holds(t, area, "r.json", "first\n")
}

func expectationRequiresTheExactBytes(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	if err := area.Replace(ctx, "r.json", []byte("next\n"), []byte("first\n")); err == nil {
		t.Fatal("an expectation was met by an absent record")
	}
	absent(t, area, "r.json")
	succeeds(t, area.WriteExclusive(ctx, "r.json", []byte("first\n")), "the first exclusive write")
	for _, wrong := range []string{"other\n", "first", ""} {
		if err := area.Replace(ctx, "r.json", []byte("next\n"), []byte(wrong)); err == nil {
			t.Fatalf("expectation %q was met by %q", wrong, "first\n")
		}
		holds(t, area, "r.json", "first\n")
	}
	succeeds(t, area.Replace(ctx, "r.json", []byte("next\n"), []byte("first\n")), "a replacement over the exact bytes")
	holds(t, area, "r.json", "next\n")
}

func appendCreatesThenExtendsInOrder(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	succeeds(t, area.Append(ctx, "log.txt", []byte("one\n")), "the first append")
	holds(t, area, "log.txt", "one\n")
	succeeds(t, area.Append(ctx, "log.txt", []byte("two\n")), "the second append")
	holds(t, area, "log.txt", "one\ntwo\n")
}

func entriesListEachNameOnceInOrder(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	succeeds(t, area.WriteExclusive(ctx, "r.json", []byte("record\n")), "a record")
	succeeds(t, area.Append(ctx, "log.txt", []byte("a\n")), "a log")
	succeeds(t, area.WriteExclusive(ctx, "p/f.json", []byte("nested record\n")), "a nested record")
	succeeds(t, area.WriteExclusive(ctx, "p/g.json", []byte("sibling\n")), "a sibling record")
	succeeds(t, area.EnsureDirectory(ctx, "d"), "a directory")
	lists(t, area, "",
		operationstore.Entry{Name: "d", Directory: true},
		operationstore.Entry{Name: "log.txt", Size: 2},
		operationstore.Entry{Name: "p", Directory: true},
		operationstore.Entry{Name: "r.json", Size: 7},
	)
	lists(t, area, "p", operationstore.Entry{Name: "f.json", Size: 14}, operationstore.Entry{Name: "g.json", Size: 8})
	lists(t, area, "d")
	lists(t, area, "absent")
	lists(t, area, "absent/deeper")
}

func removalTakesOnlyAnEmptyDirectory(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	succeeds(t, area.EnsureDirectory(ctx, "d/e"), "a nested directory")
	succeeds(t, area.WriteExclusive(ctx, "p/f.json", []byte("nested\n")), "a nested record")
	for _, refused := range []string{"", "d", "p", "p/f.json"} {
		if err := area.RemoveDirectory(ctx, refused); err == nil {
			t.Fatalf("a removal of %q succeeded", refused)
		}
	}
	holds(t, area, "p/f.json", "nested\n")
	succeeds(t, area.RemoveDirectory(ctx, "d/e"), "a removal of an empty directory")
	lists(t, area, "d")
	succeeds(t, area.RemoveDirectory(ctx, "d/e"), "a removal of an absent directory")
	succeeds(t, area.RemoveDirectory(ctx, "d"), "a removal of an emptied directory")
	succeeds(t, area.RemoveDirectory(ctx, "absent/deeper"), "a removal beneath an absent directory")
	lists(t, area, "", operationstore.Entry{Name: "p", Directory: true})
}

func readRefusesARecordBeyondItsMaximum(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	succeeds(t, area.WriteExclusive(ctx, "r.json", []byte("12345678")), "a record")
	if data, _, err := area.Read(ctx, "r.json", 7); err == nil {
		t.Fatalf("a read bounded at 7 bytes returned %q", data)
	}
	data, found, err := area.Read(ctx, "r.json", 8)
	if err != nil || !found || string(data) != "12345678" {
		t.Fatalf("a read bounded at the record's length = %q %t (%v)", data, found, err)
	}
}

func syncNamesADirectoryNeverARecord(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	succeeds(t, area.Sync(ctx, ""), "a sync of the empty area")
	succeeds(t, area.EnsureDirectory(ctx, "d"), "a directory")
	succeeds(t, area.WriteExclusive(ctx, "p/f.json", []byte("nested\n")), "a nested record")
	succeeds(t, area.WriteExclusive(ctx, "r.json", []byte("record\n")), "a record")
	for _, target := range []string{"", "d", "p", "absent", "absent/deeper"} {
		succeeds(t, area.Sync(ctx, target), "a sync of "+target)
	}
	for _, target := range []string{"r.json", "p/f.json"} {
		if err := area.Sync(ctx, target); err == nil {
			t.Fatalf("a sync of record %s succeeded", target)
		}
	}
}

func recordsAndDirectoriesNeverShareAPath(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	succeeds(t, area.EnsureDirectory(ctx, "d"), "a directory")
	succeeds(t, area.WriteExclusive(ctx, "p/f.json", []byte("nested\n")), "a nested record")
	succeeds(t, area.WriteExclusive(ctx, "r.json", []byte("record\n")), "a record")
	for _, directory := range []string{"d", "p"} {
		for _, refused := range recordCalls(ctx, area, directory) {
			if refused.err == nil {
				t.Fatalf("%s at directory %s succeeded", refused.call, directory)
			}
		}
	}
	if err := area.EnsureDirectory(ctx, "r.json"); err == nil {
		t.Fatal("a directory was ensured at a record's path")
	}
	if _, err := area.Entries(ctx, "r.json"); err == nil {
		t.Fatal("a record was listed as a directory")
	}
	if err := area.WriteExclusive(ctx, "r.json/x.json", []byte("x\n")); err == nil {
		t.Fatal("a record was written beneath a record")
	}
	if _, _, err := area.Read(ctx, "r.json/x.json", 64); err == nil {
		t.Fatal("a record was read beneath a record")
	}
	holds(t, area, "r.json", "record\n")
	holds(t, area, "p/f.json", "nested\n")
	lists(t, area, "",
		operationstore.Entry{Name: "d", Directory: true},
		operationstore.Entry{Name: "p", Directory: true},
		operationstore.Entry{Name: "r.json", Size: 7},
	)
	lists(t, area, "d")
}

func everyPathStaysInsideTheArea(t *testing.T, area operationstore.Area) {
	ctx := context.Background()
	for _, target := range []string{"", "..", "../escape.json", "/absolute.json", "a/../../b.json", "a//b.json", "./a.json"} {
		for _, refused := range recordCalls(ctx, area, target) {
			if refused.err == nil {
				t.Fatalf("%s accepted path %q", refused.call, target)
			}
		}
	}
	for _, target := range []string{"..", "../escape", "/absolute", "a/../../b", "a//b", "./a"} {
		for _, refused := range directoryCalls(ctx, area, target) {
			if refused.err == nil {
				t.Fatalf("%s accepted path %q", refused.call, target)
			}
		}
	}
	lists(t, area, "")
}

func cancelledContextChangesNothing(t *testing.T, area operationstore.Area) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, refused := range recordCalls(cancelled, area, "r.json") {
		if !errors.Is(refused.err, context.Canceled) {
			t.Fatalf("%s under a cancelled context = %v", refused.call, refused.err)
		}
	}
	for _, target := range []string{"", "d"} {
		for _, refused := range directoryCalls(cancelled, area, target) {
			if !errors.Is(refused.err, context.Canceled) {
				t.Fatalf("%s of %q under a cancelled context = %v", refused.call, target, refused.err)
			}
		}
	}
	lists(t, area, "")
}

type outcome struct {
	call string
	err  error
}

func recordCalls(ctx context.Context, area operationstore.Area, target string) []outcome {
	_, _, read := area.Read(ctx, target, 64)
	return []outcome{
		{"Read", read},
		{"WriteExclusive", area.WriteExclusive(ctx, target, []byte("x\n"))},
		{"Replace", area.Replace(ctx, target, []byte("x\n"), nil)},
		{"Append", area.Append(ctx, target, []byte("x\n"))},
	}
}

func directoryCalls(ctx context.Context, area operationstore.Area, target string) []outcome {
	_, entries := area.Entries(ctx, target)
	return []outcome{
		{"Entries", entries},
		{"EnsureDirectory", area.EnsureDirectory(ctx, target)},
		{"Sync", area.Sync(ctx, target)},
		{"RemoveDirectory", area.RemoveDirectory(ctx, target)},
	}
}

func succeeds(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s failed: %v", what, err)
	}
}

func holds(t *testing.T, area operationstore.Area, target, want string) {
	t.Helper()
	data, found, err := area.Read(context.Background(), target, 64)
	if err != nil || !found || string(data) != want {
		t.Fatalf("read of %s = %q %t (%v), want %q", target, data, found, err, want)
	}
}

func absent(t *testing.T, area operationstore.Area, target string) {
	t.Helper()
	data, found, err := area.Read(context.Background(), target, 64)
	if err != nil || found {
		t.Fatalf("read of %s = %q %t (%v), want it absent", target, data, found, err)
	}
}

func lists(t *testing.T, area operationstore.Area, target string, want ...operationstore.Entry) {
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
