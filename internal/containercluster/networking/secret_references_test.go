package networking

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A cluster network program runs on the controller and places no Secret, so
// its frozen request reports none, and a request this build cannot read is
// refused rather than reported as having none.
func TestTheNetworkReportsNoSecretReferences(t *testing.T) {
	request, err := renderRequest(clusterFixture(), "controller")
	if err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		request []byte
		invalid bool
	}{
		"rendered": {request: data},
		"unread":   {request: []byte(`{"version":"other"}`), invalid: true},
	} {
		t.Run(name, func(t *testing.T) {
			references, err := Capability{}.SecretReferences(reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: tc.request}})
			if (err != nil) != tc.invalid || len(references) != 0 {
				t.Fatalf("references = %v (%v), want none and an error only for an unread request", references, err)
			}
		})
	}
}
