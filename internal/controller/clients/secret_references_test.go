package clients

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A controller prerequisites request names tools and host packages, never a
// Secret, and one this build cannot read is refused rather than reported empty.
func TestPrerequisitesReportNoSecretReferences(t *testing.T) {
	block := planBlock(t, unwired(), stateOf(environment(), machine("container-runtime", "libvirt"), cluster()), reconciliation.Apply)
	unread := block
	unread.Request = []byte(`{"version":"other"}`)
	for name, tc := range map[string]struct {
		block   reconciliation.Block
		invalid bool
	}{
		"planned": {block: block},
		"unread":  {block: unread, invalid: true},
	} {
		t.Run(name, func(t *testing.T) {
			references, err := Capability{}.SecretReferences(tc.block)
			if (err != nil) != tc.invalid || len(references) != 0 {
				t.Fatalf("references = %v (%v), want none and an error only for an unread request", references, err)
			}
		})
	}
}
