package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestLifecycleMeterCountsOnlyDurableBlocks(t *testing.T) {
	model := &lifecyclePresentation{}
	model.seed(lifecycle.PlanResult{Steps: []lifecycle.PlanStep{
		{ID: "ready", State: "done"}, {ID: "work", State: "pending"},
	}})
	opts := PresentationOptions{Format: "text", Color: "never"}
	assertPercent := func(percent string) {
		t.Helper()
		rows := model.rows(opts)
		if len(rows) != 1 || !strings.Contains(rows[0], percent+"%") {
			t.Fatalf("meter = %q, want %s%%", rows, percent)
		}
	}
	assertPercent("50")
	model.report(lifecycle.ProgressEvent{Block: "work", Group: "group", Status: "done", Completed: 1, Declared: 1})
	assertPercent("50")
	for _, state := range []string{"running", "failed", "canceled", "unknown", "skipped"} {
		model.report(lifecycle.ProgressEvent{Block: "work", Status: state})
		assertPercent("50")
	}
	model.report(lifecycle.ProgressEvent{Phase: lifecycle.CheckPhase, Block: "work", Status: "done"})
	assertPercent("50")
	model.report(lifecycle.ProgressEvent{Block: "work", Status: "done"})
	model.report(lifecycle.ProgressEvent{Block: "work", Status: "done"})
	assertPercent("100")
}

func TestLifecycleDependencyDisplayUsesFrozenEdges(t *testing.T) {
	model := &lifecyclePresentation{}
	plan := lifecycle.PlanResult{Steps: []lifecycle.PlanStep{
		{ID: "first", State: "pending"},
		{ID: "second", Description: "Second", State: "pending", After: []int{1}},
	}}
	model.seed(plan)
	plan.Steps[1].After[0] = 2
	opts := PresentationOptions{Format: "text", Detail: "full", Color: "never"}
	rows := model.rows(opts)
	if len(rows) != 2 || !strings.Contains(rows[1], "waiting on steps 1") {
		t.Fatalf("dependency rows = %q", rows)
	}
	model.report(lifecycle.ProgressEvent{Block: "first", Status: "done"})
	if rows := model.rows(opts); len(rows) != 1 {
		t.Fatalf("settled dependency still displayed: %q", rows)
	}
}

func TestLifecyclePresentersShareContinuationAndFinishBeforeReceipt(t *testing.T) {
	var output bytes.Buffer
	out := WithPresentation(&output, PresentationOptions{Format: "text", Color: "never"})
	plan, progress := NewLifecyclePresenters(out, func() int { return 100 }, false)
	err := plan.PresentLifecyclePlan(context.Background(), lifecycle.PlanResult{Verb: "apply", Continuation: true, Steps: []lifecycle.PlanStep{
		{ID: "first", Description: "First", State: "done"},
		{ID: "second", Description: "Second", State: "pending"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	progress.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: "second", Description: "Second", Status: "running", Position: 2, Total: 2})
	progress.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: "second", Description: "Second", Status: "done", Position: 2, Total: 2})
	progress.Finish()
	if !strings.Contains(output.String(), "50%") || !strings.Contains(output.String(), "100%") || !strings.HasSuffix(output.String(), "\n") {
		t.Fatalf("progress = %q", output.String())
	}
}

func TestLifecyclePlanPreservesWaveOrderAndImpactDisclosure(t *testing.T) {
	result := lifecycle.PlanResult{Verb: "apply", Steps: []lifecycle.PlanStep{
		{ID: "a", Description: "First", Stage: "controller", State: "pending"},
		{ID: "b", Description: "Second", Stage: "machines", State: "pending", Impacts: []string{"Removes all selected data"}, Consumes: []string{"data-loss"}, After: []int{1}},
		{ID: "c", Description: "Third", Stage: "controller", State: "pending", After: []int{2}},
	}}
	for _, detail := range []string{"compact", "full"} {
		var out bytes.Buffer
		if err := NewLifecyclePlanPresenter(WithPresentation(&out, PresentationOptions{Format: "text", Color: "never", Detail: detail})).PresentLifecyclePlan(context.Background(), result); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if !(strings.Index(text, "First") < strings.Index(text, "Second") && strings.Index(text, "Second") < strings.Index(text, "Third")) {
			t.Fatalf("reordered plan: %s", text)
		}
		if !strings.Contains(text, "Removes all selected data") || !strings.Contains(text, "data-loss") {
			t.Fatalf("lost disclosure: %s", text)
		}
		if strings.Contains(text, "[after 1]") != (detail == "full") {
			t.Fatalf("dependency detail for %s: %s", detail, text)
		}
	}
}

