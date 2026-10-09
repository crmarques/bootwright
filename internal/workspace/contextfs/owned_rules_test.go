package contextfs

import (
	"bytes"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// Record validation follows the owning grammar, discovery exclusions and
// pristine evidence encoding even when their implementations change.
func TestTheStoreCopiesOfOwnedRulesAgreeWithTheirOwners(t *testing.T) {
	t.Run("the context-name grammar is the API's name rule", func(t *testing.T) {
		for _, name := range []string{"lab", "lab-rhel", "a", "a1", strings.Repeat("a", 63), strings.Repeat("a", 64),
			"", "-lab", "lab-", "Lab", "lab_rhel", "lab.rhel", "lab rhel", "ü", "0lab", "lab0", "0", "a--b", "a\n", "a\x00b"} {
			if got, want := contextName(name), api.ValidLexical("name", name); got != want {
				t.Errorf("contextName(%q) = %v, the API's name rule %v", name, got, want)
			}
		}
	})
	t.Run("a frozen path skips what discovery skips", func(t *testing.T) {
		for _, directory := range []string{"vendor", "node_modules", "playbooks", "roles", "collections", "manifests", "secrets", ".hidden", "infra", "clusters"} {
			for _, path := range []string{directory + "/object.yaml", "input/" + directory + "/object.yaml"} {
				if got, want := validFrozenPath(path, "yaml"), !desiredstate.SkippedDirectory(directory); got != want {
					t.Errorf("validFrozenPath(%q) = %v, discovery skipping its directory %v", path, got, !want)
				}
			}
		}
	})
	t.Run("the pristine mutation record is the reconciliation owner's bytes", func(t *testing.T) {
		owned, err := reconciliation.PristineEvidence().Bytes()
		if err != nil || !bytes.Equal(owned, []byte(pristineMutation)) {
			t.Fatalf("the store writes %q as pristine, the owner encodes %q (%v)", pristineMutation, owned, err)
		}
	})
}
