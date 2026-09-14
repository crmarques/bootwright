package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func previewResult() *lifecycle.PlanResult {
	return &lifecycle.PlanResult{
		Context: lifecycle.ContextIdentity{Name: "lab"},
		Verb:    "apply",
		Steps: []lifecycle.PlanStep{{
			ID: "artifact-server-lab", Description: "serve artifacts for lab on controller",
			Stage: "infra-components", Impacts: []string{"open-listener 192.0.2.1:8443"}, State: "pending",
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
	if !strings.Contains(rendered, "Apply plan") || !strings.Contains(rendered, "1. serve artifacts for lab on controller") {
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
	presenter := NewLifecycleProgressPresenter(&out, false)
	presenter.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: "artifact-server-lab", Description: "serve artifacts on lab", Status: "running", Position: 1, Total: 2})
	presenter.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: "artifact-server-lab", Description: "serve artifacts on lab", Group: "pull-image", Detail: "acquire the pinned server image", Status: "ok", Position: 1, Total: 2})
	presenter.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: "artifact-server-lab", Description: "serve artifacts on lab", Status: "done", Position: 1, Total: 2})
	want := "\nProgress\n" +
		"  [RUNNING]  serve artifacts on lab (1/2)\n" +
		"  [OK]       serve artifacts on lab: acquire the pinned server image (1/2)\n" +
		"  [DONE]     serve artifacts on lab (1/2)\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
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
		Context:     lifecycle.ContextIdentity{Name: "lab", Revision: "rev-1"},
		SetupChecks: []lifecycle.SetupCheck{{ID: "controller-binding", Status: "ready"}},
		Shared:      []lifecycle.ServiceSummary{{Kind: "ArtifactServer", Name: "lab", Machine: "controller", Status: "done"}},
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

// A selection never narrows the plan: every block stays listed, and each
// pending one says whether this invocation would start it and why not.
func TestPlanResultMarksWhatAStageSelectionWouldStart(t *testing.T) {
	result := previewResult()
	result.Stages = []string{"substrates"}
	result.Steps[0].Selection = lifecycle.StepNotSelected
	result.Steps = append(result.Steps,
		lifecycle.PlanStep{ID: "provider-metal", Description: "realize metal", Stage: "substrates", State: "pending", Selection: lifecycle.StepStart},
		lifecycle.PlanStep{ID: "provider-kubevirt", Description: "realize kubevirt", Stage: "substrates", State: "pending", Selection: lifecycle.StepWaiting, WaitsOn: "host-virtualization"})
	result.Startable, result.Deferred = 1, 2
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	for _, want := range []string{
		"1. serve artifacts for lab on controller [infra-components] [not selected]",
		"2. realize metal [substrates] [start]",
		"3. realize kubevirt [substrates] [deferred: waits on host-virtualization]",
		"Stages  substrates",
		"Starts  1 of 3 blocks, 2 deferred",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("plan text = %q, missing %q", rendered, want)
		}
	}
}

func TestPlanResultWithoutASelectionCarriesNoMarker(t *testing.T) {
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, previewResult()); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "1. serve artifacts for lab on controller [infra-components]") {
		t.Fatalf("plan text = %q", rendered)
	}
	for _, absent := range []string{"[start]", "[not selected]", "Stages", "Starts"} {
		if strings.Contains(rendered, absent) {
			t.Fatalf("plan text = %q, unexpected %q", rendered, absent)
		}
	}
}