func TestLifecycleNarrowPlanKeepsAuthorizationAndImpact(t *testing.T) {
	for _, width := range []int{24, 48} {
		var out bytes.Buffer
		opts := PresentationOptions{Width: width, Color: "always", Detail: "compact"}
		result := lifecycle.PlanResult{Verb: "destroy", Steps: []lifecycle.PlanStep{{ID: "remove", Description: "Remove selected machine disks", Object: "example-machine", Stage: "machines", Consumes: []string{"data-loss"}, Impacts: []string{"Permanently removes every selected data volume"}}}}
		if err := NewLifecyclePlanPresenter(WithPresentation(&out, opts)).PresentLifecyclePlan(context.Background(), result); err != nil {
			t.Fatal(err)
		}
		// Strip only renderer-owned styling for the content assertion; escaped
		// caller input never becomes an ANSI control sequence.
		plain := ansi.Strip(out.String())
		words := strings.Join(strings.Fields(plain), " ")
		for _, want := range []string{"data-loss", "--authorize data-loss", "Permanently removes every selected data volume"} {
			if !strings.Contains(words, want) {
				t.Fatalf("width %d omits %q: %q", width, want, words)
			}
		}
		for _, row := range strings.Split(out.String(), "\n") {
			if displayCellWidth(row) > width {
				t.Fatalf("width %d overflow: %q", width, row)
			}
		}
	}
}

func TestLifecycleProgressNeverRetainsLogReference(t *testing.T) {
	var out bytes.Buffer
	_, reporter := NewLifecyclePresenters(WithPresentation(&out, PresentationOptions{Progress: "never"}), func() int { return 80 }, false)
	reporter.ReportProgress(context.Background(), lifecycle.ProgressEvent{Block: "one", Description: "Work", Status: "running"})
	reporter.ReportLogLocation(context.Background(), "operation/logs")
	reporter.Finish()
	if strings.Contains(out.String(), "RUNNING") || !strings.Contains(out.String(), "operation/logs") {
		t.Fatalf("suppressed progress = %q", out.String())
	}
}

func TestColoredProgressFitsResizedTerminal(t *testing.T) {
	var out bytes.Buffer
	width := 48
	clock := newManualClock()
	presenter := &progressPresenter{out: WithPresentation(&out, PresentationOptions{Color: "always"}), clock: clock.clock(), columns: func() int { return width }}
	step := &progressStep{label: "Observe the declared machine readiness and prove completion", target: "example-machine", position: 1, total: 2}
	for _, columns := range []int{48, 24, 80} {
		width = columns
		row := presenter.row("running", step, "waiting for evidence", "still running, 1m")
		if displayCellWidth(row) >= width {
			t.Fatalf("width %d overflow: %q", width, row)
		}
		if !strings.Contains(row, "\x1b[") {
			t.Fatalf("color not rendered: %q", row)
		}
		if strings.Contains(ansi.Strip(row), "\x1b") {
			t.Fatalf("truncated escape: %q", row)
		}
	}
}

func TestProgressWidthCapsPhysicalTerminalAndTracksResize(t *testing.T) {
	for _, mode := range []string{"auto", "always"} {
		var out bytes.Buffer
		physical := 120
		writer := WithPresentation(&out, PresentationOptions{Color: "always", Width: 48, Progress: mode})
		presenter := NewLifecycleProgressPresenter(writer, func() int { return physical })
		step := &progressStep{label: strings.Repeat("Long task ", 20)}
		for _, width := range []int{120, 32, 90} {
			physical = width
			row := presenter.progress.row("running", step, "waiting", "")
			if got := displayCellWidth(row); got >= min(width, 48) {
				t.Fatalf("mode %s physical %d got %d columns: %q", mode, width, got, row)
			}
		}
	}
	auto := selectedProgressColumns(WithPresentation(io.Discard, PresentationOptions{Width: 48, Progress: "auto"}), func() int { return 0 })
	if auto() != 0 {
		t.Fatal("explicit width turned a pipe into a terminal")
	}
}

func TestLifecycleLiveDependencyOverviewIsBounded(t *testing.T) {
	plan := lifecycle.PlanResult{Steps: []lifecycle.PlanStep{{ID: "first", State: "pending"}}}
	for i := range 100 {
		plan.Steps = append(plan.Steps, lifecycle.PlanStep{ID: fmt.Sprint(i), Description: "Dependent block", State: "pending", After: []int{1}})
	}
	model := &lifecyclePresentation{}
	model.seed(plan)
	rows := model.rows(PresentationOptions{Detail: "full"})
	if len(rows) != 5 || !strings.Contains(rows[4], "97 more waiting on dependencies") {
		t.Fatalf("unbounded dependency view: %q", rows)
	}
	model.report(lifecycle.ProgressEvent{Block: "first", Status: "done"})
	if rows := model.rows(PresentationOptions{Detail: "full"}); len(rows) != 1 {
		t.Fatalf("completed dependencies still waiting: %q", rows)
	}
}
