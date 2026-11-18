package clients

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// An unsupported version refuses before decoding its shape.
func TestAnUnsupportedRequestVersionRefusesBeforeItsShape(t *testing.T) {
	block := planBlock(t, unwired(), stateOf(libvirtGraph(cluster())...), reconciliation.Apply)
	earlier, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	earlier.Version = "controller-clients-v2"
	data, err := earlier.Canonical()
	if err != nil || !strings.HasPrefix(string(data), `{"`) {
		t.Fatalf("the earlier request is not a canonical object: %s (%v)", data, err)
	}
	_, err = DecodeRequest([]byte(`{"aRemovedField":true,` + string(data[1:])))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "records.version-unsupported" || reported[0].Source != nil ||
		reported[0].Message != "the frozen controller prerequisites request has an unsupported version: controller-clients-v2" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("an earlier prerequisites request refused as %+v", reported)
	}
}
