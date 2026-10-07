package installation

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// An installation an earlier version froze, with a field this build no longer
// declares, refuses by naming its version and the build to destroy it with.
func TestAnEarlierRequestWithARemovedFieldRefusesByItsVersion(t *testing.T) {
	earlier, _ := onlyRequest(t, labCatalog())
	earlier.Version = "os-install-anaconda-v5"
	data, err := earlier.Canonical()
	if err != nil || !strings.HasPrefix(string(data), `{"`) {
		t.Fatalf("the earlier request is not a canonical object: %s (%v)", data, err)
	}
	_, err = DecodeRequest([]byte(`{"aRemovedField":true,` + string(data[1:])))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Source != nil ||
		reported[0].Message != "the frozen installation request has an unsupported version: os-install-anaconda-v5" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("an earlier installation refused as %+v", reported)
	}
}
