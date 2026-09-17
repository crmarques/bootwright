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
	if strings.Contains(rendered, "\nImpacts\n") {
		t.Fatalf("the plan lists impacts apart from the steps that cause them: %q", rendered)
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

// An effect means nothing without the change that causes it, so each block's
// impacts are indented under its own step rather than pooled in one list.
func TestPlanNestsEachImpactUnderTheStepThatCausesIt(t *testing.T) {
	result := previewResult()
	result.Steps[0].Impacts = []string{"open-listener 192.0.2.1:8443", "create-path /var/lib/artifacts"}
	result.Steps = append(result.Steps, lifecycle.PlanStep{
		ID: "dns-lab", Description: "resolve names for lab-dns on controller", Stage: "infra-components",
		Impacts: []string{"open-listener 192.0.2.1:53"}, State: "pending",
	})
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	want := "Plan\n" +
		"  1. serve artifacts for lab on controller [infra-components]\n" +
		"       open-listener 192.0.2.1:8443\n" +
		"       create-path /var/lib/artifacts\n" +
		"  2. resolve names for lab-dns on controller [infra-components]\n" +
		"       open-listener 192.0.2.1:53\n"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("plan text = %q, want %q", out.String(), want)
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
				Blocks:      []lifecycle.BlockResult{{ID: "artifact-server-lab", Description: "serve artifacts", State: state}},
				Logs:        []string{"op-abc/logs/operation.jsonl"},
				LogLocation: "/var/lib/bootwright/contexts/lab/state/operations/op-abc/logs",
				Receipt:     lifecycle.Receipt{Operation: "op-abc", Verb: "apply", State: state, Next: "destroy"},
			}
			if err := writeLifecycleOperation(&out, result); err != nil {
				t.Fatal(err)
			}
			rendered := out.String()
			if !strings.HasPrefix(rendered, token+" Apply "+state) {
				t.Fatalf("headline = %q, want prefix %q", rendered, token)
			}
			if !strings.Contains(rendered, "Logs  /var/lib/bootwright/contexts/lab/state/operations/op-abc/logs\n") {
				t.Fatalf("the result did not name where its logs are: %q", rendered)
			}
			if !strings.HasSuffix(rendered, "next: destroy\n") {
				t.Fatalf("result does not end with its receipt: %q", rendered)
			}
		})
	}
}

// A settled result lists blocks an earlier operation completed, so it says so
// rather than leaving them to read as work this invocation performed.
func TestSettledOperationSaysItDidNothing(t *testing.T) {
	var out bytes.Buffer
	result := &lifecycle.OperationResult{
		Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "apply",
		Blocks:  []lifecycle.BlockResult{{ID: "artifact-server-lab", Description: "serve artifacts", State: "done"}},
		Settled: true,
		Receipt: lifecycle.Receipt{Operation: "op-abc", Verb: "apply", State: "done", Next: "none"},
	}
	if err := writeLifecycleOperation(&out, result); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if !strings.HasPrefix(rendered, "[OK] Apply done") {
		t.Fatalf("headline = %q", rendered)
	}
	if !strings.Contains(rendered, "Nothing to do: this context already holds the state it declares.") {
		t.Fatalf("a settled apply did not say it did nothing: %q", rendered)
	}
	if !strings.HasSuffix(rendered, "operation: op-abc\nverb: apply\nstate: done\nnext: none\n") {
		t.Fatalf("settled result does not end with its receipt: %q", rendered)
	}
	out.Reset()
	result.Verb, result.Blocks, result.Receipt = "destroy", nil, lifecycle.Receipt{Operation: "none", Verb: "destroy", State: "done", Next: "none"}
	if err := writeLifecycleOperation(&out, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nothing to remove: this context owns no realized state.") {
		t.Fatalf("a settled destroy did not say it removed nothing: %q", out.String())
	}
}

