package baremetal

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A claim of another version, with a field this build does not declare,
// refuses by naming its version and the build to destroy it with.
func TestARequestOfAnotherVersionRefusesByItsVersion(t *testing.T) {
	current, err := DecodeRequest(planOf(t, reconciliation.Apply).Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	current.Version = "machine-baremetal-v1"
	data, err := current.Canonical()
	if err != nil || !strings.HasPrefix(string(data), `{"`) {
		t.Fatalf("the claim of another version is not a canonical object: %s (%v)", data, err)
	}
	_, err = DecodeRequest([]byte(`{"anUndeclaredField":true,` + string(data[1:])))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Source != nil ||
		reported[0].Message != "the frozen machine request has an unsupported version: machine-baremetal-v1" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("a claim of another version refused as %+v", reported)
	}
}
