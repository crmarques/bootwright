package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

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
		Heading: "Progress", Label: label, Detail: detail, Status: event.Status,
		Position: event.Position, Total: event.Total, Nested: event.Group != "",
		Completed: event.Completed, Declared: event.Declared,
	})
}

func lifecycleHeadline(result lifecycle.PlanResult) string {
	verb := strings.ToUpper(result.Verb[:1]) + result.Verb[1:]
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
	items := make([]string, 0, len(result.Steps))
	for _, step := range result.Steps {
		line := escapeDisplayLine(step.Description)
		if step.Stage != "" {
			line += " [" + escapeDisplayLine(step.Stage) + "]"
		}
		if result.Continuation {
			line += " [" + escapeDisplayLine(step.State) + "]"
		}
		if marker := selectionMarker(step); marker != "" {
			line += " [" + marker + "]"
		}
		items = append(items, line)
	}
	text.steps(items)
	if len(result.Stages) != 0 {
		text.section("")
		text.fields(field{Label: "Stages", Value: escapeDisplayLine(strings.Join(result.Stages, ", "))},
			field{Label: "Starts", Value: startSummary(result)})
	}
	impacts := []string{}
	for _, step := range result.Steps {
		for _, impact := range step.Impacts {
			impacts = append(impacts, escapeDisplayLine(impact))
		}
	}
	if len(impacts) != 0 {
		text.section("Impacts")
		text.lines(impacts)
	}
}

// selectionMarker says what a stage selection would do with one pending step,
// so the operator sees the consequence of the selection before confirming it.
func selectionMarker(step lifecycle.PlanStep) string {
	switch step.Selection {
	case lifecycle.StepStart:
		return "start"
	case lifecycle.StepNotSelected:
		return "not selected"
	case lifecycle.StepWaiting:
		return "deferred: waits on " + escapeDisplayLine(step.WaitsOn)
	}
	return ""
}

func startSummary(result lifecycle.PlanResult) string {
	return fmt.Sprintf("%d of %d blocks, %d deferred", result.Startable, len(result.Steps), result.Deferred)
}

func writeLifecyclePlan(out io.Writer, result *lifecycle.PlanResult) error {
	var text display
	text.headline("", lifecycleHeadline(*result))
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
	if len(result.Blocks) != 0 {
		text.section("Result")
		rows := make([][]string, 0, len(result.Blocks))
		for _, block := range result.Blocks {
			rows = append(rows, []string{blockStatusToken(block.State), escapeDisplayLine(block.Description)})
		}
		text.rows(rows)
	}
	if len(result.Logs) != 0 {
		text.section("")
		text.fields(field{Label: "Details", Value: escapeDisplayLine(result.Logs[0])})
	}
	if err := text.writeTo(out); err != nil {
		return err
	}
	return writeReceipt(out, result.Receipt)
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

func writeLifecycleStatus(out io.Writer, result *lifecycle.StatusResult, jsonMode bool) error {
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{
			SchemaVersion: "v1alpha1", Command: "status", OK: true, ExitCode: 0,
			Result: lifecycleStatusJSON(result), Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	var text display
	text.headline("OK", "Context "+escapeDisplayLine(result.Context.Name))
	if len(result.SetupChecks) != 0 {
		text.section("Setup")
		rows := make([][]string, 0, len(result.SetupChecks))
		for _, check := range result.SetupChecks {
			rows = append(rows, []string{setupStatusToken(check.Status), escapeDisplayLine(check.ID)})
		}
		text.rows(rows)
	}
	if len(result.Shared) != 0 {
		text.section("Shared services")
		rows := make([][]string, 0, len(result.Shared))
		for _, service := range result.Shared {
			rows = append(rows, []string{setupStatusToken(service.Status), escapeDisplayLine(service.Kind + "/" + service.Name)})
		}
		text.rows(rows)
	}
	if result.Lifecycle != nil {
		text.section("Lifecycle")
		rows := make([][]string, 0, len(result.Lifecycle.Blocks))
		for _, block := range result.Lifecycle.Blocks {
			rows = append(rows, []string{blockStatusToken(block.State), escapeDisplayLine(block.Description)})
		}
		text.rows(rows)
	}
	var tail []field
	if result.Lifecycle != nil && len(result.Lifecycle.Logs) != 0 {
		tail = append(tail, field{Label: "Details", Value: escapeDisplayLine(result.Lifecycle.Logs[0])})
	}
	if len(result.NextSteps) != 0 {
		tail = append(tail, field{Label: "Next", Value: escapeDisplayLine(result.NextSteps[0])})
	}
	if len(tail) != 0 {
		text.section("")
		text.fields(tail...)
	}
	return text.writeTo(out)
}

func setupStatusToken(status string) string {
	switch status {
	case "ready", "done":
		return "[OK]"
	case "pending", "incomplete":
		return "[PENDING]"
	case "unknown":
		return "[UNKNOWN]"
	case "unsupported":
		return "[SKIPPED]"
	}
	return "[FAIL]"
}

type statusEnvelope struct {
	Context         lifecycle.ContextIdentity   `json:"context"`
	SetupChecks     []lifecycle.SetupCheck      `json:"setupChecks"`
	Desired         lifecycle.DesiredSummary    `json:"desired"`
	Clusters        []lifecycle.ClusterSummary  `json:"clusters"`
	StorageClusters []lifecycle.ClusterSummary  `json:"storageClusters"`
	Shared          []lifecycle.ServiceSummary  `json:"shared"`
	Secrets         lifecycle.SecretSummary     `json:"secrets"`
	NextSteps       []string                    `json:"nextSteps"`
	Lifecycle       *lifecycle.LifecycleSummary `json:"lifecycle"`
}

func lifecycleStatusJSON(result *lifecycle.StatusResult) statusEnvelope {
	return statusEnvelope{
		Context: result.Context, SetupChecks: result.SetupChecks, Desired: result.Desired,
		Clusters: result.Clusters, StorageClusters: result.StorageClusters, Shared: result.Shared,
		Secrets: result.Secrets, NextSteps: result.NextSteps, Lifecycle: result.Lifecycle,
	}
}