// A block occupies one row: each group opens it with the work in flight and
// the share of the block its settled groups have proved, and no group settles
// as a row of its own.
func TestProgressReportsEachBlockAsOneRowUnbuffered(t *testing.T) {
	var out bytes.Buffer
	presenter := NewLifecycleProgressPresenter(&out, nil)
	block := lifecycle.ProgressEvent{Block: "artifact-server-lab", Description: "serve artifacts on lab", Position: 1, Total: 2}
	group := func(id, detail, status string, completed int) lifecycle.ProgressEvent {
		event := block
		event.Group, event.Detail, event.Status = id, detail, status
		event.Completed, event.Declared = completed, 2
		return event
	}
	block.Status = "running"
	presenter.ReportProgress(context.Background(), block)
	presenter.ReportProgress(context.Background(), group("pull-image", "acquire the pinned server image", "running", 0))
	presenter.ReportProgress(context.Background(), group("pull-image", "acquire the pinned server image", "ok", 1))
	presenter.ReportProgress(context.Background(), group("start-service", "start the managed service", "running", 1))
	block.Status = "done"
	presenter.ReportProgress(context.Background(), block)
	want := "\nProgress\n" +
		"  [RUNNING]  [1/2] serve artifacts on lab\n" +
		"  [RUNNING]  [1/2] serve artifacts on lab: acquire the pinned server image - 0%\n" +
		"  [RUNNING]  [1/2] serve artifacts on lab: start the managed service - 50%\n" +
		"  [DONE]     [1/2] serve artifacts on lab\n"
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
			Blocks:     []lifecycle.BlockResult{{ID: "artifact-server-lab", Description: "serve artifacts", State: "done"}},
			Logs:       []string{},
			Executable: "1.4.0 (9f2c1ab)",
		},
	}
	var text bytes.Buffer
	if err := writeLifecycleStatus(&text, result, false); err != nil {
		t.Fatal(err)
	}
	// The build that registered the operation is what a removal is planned
	// from, so a refusal naming it as the remedy is readable in advance.
	for _, want := range []string{"Context lab", "controller-binding", "ArtifactServer/lab", "bootwright destroy", "Registered by", "1.4.0 (9f2c1ab)"} {
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
				Logs:        []string{"op-abc/logs/blocks/artifact-server-lab/attempt-000001.jsonl"},
				LogLocation: "/var/lib/bootwright/contexts/lab/state/operations/op-abc/logs",
				Receipt:     lifecycle.Receipt{Operation: "op-abc", Verb: path, State: "failed", Next: "continue-" + path},
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
			if !strings.Contains(rendered, "Logs  /var/lib/bootwright/contexts/lab/state/operations/op-abc/logs\n") {
				t.Fatalf("the failure did not name where its logs are: %q", rendered)
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
		LogLocation: "/var/lib/bootwright/contexts/lab/state/operations/op-abc/logs",
	}
	var text bytes.Buffer
	if err := writeLifecycleStatus(&text, result, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "Logs  /var/lib/bootwright/contexts/lab/state/operations/op-abc/logs\n") {
		t.Fatalf("status did not name where the operation logs are: %q", text.String())
	}
	if !strings.Contains(text.String(), "bootwright apply") {
		t.Fatalf("status dropped its next step: %q", text.String())
	}
}

// The location is a line of its own, written before the first row and never
// redrawn over, so an operator can copy it while the work is still running.
func TestProgressNamesTheLogLocationBeforeItsFirstRow(t *testing.T) {
	var out bytes.Buffer
	presenter := NewLifecycleProgressPresenter(&out, nil)
	ctx := context.Background()
	presenter.ReportLogLocation(ctx, "/var/lib/bootwright/contexts/lab/state/operations/op-abc/logs")
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Block: "artifact-server-lab", Description: "serve artifacts on lab", Status: "running", Position: 1, Total: 1,
	})
	want := "\n  Logs  /var/lib/bootwright/contexts/lab/state/operations/op-abc/logs\n" +
		"\nProgress\n" +
		"  [RUNNING]  [1/1] serve artifacts on lab\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// What a removal proves before it registers is not an effect of its plan, so
