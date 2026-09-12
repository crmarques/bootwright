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

// LifecycleProgressPresenter reports each block and group while the operation
// runs, because a silent process is indistinguishable from a stuck one.
type LifecycleProgressPresenter struct{ out io.Writer }

func NewLifecycleProgressPresenter(out io.Writer) *LifecycleProgressPresenter {
	return &LifecycleProgressPresenter{out: out}
}

func (p *LifecycleProgressPresenter) ReportProgress(ctx context.Context, event lifecycle.ProgressEvent) {
	if p == nil || p.out == nil || ctx.Err() != nil {
		return
	}
	label := escapeDisplayLine(event.Block)
	if event.Group != "" {
		label += " " + escapeDisplayLine(event.Group)
	}
	position := ""
	if event.Total > 0 && event.Position > 0 {
		position = fmt.Sprintf(" (%d/%d)", event.Position, event.Total)
	}
	fmt.Fprintf(p.out, "  %s %s%s\n", progressStatusToken(event.Status), label, position)
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
		if result.Continuation {
			line += " [" + escapeDisplayLine(step.State) + "]"
		}
		items = append(items, line)
	}
	text.steps(items)
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
	if result.Receipt.State != "done" {
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
	if len(result.NextSteps) != 0 {
		text.section("")
		text.fields(field{Label: "Next", Value: escapeDisplayLine(result.NextSteps[0])})
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
