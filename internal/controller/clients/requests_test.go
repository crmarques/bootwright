package clients

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A prerequisites request an earlier version froze, with a field this build
// no longer declares, refuses by naming its version and the build to destroy
// it with.
func TestAnEarlierRequestWithARemovedFieldRefusesByItsVersion(t *testing.T) {
	capability := New(&fakeTools{}, &fakeNative{}, &fakeNative{}, &fakeInstaller{})
	block := planBlock(t, capability, stateOf(environment(), machine("container-runtime", "libvirt"), cluster()), reconciliation.Apply)
	earlier, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	earlier.Version = "controller-clients-v1"
	data, err := earlier.Canonical()
	if err != nil || !strings.HasPrefix(string(data), `{"`) {
		t.Fatalf("the earlier request is not a canonical object: %s (%v)", data, err)
	}
	_, err = DecodeRequest([]byte(`{"aRemovedField":true,` + string(data[1:])))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Source != nil ||
		reported[0].Message != "the frozen controller prerequisites request has an unsupported version: controller-clients-v1" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("an earlier prerequisites request refused as %+v", reported)
	}
}
