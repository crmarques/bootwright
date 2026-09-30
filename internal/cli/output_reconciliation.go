package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// logLocationLabel names the operation's own log directory. The same label is
// used while the work runs and after it settles, because it is the same place.
const logLocationLabel = "Logs"

// LifecyclePlanPresenter writes the frozen plan before confirmation, so an
// operator sees exactly what an operation would do before authorizing it.
type LifecyclePlanPresenter struct{ out io.Writer }

func NewLifecyclePlanPresenter(out io.Writer) *LifecyclePlanPresenter {
	return &LifecyclePlanPresenter{out: out}
}

func (p *LifecyclePlanPresenter) PresentLifecyclePlan(ctx context.Context, result lifecycle.PlanResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.out == nil {
		return &lifecycleOutputFailure{}
	}
	var text display
	text.headline("", lifecycleHeadline(result))
	writeLifecycleSteps(&text, result)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := text.writeTo(p.out); err != nil {
		return &lifecycleOutputFailure{}
	}
	return nil
}

type lifecycleOutputFailure struct{}

func (*lifecycleOutputFailure) Error() string { return "lifecycle plan output failed" }

// LifecycleProgressPresenter streams each block and presentation group while
// the operation runs, because a silent process is indistinguishable from a
// stuck one. A group is a sub-step of its block.
type LifecycleProgressPresenter struct{ progress progressPresenter }

// NewLifecycleProgressPresenter streams to out. Given a terminal width reader
// the running row is rewritten in place within it; anywhere else rows are
// appended.
func NewLifecycleProgressPresenter(out io.Writer, columns func() int) *LifecycleProgressPresenter {
	return &LifecycleProgressPresenter{progress: progressPresenter{out: out, clock: systemProgressClock(), columns: columns}}
}

// Finish terminates a row a terminal is still rewriting. The runner calls it
// once the operation returns, before any result or diagnostic is written.
func (p *LifecycleProgressPresenter) Finish() {
	if p != nil {
		p.progress.finish()
	}
}

func (p *LifecycleProgressPresenter) ReportProgress(ctx context.Context, event lifecycle.ProgressEvent) {
	if p == nil || p.progress.out == nil || ctx.Err() != nil || event.Block == "" {
		return
	}
	label, detail := event.Description, event.Detail
	if label == "" {
		label = event.Block
	}
	if detail == "" {
		detail = event.Group
	}
	p.progress.report(ctx, progressEvent{
		Heading: lifecyclePhaseHeading(event.Phase), Label: label, Detail: detail, Status: event.Status,
		Position: event.Position, Total: event.Total, Nested: event.Group != "",
		Completed: event.Completed, Declared: event.Declared,
	})
}

// lifecyclePhaseHeading separates what an operation proves before it registers
// from what it then does. A check settles under its own heading while no
// operation and no log exist, so Progress opens on the first effect and holds
// effects alone: one row per block, all of them after the Logs field that says
// where to follow them.
func lifecyclePhaseHeading(phase string) string {
	if phase == lifecycle.CheckPhase {
		return "Checks"
	}
	return "Progress"
}

// ReportLogLocation names where the operation writes, before its first effect.
// It is a field of its own so it survives the progress rows a terminal redraws
// over each other, and an operator can follow the work as it happens.
func (p *LifecycleProgressPresenter) ReportLogLocation(ctx context.Context, location string) {
	if p == nil || p.progress.out == nil || ctx.Err() != nil || location == "" {
		return
	}
	p.progress.field(logLocationLabel, location)
}

func lifecycleHeadline(result lifecycle.PlanResult) string {
	verb := strings.ToUpper(result.Verb[:1]) + result.Verb[1:]
	if result.Finalizes {
		return verb + " finalization"
	}
	if result.Continuation {
		return verb + " continuation plan"
	}
	return verb + " plan"
}

func writeLifecycleSteps(text *display, result lifecycle.PlanResult) {
	if len(result.Steps) == 0 {
		text.section("Plan")
		text.lines([]string{"no blocks"})
		return
	}
	text.section("Plan")
	items := make([]step, 0, len(result.Steps))
	for _, planned := range result.Steps {
		line := planned.Description
		if planned.Stage != "" {
			line += " [" + planned.Stage + "]"
		}
		if result.Continuation {
			line += " [" + planned.State + "]"
		}
		if marker := selectionMarker(planned); marker != "" {
			line += " [" + marker + "]"
		}
		if waiting := waitMarker(planned); waiting != "" {
			line += " " + waiting
		}
		items = append(items, step{Text: line, Effects: planned.Impacts})
	}
	text.steps(items)
	var closing []field
	if len(result.Stages) != 0 {
		closing = append(closing, field{Label: "Stages", Value: strings.Join(result.Stages, ", ")},
			field{Label: "Starts", Value: startSummary(result)})
	}
	if summary := concurrencySummary(result); summary != "" {
		closing = append(closing, field{Label: "Concurrency", Value: summary})
	}
	if len(closing) != 0 {
		text.section("")
		text.fields(closing...)
	}
}

