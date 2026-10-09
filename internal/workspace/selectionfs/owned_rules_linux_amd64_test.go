//go:build linux && amd64

package selectionfs

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func TestSelectionNamesFollowTheAPIGrammar(t *testing.T) {
	for _, name := range []string{"lab", "lab-rhel", "a", "0", "0lab", "lab0", "a--b", strings.Repeat("a", 63), strings.Repeat("a", 64),
		"", "-lab", "lab-", "Lab", "lab_rhel", "lab.rhel", "lab rhel", "ü", "a\n", "a\x00b"} {
		if got, want := validSelection(contexts.Selection{Version: contexts.SelectionVersion, Name: name}), api.ValidLexical("name", name); got != want {
			t.Errorf("name %q: accepted = %v, API grammar = %v", name, got, want)
		}
	}
}
