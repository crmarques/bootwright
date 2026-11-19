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
	request, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.Version = "controller-clients-v2"
	data, err := request.Canonical()
	if err != nil || !strings.HasPrefix(string(data), `{"`) {
		t.Fatalf("the request is not a canonical object: %s (%v)", data, err)
	}
	_, err = DecodeRequest([]byte(`{"anUndeclaredMember":true,` + string(data[1:])))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "records.version-unsupported" || reported[0].Source != nil ||
		reported[0].Message != "the frozen controller prerequisites request has an unsupported version: controller-clients-v2" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("a request of another version refused as %+v", reported)
	}
}
