package installation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func preparingRequest(t *testing.T, request Request, operation string) (lifecycle.RunRequest, []byte, *[]string) {
	t.Helper()
	runner := &fakeRunner{}
	execution := registrationExecution(t, request, []byte("org"), []byte("key"))
	execution.Attempt = 1
	recorded := &[]string{}
	execution.Prepare = func(_ context.Context, preparation json.RawMessage) error {
		*recorded = append(*recorded, string(preparation))
		return nil
	}
	capability := New(runner)
	switch operation {
	case "apply":
		_, _ = capability.Apply(context.Background(), execution)
	default:
		_, _ = capability.Destroy(context.Background(), execution)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("%s did not reach the adapter", operation)
	}
	marker, err := MarkerFor(request, execution.Block.RequestDigest)
	if err != nil {
		t.Fatal(err)
	}
	return runner.requests[0], marker, recorded
}

func TestEveryApplyValidatesItsMarkerPreparation(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	run, marker, recorded := preparingRequest(t, request, "apply")
	if run.Prepare == nil {
		t.Fatal("a registering apply admits no before-state")
	}
	quoted, _ := json.Marshal(string(marker))
	proved := `{"marker":` + string(quoted) + `}`
	if err := run.Prepare(context.Background(), json.RawMessage(proved)); err != nil || len(*recorded) != 1 || (*recorded)[0] != proved {
		t.Fatalf("the proved marker was recorded as %q (%v)", *recorded, err)
	}
	for _, refused := range []string{
		`{"marker":"another"}`,
		`{"marker":""}`,
		`{"marker":` + string(quoted) + `,"registered":true}`,
		`{}`,
		`{"marker":7}`,
	} {
		err := run.Prepare(context.Background(), json.RawMessage(refused))
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
			reported[0].Message != "the installation's automation published a before-state that does not name this installation's marker" {
			t.Fatalf("%s was judged %+v (%v)", refused, reported, err)
		}
	}
	if len(*recorded) != 1 {
		t.Fatalf("a refused before-state was recorded: %q", *recorded)
	}
	if run, _, _ := preparingRequest(t, request, "destroy"); run.Prepare != nil {
		t.Fatal("a removal admits a before-state")
	}
	if run, _, _ := preparingRequest(t, pinnedRequest(t, labCatalog()), "apply"); run.Prepare == nil {
		t.Fatal("an apply that registers nothing admits no disk-boot intent before its network restart")
	}
}

func TestRegisteredBeforeReadsAnyAttempt(t *testing.T) {
	marker := []byte("frozen-marker")
	proved := json.RawMessage(`{"marker":"frozen-marker"}`)
	for name, want := range map[string]struct {
		preparations []json.RawMessage
		registered   bool
	}{
		"never prepared": {nil, false},
		"first attempt":  {[]json.RawMessage{proved}, true},
		"every attempt":  {[]json.RawMessage{proved, proved}, true},
	} {
		registered, err := registeredBefore(testContext, "rhel-01", want.preparations, marker)
		if err != nil || registered != want.registered {
			t.Fatalf("%s: registered = %t (%v)", name, registered, err)
		}
	}
	for _, unreadable := range []json.RawMessage{
		json.RawMessage(`{"marker":"another-marker"}`),
		json.RawMessage(`{"marker":"frozen-marker","extra":1}`),
		json.RawMessage(`[]`),
	} {
		registered, err := registeredBefore(testContext, "rhel-01", []json.RawMessage{proved, unreadable}, marker)
		reported := diagnostics.Of(err)
		if registered || len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
			reported[0].Message != "the apply records of Machine/rhel-01 hold a before-state this executable cannot read" ||
			reported[0].Remediation != "review its durable state with bootwright status --context "+testContext {
			t.Fatalf("%s was read as %t, %+v", unreadable, registered, reported)
		}
	}
}

func TestAFrozenRequestOfTheEarlierVersionRefuses(t *testing.T) {
	_, err := DecodeRequest([]byte(`{"version":"os-install-anaconda-v14"}`))
	if reported := diagnostics.Of(err); len(reported) == 0 || !strings.Contains(reported[0].Message, "os-install-anaconda-v14") {
		t.Fatalf("the earlier version was read as %v", err)
	}
}
