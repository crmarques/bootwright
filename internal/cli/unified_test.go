package cli

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func numbered(count int) []string {
	lines := make([]string, count)
	for index := range lines {
		lines[index] = fmt.Sprintf("line %d", index+1)
	}
	return lines
}

func replaced(lines []string, replacements map[int]string) []string {
	out := append([]string{}, lines...)
	for index, text := range replacements {
		out[index-1] = text
	}
	return out
}

func rendered(hunks []unifiedHunk) []string {
	return formatUnified("a", "b", hunks)
}

func TestUnifiedHunksOfEqualSidesAreNone(t *testing.T) {
	if hunks := unifiedHunks(numbered(5), numbered(5), 3); len(hunks) != 0 {
		t.Fatalf("equal sides gave hunks %+v", hunks)
	}
	if hunks := unifiedHunks(nil, nil, 3); len(hunks) != 0 {
		t.Fatalf("two empty sides gave hunks %+v", hunks)
	}
}

func TestUnifiedHunksShowAOneLineChangeWithThreeLinesOfContext(t *testing.T) {
	old := numbered(12)
	hunks := unifiedHunks(old, replaced(old, map[int]string{6: "line six"}), 3)
	want := []string{
		"--- a", "+++ b", "@@ -3,7 +3,7 @@",
		" line 3", " line 4", " line 5", "-line 6", "+line six", " line 7", " line 8", " line 9",
	}
	if got := rendered(hunks); !reflect.DeepEqual(got, want) {
		t.Fatalf("hunks =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestUnifiedHunksClampContextAtTheStartAndEndOfTheFile(t *testing.T) {
	old := numbered(4)
	hunks := unifiedHunks(old, replaced(old, map[int]string{1: "first", 4: "last"}), 3)
	want := []string{
		"--- a", "+++ b", "@@ -1,4 +1,4 @@",
		"-line 1", "+first", " line 2", " line 3", "-line 4", "+last",
	}
	if got := rendered(hunks); !reflect.DeepEqual(got, want) {
		t.Fatalf("hunks =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestUnifiedHunksShowAnInsertionAtTheStartAndAtTheEnd(t *testing.T) {
	old := numbered(5)
	atStart := unifiedHunks(old, append([]string{"new"}, old...), 3)
	wantStart := []string{"--- a", "+++ b", "@@ -1,3 +1,4 @@", "+new", " line 1", " line 2", " line 3"}
	if got := rendered(atStart); !reflect.DeepEqual(got, wantStart) {
		t.Fatalf("insertion at the start =\n%s", strings.Join(got, "\n"))
	}
	atEnd := unifiedHunks(old, append(append([]string{}, old...), "new"), 3)
	wantEnd := []string{"--- a", "+++ b", "@@ -3,3 +3,4 @@", " line 3", " line 4", " line 5", "+new"}
	if got := rendered(atEnd); !reflect.DeepEqual(got, wantEnd) {
		t.Fatalf("insertion at the end =\n%s", strings.Join(got, "\n"))
	}
}

func TestUnifiedHunksOfAnEmptySideStartAtLineZero(t *testing.T) {
	added := rendered(unifiedHunks(nil, []string{"x", "y"}, 3))
	if want := []string{"--- a", "+++ b", "@@ -0,0 +1,2 @@", "+x", "+y"}; !reflect.DeepEqual(added, want) {
		t.Fatalf("an empty old side =\n%s", strings.Join(added, "\n"))
	}
	removed := rendered(unifiedHunks([]string{"x", "y", "z"}, nil, 3))
	if want := []string{"--- a", "+++ b", "@@ -1,3 +0,0 @@", "-x", "-y", "-z"}; !reflect.DeepEqual(removed, want) {
		t.Fatalf("an empty new side =\n%s", strings.Join(removed, "\n"))
	}
}

func TestUnifiedHunksMergeChangesWhoseContextTouches(t *testing.T) {
	old := numbered(20)
	merged := unifiedHunks(old, replaced(old, map[int]string{5: "five", 12: "twelve"}), 3)
	if len(merged) != 1 || merged[0].oldStart != 2 || merged[0].oldLines != 14 || merged[0].newStart != 2 || merged[0].newLines != 14 {
		t.Fatalf("changes six lines apart gave %+v, want one hunk -2,14 +2,14", merged)
	}
	separate := unifiedHunks(old, replaced(old, map[int]string{5: "five", 13: "thirteen"}), 3)
	if len(separate) != 2 || separate[0].oldStart != 2 || separate[0].oldLines != 7 || separate[1].oldStart != 10 || separate[1].oldLines != 7 {
		t.Fatalf("changes seven lines apart gave %+v, want two hunks -2,7 and -10,7", separate)
	}
}

func TestUnifiedHunksCarryTheShiftOfEarlierHunksIntoLaterStarts(t *testing.T) {
	old := numbered(30)
	changed := append(append(append([]string{}, old[:4]...), "extra one", "extra two"), old[4:]...)
	changed = replaced(changed, map[int]string{29: "late"})
	hunks := unifiedHunks(old, changed, 1)
	if len(hunks) != 2 {
		t.Fatalf("got %d hunks, want 2: %+v", len(hunks), hunks)
	}
	if first := hunks[0]; first.oldStart != 4 || first.oldLines != 2 || first.newStart != 4 || first.newLines != 4 {
		t.Fatalf("first hunk = %+v, want -4,2 +4,4", first)
	}
	if second := hunks[1]; second.oldStart != 26 || second.oldLines != 3 || second.newStart != 28 || second.newLines != 3 {
		t.Fatalf("second hunk = %+v, want -26,3 +28,3", second)
	}
}

func TestUnifiedHunksBeyondTheCellBoundReplaceTheWholeChangedRegion(t *testing.T) {
	old, changed := make([]string, 2001), make([]string, 2001)
	for index := range old {
		old[index], changed[index] = fmt.Sprintf("old %d", index), fmt.Sprintf("new %d", index)
	}
	hunks := unifiedHunks(old, changed, 3)
	if len(hunks) != 1 || hunks[0].oldStart != 1 || hunks[0].oldLines != 2001 || hunks[0].newStart != 1 || hunks[0].newLines != 2001 {
		t.Fatalf("hunks = %d, want one replacing both sides whole", len(hunks))
	}
	for index, line := range hunks[0].lines {
		want := byte('-')
		if index >= 2001 {
			want = '+'
		}
		if line.op != want {
			t.Fatalf("line %d op %q, want %q", index, line.op, want)
		}
	}
}

func TestUnifiedHunksKeepTheCommonEndsOfSidesBeyondTheCellBound(t *testing.T) {
	old, changed := make([]string, 2001), make([]string, 2001)
	for index := range old {
		old[index], changed[index] = fmt.Sprintf("old %d", index), fmt.Sprintf("new %d", index)
	}
	old = append(append([]string{"head"}, old...), "tail")
	changed = append(append([]string{"head"}, changed...), "tail")
	hunks := unifiedHunks(old, changed, 3)
	if len(hunks) != 1 || hunks[0].lines[0] != (unifiedLine{' ', "head"}) || hunks[0].oldLines != 2003 || hunks[0].newLines != 2003 {
		t.Fatalf("hunks = %+v, want one that keeps the head and tail as context", hunks)
	}
}

func TestUnifiedHunksOfMovedLinesAreMinimal(t *testing.T) {
	hunks := unifiedHunks([]string{"a", "b", "c", "d"}, []string{"a", "c", "d", "b"}, 0)
	want := []string{"--- a", "+++ b", "@@ -2,1 +1,0 @@", "-b", "@@ -4,0 +4,1 @@", "+b"}
	if got := rendered(hunks); !reflect.DeepEqual(got, want) {
		t.Fatalf("hunks without context =\n%s", strings.Join(got, "\n"))
	}
}