// it settles under its own heading as one row however many blocks it probes.
// Reported as effects it produced a second row per block, ahead of the log
// location, and an operator read every step of the removal twice.
func TestQuiescenceChecksSettleAsOneRowBeforeTheEffects(t *testing.T) {
	var out bytes.Buffer
	presenter := NewLifecycleProgressPresenter(&out, nil)
	ctx := context.Background()
	check := func(group, detail, status string, completed int) lifecycle.ProgressEvent {
		return lifecycle.ProgressEvent{
			Phase: lifecycle.CheckPhase, Block: "quiescence",
			Description: "prove nothing this removal takes back is still in use",
			Group:       group, Detail: detail, Status: status,
			Completed: completed, Declared: 2,
		}
	}
	presenter.ReportProgress(ctx, check("machine-rhel-01", "Machine/rhel-01", "running", 0))
	presenter.ReportProgress(ctx, check("artifact-server-lab", "ArtifactServer/lab", "running", 1))
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Phase: lifecycle.CheckPhase, Block: "quiescence",
		Description: "prove nothing this removal takes back is still in use", Status: "ok",
	})
	presenter.ReportLogLocation(ctx, "/var/lib/bootwright/contexts/lab/state/operations/op-abc/logs")
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Block: "machine-rhel-01", Description: "remove the virtual machine rhel-01", Status: "done", Position: 1, Total: 2,
	})
	want := "\nChecks\n" +
		"  [RUNNING]  prove nothing this removal takes back is still in use: Machine/rhel-01 - 0%\n" +
		"  [RUNNING]  prove nothing this removal takes back is still in use: ArtifactServer/lab - 50%\n" +
		"  [OK]       prove nothing this removal takes back is still in use\n" +
		"\n  Logs  /var/lib/bootwright/contexts/lab/state/operations/op-abc/logs\n" +
		"\nProgress\n" +
		"  [DONE]     [1/2] remove the virtual machine rhel-01\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// A removal over an apply that did not finish proves the outcome of what it
// takes back before it proves nothing is in use. Both precede every effect, so
// each settles as one row under the same heading, in the order it is performed.
func TestResolutionChecksSettleAsOneRowBeforeTheGate(t *testing.T) {
	var out bytes.Buffer
	presenter := NewLifecycleProgressPresenter(&out, nil)
	ctx := context.Background()
	resolution := func(group, detail, status string, completed int) lifecycle.ProgressEvent {
		return lifecycle.ProgressEvent{
			Phase: lifecycle.CheckPhase, Block: "resolution",
			Description: "prove the outcome of every effect this removal takes back",
			Group:       group, Detail: detail, Status: status,
			Completed: completed, Declared: 2,
		}
	}
	presenter.ReportProgress(ctx, resolution("dns-lab", "resolve names for lab-dns", "running", 0))
	presenter.ReportProgress(ctx, resolution("ntp-lab", "serve time for lab-ntp", "running", 1))
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Phase: lifecycle.CheckPhase, Block: "resolution",
		Description: "prove the outcome of every effect this removal takes back", Status: "ok",
	})
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Phase: lifecycle.CheckPhase, Block: "quiescence",
		Description: "prove nothing this removal takes back is still in use", Status: "ok",
	})
	want := "\nChecks\n" +
		"  [RUNNING]  prove the outcome of every effect this removal takes back: resolve names for lab-dns - 0%\n" +
		"  [RUNNING]  prove the outcome of every effect this removal takes back: serve time for lab-ntp - 50%\n" +
		"  [OK]       prove the outcome of every effect this removal takes back\n" +
		"  [OK]       prove nothing this removal takes back is still in use\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// A removal that cannot prove an effect settles its proof as unknown and
// registers nothing, so no Progress heading ever opens.
func TestUnresolvedRemovalChecksSettleAsUnknown(t *testing.T) {
	var out bytes.Buffer
	presenter := NewLifecycleProgressPresenter(&out, nil)
	ctx := context.Background()
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Phase: lifecycle.CheckPhase, Block: "resolution",
		Description: "prove the outcome of every effect this removal takes back",
		Group:       "dns-lab", Detail: "resolve names for lab-dns", Status: "running", Declared: 1,
	})
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Phase: lifecycle.CheckPhase, Block: "resolution",
		Description: "prove the outcome of every effect this removal takes back", Status: "unknown",
	})
	want := "\nChecks\n" +
		"  [RUNNING]  prove the outcome of every effect this removal takes back: resolve names for lab-dns - 0%\n" +
		"  [UNKNOWN]  prove the outcome of every effect this removal takes back\n"
	if out.String() != want {
		t.Fatalf("progress = %q, want %q", out.String(), want)
	}
}

