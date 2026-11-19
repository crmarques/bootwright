package artifactserver

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A server of another version, with a member this build does not
// declare, refuses by naming its version and the build to destroy it with.
func TestAnotherVersionWithAnUndeclaredMemberRefusesByItsVersion(t *testing.T) {
	plan, err := New(&fakeRunner{}, fixedClock{}).Plan(context.Background(), planInput(t, reconciliation.Apply, controller(), artifactServer()))
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan = %+v (%v)", plan, err)
	}
	other, err := DecodeRequest(plan.Definitions[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	other.Version = "artifact-server-nginx-v0"
	data, err := other.Canonical()
	if err != nil || !strings.HasPrefix(string(data), `{"`) {
		t.Fatalf("the request is not a canonical object: %s (%v)", data, err)
	}
	_, err = DecodeRequest([]byte(`{"anUndeclaredMember":true,` + string(data[1:])))
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "records.version-unsupported" || reported[0].Source != nil ||
		reported[0].Message != "the frozen artifact-server request has an unsupported version: artifact-server-nginx-v0" ||
		reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("another artifact server refused as %+v", reported)
	}
}

func TestARequestOfAnotherVersionRefuses(t *testing.T) {
	r := fixtureRequest(t)
	r.Version = "artifact-server-nginx-v2"
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeRequest(data)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Message != "the frozen artifact-server request has an unsupported version: artifact-server-nginx-v2" || reported[0].Remediation != "destroy it with the build that applied it" {
		t.Fatalf("refusal: %+v", reported)
	}
}
