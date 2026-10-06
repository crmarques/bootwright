package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
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

// The headline state, a block description and the log directory read in the
// result exactly as the receipt below it escapes them, once.
func TestOperationResultTextEscapesOnce(t *testing.T) {
	raw := func(label string) string { return label + `\x` }
	shown := func(label string) string { return label + `\\x` }
	result := &lifecycle.OperationResult{
		Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: "apply",
		Blocks:      []lifecycle.BlockResult{{ID: "artifact-server-lab", Description: raw("description"), State: "done"}},
		LogLocation: raw("location"),
		Receipt:     lifecycle.Receipt{Operation: raw("operation"), Verb: "apply", State: raw("state"), Next: raw("next")},
	}
	var out bytes.Buffer
	if err := writeLifecycleOperation(&out, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `\\\\`) {
		t.Fatalf("operation text escaped a value twice: %q", out.String())
	}
	for _, want := range []string{
		"[FAIL] Apply " + shown("state") + "\n", "  [DONE]  " + shown("description") + "\n",
		"  Logs  " + shown("location") + "\n", "\nstate: " + shown("state") + "\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("operation text = %q, missing %q", out.String(), want)
		}
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
	// A settled invocation that first completed an interrupted finalization,
	// or a destroy that first released what an interrupted registration left,
	// says it did only that, in place of the note that it did nothing at all.
	for _, test := range []struct {
		verb, recovered, note, plain string
	}{
		{"apply", lifecycle.RecoveredFinalization, "Nothing to do: this invocation only completed an interrupted finalization.", "Nothing to do: this context already holds the state it declares."},
		{"destroy", lifecycle.RecoveredFinalization, "Nothing to remove: this invocation only completed an interrupted finalization.", "Nothing to remove: this context owns no realized state."},
		{"destroy", lifecycle.RecoveredRelease, "Nothing to remove: this invocation only released what an interrupted registration left.", "Nothing to remove: this context owns no realized state."},
	} {
		t.Run(test.verb+" after a "+test.recovered, func(t *testing.T) {
			var out bytes.Buffer
			result := &lifecycle.OperationResult{
				Context: lifecycle.ContextIdentity{Name: "lab"}, Verb: test.verb, Settled: true, Recovered: test.recovered,
				Receipt: lifecycle.Receipt{Operation: "op-abc", Verb: test.verb, State: "done", Next: "none"},
			}
			if err := writeLifecycleOperation(&out, result); err != nil {
				t.Fatal(err)
			}
			rendered := out.String()
			if !strings.Contains(rendered, "  "+test.note+"\n") {
				t.Fatalf("the settled %s did not name its %s: %q", test.verb, test.recovered, rendered)
			}
			if strings.Contains(rendered, test.plain) {
				t.Fatalf("the settled %s also said it did nothing: %q", test.verb, rendered)
			}
		})
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
		Context:     lifecycle.ContextIdentity{Name: "lab", Revision: "rev-1", Mode: "ready"},
		SetupChecks: []lifecycle.SetupCheck{{ID: "controller-binding", Status: "ready"}},
		Shared:      []lifecycle.ServiceSummary{{Kind: "ArtifactServer", Name: "lab", Machine: "controller", Status: "done"}},
		NextSteps:   []string{"bootwright apply", "bootwright destroy"},
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
	for _, want := range []string{"Context lab", "  Mode  ready\n", "Controller binding", "ArtifactServer/lab", "Registered by", "1.4.0 (9f2c1ab)"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("status text = %q, missing %q", text.String(), want)
		}
	}
	// Every next step is an action the operator may take, so none is dropped.
	for _, step := range result.NextSteps {
		if !strings.Contains(text.String(), "  "+step+"\n") {
			t.Fatalf("status text = %q, missing next step %q", text.String(), step)
		}
	}
	var encoded bytes.Buffer
	if err := writeLifecycleStatus(&encoded, result, true); err != nil {
		t.Fatal(err)
	}
	line := encoded.String()
	if !strings.HasPrefix(line, `{"schemaVersion":"v1alpha1","command":"status","ok":true,"exitCode":0,"result":{"context":{"name":"lab","mode":"ready"},`) {
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

// While a block is unproved or failed, the steps the next apply works first
// read resolve or retry, and every other ready step reads deferred behind the
// first of them.
func TestPlanResultMarksTheBlockTheNextApplyWorksFirst(t *testing.T) {
	for _, kind := range []string{lifecycle.StepRetry, lifecycle.StepResolve} {
		result := previewResult()
		result.Continuation, result.Stages = true, []string{"machines"}
		result.Steps = []lifecycle.PlanStep{
			{ID: "os-install-rhel-01", Description: "install rhel-01", Stage: "machines", State: "failed", Selection: kind},
			{ID: "os-install-rhel-02", Description: "install rhel-02", Stage: "machines", State: "pending", Selection: lifecycle.StepWaiting, WaitsOn: "os-install-rhel-01"},
		}
		result.Startable, result.Deferred = 1, 1
		var out bytes.Buffer
		if err := writeLifecyclePlan(&out, result); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"  1. install rhel-01 [machines] [failed] [" + kind + "]\n",
			"  2. install rhel-02 [machines] [pending] [deferred: waits on os-install-rhel-01]\n",
			"  Starts  1 of 2 blocks, 1 deferred\n",
		} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("plan text = %q, missing %q", out.String(), want)
			}
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

// Display escapes each value once, so every status value reads in text exactly
// as the safe display text JSON encodes it. Each input carries one backslash
// under a label no other input ends with, so a value escaped twice, or never,
// cannot pass for another.
func TestStatusTextEscapesOnce(t *testing.T) {
	raw := func(label string) string { return label + `\x` }
	shown := func(label string) string { return label + `\\x` }
	result := &lifecycle.StatusResult{
		Context:         lifecycle.ContextIdentity{Name: raw("ctx"), Revision: raw("rev"), Mode: raw("mode")},
		SetupChecks:     []lifecycle.SetupCheck{{ID: raw("check"), Status: raw("readiness")}},
		Desired:         lifecycle.DesiredSummary{Revision: raw("rev"), Environment: raw("env")},
		Clusters:        []lifecycle.ClusterSummary{{Name: raw("cname"), Kind: raw("ckind"), Status: lifecycle.RealizationStatus(raw("cstatus"))}},
		StorageClusters: []lifecycle.ClusterSummary{{Name: raw("sname"), Kind: raw("skind"), Status: lifecycle.RealizationStatus(raw("sstatus"))}},
		Shared:          []lifecycle.ServiceSummary{{Kind: raw("vkind"), Name: raw("vname"), Machine: raw("machine"), Status: lifecycle.RealizationStatus(raw("vstatus"))}},
		NextSteps:       []string{raw("step")},
		Lifecycle: &lifecycle.LifecycleSummary{
			Operation: raw("operation"), Verb: raw("verb"), State: raw("opstate"), Next: raw("next"),
			Blocks: []lifecycle.BlockResult{{
				ID: raw("block"), Description: raw("description"), Stage: raw("stage"), State: raw("bstate"),
				Unresolved: &lifecycle.Unresolved{Reason: raw("reason"), Remedy: raw("remedy")},
			}},
			Logs:       []string{raw("log")},
			Executable: raw("build"),
		},
		Contradictions: []string{raw("contradiction")},
		LogLocation:    raw("location"),
	}
	var text bytes.Buffer
	if err := writeLifecycleStatus(&text, result, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.String(), `\\\\`) {
		t.Fatalf("status text escaped a value twice: %q", text.String())
	}
	for _, want := range []string{
		"[OK] Context " + shown("ctx") + "\n", "  Mode  " + shown("mode") + "\n", "  [UNKNOWN]  " + shown("check") + "\n",
		"  Revision         " + shown("rev") + "\n", "  Environment      " + shown("env") + "\n",
		"  [UNKNOWN]  " + shown("ckind") + "/" + shown("cname") + "\n", "  [UNKNOWN]  " + shown("skind") + "/" + shown("sname") + "\n",
		"  [UNKNOWN]  " + shown("vkind") + "/" + shown("vname") + "  " + shown("machine") + "\n", "  " + shown("step") + "\n",
		"  Operation  " + shown("operation") + "\n", "  Verb       " + shown("verb") + "\n",
		"  State      " + shown("opstate") + "\n", "  Next       " + shown("next") + "\n",
		"  [FAIL]  " + shown("description") + "\n",
		"  Registered by  " + shown("build") + "\n", "  Logs           " + shown("location") + "\n",
		"Contradictions\n  " + shown("contradiction") + "\n",
		"Unresolved " + shown("block") + "\n  Reason  " + shown("reason") + "\n  Remedy  " + shown("remedy") + "\n",
	} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("status text = %q, missing %q", text.String(), want)
		}
	}
	var encoded bytes.Buffer
	if err := writeLifecycleStatus(&encoded, result, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded.String(), `"setupChecks":[{"id":"check\\\\x","status":"readiness\\\\x"}]`) {
		t.Fatalf("status JSON = %q, want the check as check\\\\x", encoded.String())
	}
	var envelope struct {
		Result any `json:"result"`
	}
	if err := json.Unmarshal(encoded.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	collectJSONStrings("", envelope.Result, got)
	want := map[string]string{}
	for path, label := range map[string]string{
		"context.name": "ctx", "context.mode": "mode",
		"setupChecks[0].id": "check", "setupChecks[0].status": "readiness",
		"desired.revision": "rev", "desired.environment": "env",
		"clusters[0].name": "cname", "clusters[0].kind": "ckind", "clusters[0].status": "cstatus",
		"storageClusters[0].name": "sname", "storageClusters[0].kind": "skind", "storageClusters[0].status": "sstatus",
		"shared[0].kind": "vkind", "shared[0].name": "vname", "shared[0].machine": "machine", "shared[0].status": "vstatus",
		"nextSteps[0]":        "step",
		"lifecycle.operation": "operation", "lifecycle.verb": "verb", "lifecycle.state": "opstate", "lifecycle.next": "next",
		"lifecycle.blocks[0].id": "block", "lifecycle.blocks[0].description": "description",
		"lifecycle.blocks[0].stage": "stage", "lifecycle.blocks[0].state": "bstate",
		"lifecycle.blocks[0].unresolved.reason": "reason", "lifecycle.blocks[0].unresolved.remedy": "remedy",
		"lifecycle.logs[0]": "log",
		"contradictions[0]": "contradiction",
	} {
		want[path] = shown(label)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status JSON strings = %q, want %q", got, want)
	}
}

// collectJSONStrings records every string a decoded JSON value holds under its
// path, so a test can require each one and admit no other.
func collectJSONStrings(path string, value any, into map[string]string) {
	switch typed := value.(type) {
	case string:
		into[path] = typed
	case map[string]any:
		for key, element := range typed {
			if path != "" {
				key = path + "." + key
			}
			collectJSONStrings(key, element, into)
		}
	case []any:
		for index, element := range typed {
			collectJSONStrings(path+"["+strconv.Itoa(index)+"]", element, into)
		}
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
		"Concurrency  3 waves, widest 2 steps\n",
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
	if !strings.Contains(out.String(), "Concurrency  2 waves, widest 1 step\n") {
		t.Fatalf("plan text = %q", out.String())
	}
}

// The plan's own width and the build's bound are two facts: a plan wider than
// the blocks this build starts at a time says how many it starts, so a wide
// plan never reads as running wider than it does, and one the bound covers
// adds nothing.
func TestPlanReportsItsWidthAndTheBuildsBound(t *testing.T) {
	for _, test := range []struct {
		waves, widest, bound int
		want                 string
	}{
		{4, 5, 1, "4 waves, widest 5 steps; this build starts 1 block at a time"},
		{4, 5, 2, "4 waves, widest 5 steps; this build starts 2 blocks at a time"},
		{4, 5, 5, "4 waves, widest 5 steps"},
		{4, 5, 8, "4 waves, widest 5 steps"},
		{1, 1, 1, "1 wave, widest 1 step"},
		{3, 2, 0, "3 waves, widest 2 steps"},
	} {
		result := previewResult()
		result.Waves, result.Widest, result.Bound = test.waves, test.widest, test.bound
		var out bytes.Buffer
		if err := writeLifecyclePlan(&out, result); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "\n  Concurrency  "+test.want+"\n") || strings.Contains(out.String(), "at once") {
			t.Fatalf("waves %d, widest %d, bound %d: plan text = %q, want %q", test.waves, test.widest, test.bound, out.String(), test.want)
		}
	}
}

// Every presentation marks a step that consumes an authorization and closes
// with the tokens the plan requires and the steps that consume each, by their
// place in the plan; a plan that consumes none requires nothing.
func TestPlanMarksTheStepsAnAuthorizationAcknowledges(t *testing.T) {
	result := previewResult()
	result.Continuation = true
	result.Steps = []lifecycle.PlanStep{
		{ID: "machine", Description: "realize the machine rhel-01", Stage: "machines", State: "done", Consumes: []string{"data-loss"}},
		{ID: "artifacts", Description: "serve artifacts for lab", Stage: "infra-components", State: "pending", Selection: lifecycle.StepStart},
		{ID: "metal", Description: "install metal-01", Stage: "machines", State: "pending", Consumes: []string{"data-loss"}, Selection: lifecycle.StepNotSelected, After: []int{2}},
	}
	var out bytes.Buffer
	if err := writeLifecyclePlan(&out, result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  1. realize the machine rhel-01 [machines] [done] [data-loss]\n",
		"  2. serve artifacts for lab [infra-components] [pending] [start]\n",
		"  3. install metal-01 [machines] [pending] [data-loss] [not selected] [after 2]\n",
		"\n  Requires  --authorize data-loss (step 1, 3)\noperation: ",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("plan text = %q, missing %q", out.String(), want)
		}
	}
	out.Reset()
	if err := writeLifecyclePlan(&out, previewResult()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Requires") || strings.Contains(out.String(), "[data-loss]") {
		t.Fatalf("a plan that consumes nothing required an authorization: %q", out.String())
	}
}

