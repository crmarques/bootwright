package compilation_test

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

func TestSortedNamesOwnsDeterministicIndependentReportNames(t *testing.T) {
	for _, input := range [][]string{nil, {}} {
		if got := compilation.SortedNames(input); got == nil || len(got) != 0 {
			t.Fatalf("empty names = %#v, want allocated empty slice", got)
		}
	}
	original := []string{"z", "a", "z", "b"}
	for _, input := range [][]string{original, {"b", "z", "a", "z"}} {
		got := compilation.SortedNames(input)
		if want := []string{"a", "b", "z"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("names = %v, want %v", got, want)
		}
		got[0] = "changed"
	}
	if !reflect.DeepEqual(original, []string{"z", "a", "z", "b"}) {
		t.Fatalf("sorting or changing its result mutated input: %v", original)
	}
}
