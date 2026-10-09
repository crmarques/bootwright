package contexts

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestContextNamesFollowTheAPIGrammar(t *testing.T) {
	for _, name := range []string{"lab", "lab-rhel", "a", "0", "0lab", "lab0", "a--b", strings.Repeat("a", 63), strings.Repeat("a", 64),
		"", "-lab", "lab-", "Lab", "lab_rhel", "lab.rhel", "lab rhel", "ü", "a\n", "a\x00b"} {
		if got, want := validName(name) == nil, api.ValidLexical("name", name); got != want {
			t.Errorf("name %q: accepted = %v, API grammar = %v", name, got, want)
		}
	}
}