// The presenter writes the plan apply and destroy confirm as its exact bytes
// and nothing once the invocation is cancelled. A plan it could not write ends
// the invocation with status 1 and no further output, because nothing written
// after it could be trusted; a presenter that was never configured says so.
func TestLifecyclePlanPresenterWritesBeforeConfirmationAndStopsOnFailure(t *testing.T) {
	result := previewResult()
	result.Steps[0].Consumes = []string{"data-loss"}
	result.Waves, result.Widest, result.Bound = 1, 1, 1
	var out bytes.Buffer
	if err := NewLifecyclePlanPresenter(&out).PresentLifecyclePlan(context.Background(), *result); err != nil {
		t.Fatal(err)
	}
	want := "Apply plan\n\nPlan\n" +
		"  1. serve artifacts for lab on controller [infra-components] [data-loss]\n" +
		"       open-listener 192.0.2.1:8443\n\n" +
		"  Concurrency  1 wave, widest 1 step\n" +
		"  Requires     --authorize data-loss (step 1)\n"
	if out.String() != want {
		t.Fatalf("presented %q, want %q", out.String(), want)
	}
	out.Reset()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewLifecyclePlanPresenter(&out).PresentLifecyclePlan(canceled, *result); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("a cancelled presentation = %v, wrote %q", err, out.String())
	}
	for name, presenter := range map[string]*LifecyclePlanPresenter{"a failing writer": NewLifecyclePlanPresenter(rejectingWriter{}), "no writer": nil} {
		t.Run(name, func(t *testing.T) {
			presented := presenter.PresentLifecyclePlan(context.Background(), *result)
			record := &dispatchRecord{err: presented}
			var stdout, stderr bytes.Buffer
			code := New(Config{Out: &stdout, ErrOut: &stderr, Services: dispatchSpies(record)}).Run(context.Background(), []string{"apply", "--yes"})
			wantErr := ""
			if presenter == nil {
				wantErr = "[FAIL] runtime.internal: lifecycle plan presentation is not configured\n"
			}
			if code != 1 || stdout.Len() != 0 || stderr.String() != wantErr {
				t.Fatalf("exit %d, stdout %q, stderr %q, want stderr %q", code, stdout.String(), stderr.String(), wantErr)
			}
		})
	}
}
