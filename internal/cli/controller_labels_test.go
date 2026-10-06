package cli

import (
	"strings"
	"testing"
	"unicode"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// Every check setup and readiness can report reads as prose, never as the
// identity it is keyed by.
func TestEveryControllerCheckHasALabel(t *testing.T) {
	ids := prerequisites.CheckIDs()
	if len(ids) == 0 {
		t.Fatal("prerequisites lists no check identities")
	}
	for _, id := range ids {
		label := controllerActionLabel(id)
		if label == id || label == "" || !unicode.IsUpper([]rune(label)[0]) || strings.Contains(label, "-") {
			t.Errorf("check %s reads %q, want a capitalized prose label", id, label)
		}
	}
}