// On a terminal the whole gate occupies the one line its rows redraw, so the
// heading that follows it is the only thing that terminates it.
func TestTerminalQuiescenceChecksOccupyOneLine(t *testing.T) {
	var out bytes.Buffer
	presenter := NewLifecycleProgressPresenter(&out, func() int { return 120 })
	ctx := context.Background()
	for _, detail := range []string{"Machine/rhel-01", "ArtifactServer/lab"} {
		presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
			Phase: lifecycle.CheckPhase, Block: "quiescence", Description: "prove nothing is in use",
			Group: detail, Detail: detail, Status: "running", Declared: 2,
		})
	}
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Phase: lifecycle.CheckPhase, Block: "quiescence", Description: "prove nothing is in use", Status: "ok",
	})
	rows, _ := strings.CutPrefix(out.String(), "\nChecks\n")
	if lines := strings.Count(rows, "\n"); lines != 1 {
		t.Fatalf("the gate wrote %d lines under its heading, want one: %q", lines, out.String())
	}
	if !strings.HasSuffix(out.String(), "  [OK]       prove nothing is in use\n") {
		t.Fatalf("the gate did not settle as its own row: %q", out.String())
	}
}

// A terminal rewrites running rows in place, so the location has to terminate
// the open row rather than be erased by the next redraw.
func TestTerminalLogLocationSurvivesTheRedrawnRow(t *testing.T) {
	var out bytes.Buffer
	presenter := NewLifecycleProgressPresenter(&out, func() int { return 120 })
	ctx := context.Background()
	presenter.ReportProgress(ctx, lifecycle.ProgressEvent{
		Block: "one", Description: "serve artifacts", Status: "running", Position: 1, Total: 2,
	})
	presenter.ReportLogLocation(ctx, "/var/lib/bootwright/contexts/lab/state/operations/op-abc/logs")
	rendered := out.String()
	location := "\n  Logs  /var/lib/bootwright/contexts/lab/state/operations/op-abc/logs\n"
	if !strings.HasSuffix(rendered, location) {
		t.Fatalf("progress = %q, want it to end with %q", rendered, location)
	}
	if !strings.Contains(rendered, "[RUNNING]  [1/2] serve artifacts\n") {
		t.Fatalf("the open row was not terminated before the location: %q", rendered)
	}
}

// The plan says what orders the work: each step names the steps it waits for
// by their place in the list, and a closing field says how much of the plan
// its own shape lets run at once. A step that waits for nothing names none,
// which is what marks it as one of the first to start.
func TestPlanNamesTheStepsEachOneWaitsFor(t *testing.T) {
	result := previewResult()
	result.Waves, result.Widest = 3, 2
	result.Steps = []lifecycle.PlanStep{
		{ID: "artifacts", Description: "serve artifacts for lab", Stage: "infra-components", State: "pending", Wave: 1},
		{ID: "names", Description: "resolve names for lab", Stage: "infra-components", State: "pending", Wave: 1},
		{ID: "provider", Description: "realize the libvirt host", Stage: "substrates", State: "pending", After: []int{1, 2}, Wave: 2},
		{ID: "machine", Description: "realize the machine rhel-01", Stage: "machines", State: "pending", After: []int{3}, Wave: 3},
	}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	for _, want := range []string{
		"1. serve artifacts for lab [infra-components]\n",
		"3. realize the libvirt host [substrates] [after 1, 2]\n",
		"4. realize the machine rhel-01 [machines] [after 3]\n",
		"Concurrency  3 waves, up to 2 steps at once\n",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("plan text %q omits %q", rendered, want)
		}
	}
	if strings.Contains(rendered, "1. serve artifacts for lab [infra-components] [after") {
		t.Fatalf("a step that waits for nothing named a predecessor: %q", rendered)
	}
}

// A plan whose steps are one chain says so, so a long plan that cannot be made
// any faster is not read as one that could.
func TestPlanReportsAChainAsOneStepAtATime(t *testing.T) {
	result := previewResult()
	result.Waves, result.Widest = 2, 1
	result.Steps = []lifecycle.PlanStep{
		{ID: "first", Description: "prepare the controller", Stage: "controller", State: "pending", Wave: 1},
		{ID: "second", Description: "serve artifacts for lab", Stage: "infra-components", State: "pending", After: []int{1}, Wave: 2},
	}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Concurrency  2 waves, up to 1 step at once\n") {
		t.Fatalf("plan text = %q", out.String())
	}
}
