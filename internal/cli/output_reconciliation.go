package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		items = append(items, step{Text: line, Effects: planned.Impacts})
	}
	text.steps(items)
	if len(result.Stages) != 0 {
		text.section("")
		text.fields(field{Label: "Stages", Value: strings.Join(result.Stages, ", ")},
			field{Label: "Starts", Value: startSummary(result)})
	}
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
		text.lines([]string{settledNote(result.Verb)})
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

// settledNote says why an operation performed nothing. Without it a result
// listing only completed blocks reads as though this invocation did that work.
func settledNote(verb string) string {
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
	// A removal is planned from what the registering build froze, so the
	// operator reads which build that was before meeting a refusal that names
	// it as the remedy.
	if result.Lifecycle != nil && result.Lifecycle.Executable != "" {
		tail = append(tail, field{Label: "Registered by", Value: escapeDisplayLine(result.Lifecycle.Executable)})
	}
	if result.LogLocation != "" {
		tail = append(tail, field{Label: logLocationLabel, Value: escapeDisplayLine(result.LogLocation)})
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
