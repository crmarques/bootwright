package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func previewResult() *lifecycle.PlanResult {
	return &lifecycle.PlanResult{
		Context: lifecycle.ContextIdentity{Name: "lab"},
		Verb:    "apply",
		Steps: []lifecycle.PlanStep{{
			ID: "artifact-server-lab", Description: "serve artifacts for lab on bastion",
			Impacts: []string{"open-listener 192.0.2.1:8443"}, State: "pending",
		}},
		Receipt: lifecycle.Receipt{Operation: "none", Verb: "plan", State: "preview", Next: "apply"},
	}
}

// The receipt's labels, order and final newline are a public contract, so it
// is compared byte for byte.
func TestLifecycleReceiptBytesAreStable(t *testing.T) {
	var out bytes.Buffer
	if err := writeReceipt(&out, lifecycle.Receipt{Operation: "op-abc", Verb: "apply", State: "done", Next: "destroy"}); err != nil {
		t.Fatal(err)
	}
	want := "operation: op-abc\nverb: apply\nstate: done\nnext: destroy\n"
	if out.String() != want {
		t.Fatalf("receipt = %q, want %q", out.String(), want)
	}
}

func TestPlanResultEndsWithItsReceipt(t *testing.T) {
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, previewResult()); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "Apply plan") || !strings.Contains(rendered, "1. serve artifacts for lab on bastion") {
		t.Fatalf("plan text = %q", rendered)
	}
	if !strings.Contains(rendered, "open-listener 192.0.2.1:8443") {
		t.Fatal("the plan omitted its impacts")
	}
	if !strings.HasSuffix(rendered, "operation: none\nverb: plan\nstate: preview\nnext: apply\n") {
		t.Fatalf("plan does not end with its receipt: %q", rendered)
	}
	for _, line := range strings.Split(rendered, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Fatalf("a rendered line has trailing whitespace: %q", line)
		}
	}
}

func TestContinuationPlanShowsEachBlockState(t *testing.T) {
	result := previewResult()
	result.Continuation = true
	result.Steps[0].State = "done"
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Apply continuation plan") || !strings.Contains(out.String(), "[done]") {
		t.Fatalf("continuation plan = %q", out.String())
	}
}

func TestOperationResultLeadsWithItsOutcomeAndNamesItsLog(t *testing.T) {
	for state, token := range map[string]string{"done": "[OK]", "failed": "[FAIL]", "unknown": "[UNKNOWN]"} {
		t.Run(state, func(t *testing.T) {
			var out bytes.Buffer
			result := &lifecycle.OperationResult{
				Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "apply",
				Blocks:  []lifecycle.BlockResult{{ID: "artifact-server-lab", Description: "serve artifacts", State: state}},
				Logs:    []string{"op-abc/logs/operation.jsonl"},
				Receipt: lifecycle.Receipt{Operation: "op-abc", Verb: "apply", State: state, Next: "destroy"},
			}
			if err := writeLifecycleOperation(&out, result); err != nil {
				t.Fatal(err)
			}
			rendered := out.String()
			if !strings.HasPrefix(rendered, token+" Apply "+state) {
				t.Fatalf("headline = %q, want prefix %q", rendered, token)
			}
			if !strings.Contains(rendered, "op-abc/logs/operation.jsonl") {
				t.Fatal("the result did not name its private log")
			}
			if !strings.HasSuffix(rendered, "next: destroy\n") {
				t.Fatalf("result does not end with its receipt: %q", rendered)
			}
		})
	}
}

func TestProgressReportsEachBlockAndGroupUnbuffered(t *testing.T) {
	var out bytes.Buffer
	presenter := NewLifecycleProgressPresenter(&out)
	presenter.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: "artifact-server-lab", Status: "running", Position: 1, Total: 2})
	presenter.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: "artifact-server-lab", Group: "pull-image", Status: "ok"})
	rendered := out.String()
	if !strings.Contains(rendered, "artifact-server-lab (1/2)") || !strings.Contains(rendered, "artifact-server-lab pull-image") {
		t.Fatalf("progress = %q", rendered)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := out.Len()
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{Block: "artifact-server-lab", Status: "running"})
	if out.Len() != before {
		t.Fatal("progress continued after cancellation")
	}
}

// Presentation must never carry an attacker-chosen control sequence into the
// operator's terminal.
func TestLifecycleOutputEscapesUntrustedText(t *testing.T) {
	result := previewResult()
	result.Steps[0].Description = "serve\x1b[31m artifacts"
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("plan text carried an escape sequence: %q", out.String())
	}
}

func TestStatusRendersTextAndJSON(t *testing.T) {
	result := &lifecycle.StatusResult{
		Context:     lifecycle.ContextIdentity{Name: "lab", ID: "ctx-1", Revision: "rev-1"},
		SetupChecks: []lifecycle.SetupCheck{{ID: "controller-binding", Status: "ready"}},
		Shared:      []lifecycle.ServiceSummary{{Kind: "ArtifactServer", Name: "lab", Machine: "bastion", Status: "done"}},
		NextSteps:   []string{"bootwright destroy"},
		Lifecycle: &lifecycle.LifecycleSummary{
			Operation: "op-abc", Verb: "apply", State: "done", Next: "destroy",
			Blocks: []lifecycle.BlockResult{{ID: "artifact-server-lab", Description: "serve artifacts", State: "done"}},
			Logs:   []string{},
		},
	}
	var text bytes.Buffer
	if err := writeLifecycleStatus(&text, result, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Context lab", "controller-binding", "ArtifactServer/lab", "bootwright destroy"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("status text = %q, missing %q", text.String(), want)
		}
	}
	var encoded bytes.Buffer
	if err := writeLifecycleStatus(&encoded, result, true); err != nil {
		t.Fatal(err)
	}
	line := encoded.String()
	if !strings.HasPrefix(line, `{"schemaVersion":"v1alpha1","command":"status","ok":true,"exitCode":0,"result":{"context":`) {
		t.Fatalf("status JSON envelope = %q", line)
	}
	for _, want := range []string{`"setupChecks":`, `"desired":`, `"clusters":`, `"storageClusters":`, `"shared":`, `"secrets":`, `"nextSteps":`, `"lifecycle":`} {
		if !strings.Contains(line, want) {
			t.Fatalf("status JSON = %q, missing %q", line, want)
		}
	}
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatal("status JSON is not one document followed by one newline")
	}
}