// waitMarker names the steps one step waits for, by their place in the plan, so
// the list itself says what orders the work. A step that waits for nothing
// carries no marker, which is what marks it as one of the first to start.
func waitMarker(planned lifecycle.PlanStep) string {
	if len(planned.After) == 0 {
		return ""
	}
	places := make([]string, 0, len(planned.After))
	for _, position := range planned.After {
		places = append(places, strconv.Itoa(position))
	}
	return "[after " + strings.Join(places, ", ") + "]"
}

// concurrencySummary says how much of the plan its own shape lets run at once.
// A plan whose steps are one long chain says so, which is the difference
// between a long queue and a wide one.
func concurrencySummary(result lifecycle.PlanResult) string {
	if result.Waves == 0 || len(result.Steps) == 0 {
		return ""
	}
	steps := "steps"
	if result.Widest == 1 {
		steps = "step"
	}
	waves := "waves"
	if result.Waves == 1 {
		waves = "wave"
	}
	return fmt.Sprintf("%d %s, up to %d %s at once", result.Waves, waves, result.Widest, steps)
}

// selectionMarker says what a stage selection would do with one pending step,
// so the operator sees the consequence of the selection before confirming it.
func selectionMarker(planned lifecycle.PlanStep) string {
	switch planned.Selection {
	case lifecycle.StepStart:
		return "start"
	case lifecycle.StepNotSelected:
		return "not selected"
	case lifecycle.StepWaiting:
		return "deferred: waits on " + planned.WaitsOn
	}
	return ""
}

func startSummary(result lifecycle.PlanResult) string {
	return fmt.Sprintf("%d of %d blocks, %d deferred", result.Startable, len(result.Steps), result.Deferred)
}

func writeLifecyclePlan(out io.Writer, result *lifecycle.PlanResult) error {
	var text display
	text.headline("", lifecycleHeadline(*result))
	if result.Finalizes {
		text.section("")
		text.lines([]string{"Nothing to run: every block is done, so bootwright " + result.Verb + " only completes this operation's interrupted finalization."})
	}
	writeLifecycleSteps(&text, *result)
	if err := text.writeTo(out); err != nil {
		return err
	}
	return writeReceipt(out, result.Receipt)
}

func writeLifecycleOperation(out io.Writer, result *lifecycle.OperationResult) error {
	var text display
	status := "OK"
	if result.Receipt.State != "done" && result.Receipt.State != "paused" {
		status = "FAIL"
	}
	if result.Receipt.State == "unknown" {
		status = "UNKNOWN"
	}
	verb := strings.ToUpper(result.Verb[:1]) + result.Verb[1:]
	text.headline(status, verb+" "+escapeDisplayLine(result.Receipt.State))
	if result.Settled {
		text.section("")
		text.lines([]string{settledNote(result.Verb, result.Recovered)})
	}
	if len(result.Blocks) != 0 {
		text.section("Result")
		rows := make([][]string, 0, len(result.Blocks))
		for _, block := range result.Blocks {
			rows = append(rows, []string{blockStatusToken(block.State), escapeDisplayLine(block.Description)})
		}
		text.rows(rows)
	}
	if result.LogLocation != "" {
		text.section("")
		text.fields(field{Label: logLocationLabel, Value: escapeDisplayLine(result.LogLocation)})
	}
	if err := text.writeTo(out); err != nil {
		return err
	}
	return writeReceipt(out, result.Receipt)
}

// settledNote says why an operation performed nothing, and names what it
// completed first when that was all it did. Without it a result listing only
// completed blocks reads as though this invocation did that work.
func settledNote(verb, recovered string) string {
	switch recovered {
	case lifecycle.RecoveredFinalization:
		if verb == "destroy" {
			return "Nothing to remove: this invocation only completed an interrupted finalization."
		}
		return "Nothing to do: this invocation only completed an interrupted finalization."
	case lifecycle.RecoveredRelease:
		return "Nothing to remove: this invocation only released what an interrupted registration left."
	}
	if verb == "destroy" {
		return "Nothing to remove: this context owns no realized state."
	}
	return "Nothing to do: this context already holds the state it declares."
}

// writeReceipt emits the stable machine-readable tail. Its labels, order and
// final newline are a public contract.
func writeReceipt(out io.Writer, receipt lifecycle.Receipt) error {
	_, err := fmt.Fprintf(out, "operation: %s\nverb: %s\nstate: %s\nnext: %s\n",
		escapeDisplayLine(receipt.Operation), escapeDisplayLine(receipt.Verb),
		escapeDisplayLine(receipt.State), escapeDisplayLine(receipt.Next))
	return err
}

func blockStatusToken(state string) string {
	switch state {
	case "done":
		return "[DONE]"
	case "pending":
		return "[PENDING]"
	case "running":
		return "[RUNNING]"
	case "unknown":
		return "[UNKNOWN]"
	}
	return "[FAIL]"
}