// A pause is a successful stop, so it leads with OK and names the continuation.
func TestPausedOperationReportsSuccessAndItsContinuation(t *testing.T) {
	var out bytes.Buffer
	result := &lifecycle.OperationResult{
		Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "apply",
		Blocks: []lifecycle.BlockResult{
			{ID: "artifact-server-lab", Description: "serve artifacts", Stage: "infra-components", State: "done"},
			{ID: "provider-metal", Description: "realize metal", Stage: "substrates", State: "pending"},
		},
		Receipt: lifecycle.Receipt{Operation: "op-abc", Verb: "apply", State: "paused", Next: "continue-apply"},
	}
	if err := writeLifecycleOperation(&out, result); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if !strings.HasPrefix(rendered, "[OK] Apply paused") {
		t.Fatalf("headline = %q", rendered)
	}
	if !strings.HasSuffix(rendered, "operation: op-abc\nverb: apply\nstate: paused\nnext: continue-apply\n") {
		t.Fatalf("receipt = %q", rendered)
	}
}

func TestFailedOperationKeepsItsResultLogAndReceipt(t *testing.T) {
	for _, path := range []string{"apply", "destroy"} {
		t.Run(path, func(t *testing.T) {
			operation := &lifecycle.OperationResult{
				Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: path,
				Blocks: []lifecycle.BlockResult{
					{ID: "artifact-server-lab", Description: "serve artifacts for lab-artifacts", State: "failed"},
					{ID: "dns-lab", Description: "resolve names for lab-dns", State: "pending"},
				},
				Logs:    []string{"op-abc/logs/blocks/artifact-server-lab/attempt-000001.jsonl"},
				Receipt: lifecycle.Receipt{Operation: "op-abc", Verb: path, State: "failed", Next: "continue-" + path},
			}
			failure := &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: "lifecycle.state", Message: "the operation did not complete"}}}
			record := &dispatchRecord{result: commandResult{lifecycleOperation: operation}, err: failure}
			var out, errOut bytes.Buffer
			code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{path, "--yes"})
			if code != 1 {
				t.Fatalf("code = %d", code)
			}
			rendered := out.String()
			if !strings.Contains(rendered, "serve artifacts for lab-artifacts") {
				t.Fatalf("result rows missing: %q", rendered)
			}
			if !strings.Contains(rendered, "op-abc/logs/blocks/artifact-server-lab/attempt-000001.jsonl") {
				t.Fatalf("the failure did not name its private log: %q", rendered)
			}
			if !strings.HasSuffix(rendered, "next: continue-"+path+"\n") {
				t.Fatalf("result does not end with its receipt: %q", rendered)
			}
			if errOut.String() != "[FAIL] lifecycle.state: the operation did not complete\n" {
				t.Fatalf("stderr = %q", errOut.String())
			}
		})
	}
}

func TestRefusedOperationPresentsOnlyItsDiagnostic(t *testing.T) {
	failure := &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: "lifecycle.stage", Message: "no block carries the selected stage"}}}
	record := &dispatchRecord{err: failure}
	var out, errOut bytes.Buffer
	code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"apply", "--stage", "substrates", "--yes"})
	if code != 1 || out.String() != "" {
		t.Fatalf("refusal code=%d stdout=%q", code, out.String())
	}
	if errOut.String() != "[FAIL] lifecycle.stage: no block carries the selected stage\n" {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestStatusNamesTheLogOfAnIncompleteOperation(t *testing.T) {
	result := &lifecycle.StatusResult{
		Context:   lifecycle.ContextIdentity{Name: "lab", Revision: "rev-1"},
		NextSteps: []string{"bootwright apply"},
		Lifecycle: &lifecycle.LifecycleSummary{
			Operation: "op-abc", Verb: "apply", State: "failed", Next: "continue-apply",
			Blocks: []lifecycle.BlockResult{{ID: "artifact-server-lab", Description: "serve artifacts", State: "failed"}},
			Logs:   []string{"op-abc/logs/blocks/artifact-server-lab/attempt-000001.jsonl"},
		},
	}
	var text bytes.Buffer
	if err := writeLifecycleStatus(&text, result, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "op-abc/logs/blocks/artifact-server-lab/attempt-000001.jsonl") {
		t.Fatalf("status did not name the operation log: %q", text.String())
	}
	if !strings.Contains(text.String(), "bootwright apply") {
		t.Fatalf("status dropped its next step: %q", text.String())
	}
}
