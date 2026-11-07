package installation

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// An installation of another version, with a field this build does not
// declare, refuses by naming its version and the build to destroy it with.
func TestARequestOfAnotherVersionRefusesByItsVersion(t *testing.T) {
	other, _ := onlyRequest(t, labCatalog())
	other.Version = "os-install-anaconda-v5"
	data, err := other.Canonical()
	if err != nil || !strings.HasPrefix(string(data), `{"`) {
		t.Fatalf("the request of another version is not a canonical object: %s (%v)", data, err)
	}
	_, err = DecodeRequest([]byte(`{"anUndeclaredField":true,` + string(data[1:])))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Source != nil ||
		reported[0].Message != "the frozen installation request has an unsupported version: os-install-anaconda-v5" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("an installation of another version refused as %+v", reported)
	}
}
