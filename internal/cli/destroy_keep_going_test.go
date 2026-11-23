package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestDestroyKeepGoingDispatchesOnlyForDestroy(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"destroy", "--keep-going"}, true},
		{[]string{"destroy", "--dry-run", "--keep-going"}, true},
		{[]string{"destroy", "--keep-going=false"}, false},
	} {
		_, _, _, record := runRecorded(test.args)
		var keep bool
		switch request := record.request.(type) {
		case lifecycle.DestroyRequest:
			keep = request.KeepGoing
		case lifecycle.PlanRequest:
			keep = request.KeepGoing && request.Verb == "destroy" && request.Readiness
		default:
			t.Fatalf("%v dispatched %#v", test.args, request)
		}
		if keep != test.want {
			t.Fatalf("%v dispatched %#v", test.args, record.request)
		}
	}
	code, _, _, record := runRecorded([]string{"apply", "--keep-going"})
	if code != 2 || record.calls != 0 {
		t.Fatalf("apply --keep-going: code=%d calls=%d", code, record.calls)
	}
}

func TestDestroyKeepGoingShowsRemainingInTextAndStructuredFailure(t *testing.T) {
	failure := &lifecycle.BlockFailure{Code: "erase.machine-running", Message: "The Machine is running", Remedy: "Stop it and retry"}
	for _, format := range []string{"text", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			record := &dispatchRecord{
				result: commandResult{lifecycleOperation: &lifecycle.OperationResult{
					Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "destroy", KeepGoing: true,
					Steps: []lifecycle.PlanStep{
						{ID: "guest", Kind: "Machine", Object: "worker-a"}, {ID: "other", Kind: "Machine", Object: "worker-b"},
						{ID: "provider", Kind: "Substrate", Object: "provider"},
					},
					Blocks: []lifecycle.BlockResult{
						{ID: "guest", State: "failed", Failure: failure}, {ID: "other", State: "done"},
						{ID: "provider", State: "pending", WaitsFor: []string{"guest"}},
					},
					RetiringWithHosts: []string{"other"},
					NextCommand:       "bootwright destroy --context lab --keep-going",
					Receipt:           lifecycle.Receipt{Verb: "destroy", State: "failed", Operation: "removal", Next: "destroy"},
				}},
				err: diagnostics.NewFailure("destroy.incomplete", "cleanup is incomplete", ""),
			}
			var out, stderr bytes.Buffer
			code := New(Config{Out: &out, ErrOut: &stderr, Services: dispatchSpies(record)}).Run(t.Context(),
				[]string{"destroy", "--keep-going", "--output", format})
			if code != 1 {
				t.Fatalf("exit = %d, stdout = %s, stderr = %s", code, &out, &stderr)
			}
			if format == "text" {
				for _, want := range []string{"Remaining cleanup", "Machine/worker-a", "The Machine is running", "Waiting for guest", "1 of 3 cleanup steps", "Retirement awaiting host cleanup", "--keep-going"} {
					if !strings.Contains(out.String(), want) {
						t.Fatalf("stdout lacks %q: %s", want, &out)
					}
				}
				return
			}
			doc := structuredDocument(t, out.String())
			result := doc["result"].(map[string]any)
			remaining := result["remaining"].([]any)
			if stderr.Len() != 0 || result["keepGoing"] != true || len(remaining) != 2 || remaining[0] != "guest" || remaining[1] != "provider" {
				t.Fatalf("unexpected structured result %v, stderr = %s", result, &stderr)
			}
		})
	}
}

func TestDestroyKeepGoingPreviewShowsConditionalRetryAndBlockers(t *testing.T) {
	plan := &lifecycle.PlanResult{
		Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "destroy", KeepGoing: true,
		Steps: []lifecycle.PlanStep{
			{ID: "guest", Kind: "Machine", Object: "worker-a", PreviousFailure: &lifecycle.BlockFailure{Code: "erase.machine-running", Message: "The Machine was running"}},
			{ID: "provider", Kind: "Substrate", Object: "provider", After: []int{1}},
			{ID: "storage", Kind: "StorageCluster", Object: "storage", State: "unknown"},
		},
		Checks: []lifecycle.PlanCheck{
			{ID: "removal-evidence", Status: "deferred"},
			{ID: "initial-power", Status: "passed", Summary: "Managed Machines are stopped or absent"},
		},
	}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, plan); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"will retry", "The Machine was running", "[after 1]", "steps 2 still wait", "quiescence during execution", "resolve before cleanup", "stopped or absent"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("preview lacks %q: %s", want, &out)
		}
	}
	plan.Checks = []lifecycle.PlanCheck{{ID: "destroy-admission", Status: "failed"}}
	out.Reset()
	if err := writeLifecyclePlan(&out, plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No cleanup would start") || strings.Contains(out.String(), "will retry") {
		t.Fatalf("blocked preview = %s", &out)
	}
}

func TestDestroyKeepGoingReportsUnverifiedOutcomes(t *testing.T) {
	result := &lifecycle.OperationResult{
		Verb: "destroy", KeepGoing: true,
		Steps: []lifecycle.PlanStep{
			{ID: "unreachable", Kind: "Machine", Object: "node-a"},
			{ID: "interrupted", Kind: "Machine", Object: "node-b"},
			{ID: "failed", Kind: "Machine", Object: "node-c"},
		},
		Blocks: []lifecycle.BlockResult{
			{ID: "unreachable", State: "unknown", Unresolved: &lifecycle.Unresolved{Reason: "The provider could not prove removal"}},
			{ID: "interrupted", State: "running"},
			{ID: "failed", State: "failed"},
		},
		Receipt: lifecycle.Receipt{Verb: "destroy", State: "unknown"},
	}
	var out bytes.Buffer
	if err := writeLifecycleOperation(&out, result); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "Outcome unverified") != 2 ||
		!strings.Contains(out.String(), "The provider could not prove removal") ||
		!strings.Contains(out.String(), "Cleanup failed") {
		t.Fatalf("remaining cleanup = %s", &out)
	}
}
