package artifactserver

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A server an earlier version froze, with a field this build no longer
// declares, refuses by naming its version and the build to destroy it with.
func TestAnEarlierRequestWithARemovedFieldRefusesByItsVersion(t *testing.T) {
	plan, err := New(&fakeRunner{}, fixedClock{}).Plan(context.Background(), planInput(t, reconciliation.Apply, controller(), artifactServer()))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	earlier, err := DecodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	earlier.Version = "artifact-server-nginx-v1"
	data, err := earlier.Canonical()
	if err != nil || !strings.HasPrefix(string(data), `{"`) {
		t.Fatalf("the earlier request is not a canonical object: %s (%v)", data, err)
	}
	_, err = DecodeRequest([]byte(`{"aRemovedField":true,` + string(data[1:])))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Source != nil ||
		reported[0].Message != "the frozen artifact-server request has an unsupported version: artifact-server-nginx-v1" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("an earlier artifact server refused as %+v", reported)
	}
}
