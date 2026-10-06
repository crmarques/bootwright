package cli

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Every cluster and shared service status reads with its own token, a failed
// one as a definite failure, and a value outside the vocabulary as an outcome
// that cannot be proved, never as a failure or a success. A setup check the
// first apply has yet to settle reads pending.
func TestStatusTokensCoverTheVocabulary(t *testing.T) {
	for status, want := range map[lifecycle.RealizationStatus]string{
		lifecycle.RealizationDone:        "[OK]",
		lifecycle.RealizationPending:     "[PENDING]",
		lifecycle.RealizationFailed:      "[FAIL]",
		lifecycle.RealizationUnknown:     "[UNKNOWN]",
		lifecycle.RealizationUnsupported: "[SKIPPED]",
		"realized":                       "[UNKNOWN]",
	} {
		if got := serviceStatusToken(status); got != want {
			t.Errorf("status %q reads %s, want %s", status, got, want)
		}
	}
	for status, want := range map[string]string{"ready": "[OK]", "not-ready": "[FAIL]", "pending": "[PENDING]", "unverified": "[UNKNOWN]"} {
		if got := checkToken(status); got != want {
			t.Errorf("check %q reads %s, want %s", status, got, want)
		}
	}
}
