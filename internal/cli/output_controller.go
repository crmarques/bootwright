package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const controllerSetupHeadline = "Bastion setup"

// ControllerPresenter owns everything bastion setup shows while it works: the
// resolution steps before the plan, the plan itself and the approved actions
// after confirmation. One object presents all three so the headline is written
// once and every row shares one layout. Construction performs no output; a
// plan write failure prevents setup mutation.
type ControllerPresenter struct {
	progress progressPresenter
	headline bool
}

func NewControllerPresenter(out io.Writer) *ControllerPresenter {
	return &ControllerPresenter{progress: progressPresenter{out: out, clock: systemProgressClock()}}
}

func (p *ControllerPresenter) PresentControllerPlan(ctx context.Context, report prerequisites.Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.progress.out == nil || !validControllerReport(&report) {
		return &controllerOutputFailure{}
	}
	var text display
	if !p.headline {
		text.headline("", controllerHeadline("bastion setup", &report))
	}
	controllerPlanText(&text, &report)
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.headline {
		// The plan follows the resolution rows as its own block.
		if _, err := io.WriteString(p.progress.out, "\n"); err != nil {
			return &controllerOutputFailure{}
		}
	}
	if err := text.writeTo(p.progress.out); err != nil {
		return &controllerOutputFailure{}
	}
	p.headline = true
	return nil
}

// ReportProgress streams one setup row. Resolution rows precede the plan, so
// the first one also opens the headline the plan would otherwise write.
func (p *ControllerPresenter) ReportProgress(ctx context.Context, event prerequisites.ProgressEvent) {
	if p == nil || p.progress.out == nil || ctx.Err() != nil || event.Action == "" {
		return
	}
	heading, nested := "Progress", event.Detail != ""
	if event.Phase == prerequisites.ResolutionPhase {
		heading, nested = "Resolving", false
		if !p.headline {
			p.headline = true
			io.WriteString(p.progress.out, controllerSetupHeadline+"\n")
		}
	}
	p.progress.report(ctx, progressEvent{
		Heading: heading, Label: controllerActionLabel(event.Action), Detail: event.Detail,
		Status: event.Status, Position: event.Step, Total: event.Steps, Nested: nested,
	})
}

type controllerOutputFailure struct{}

func (*controllerOutputFailure) Error() string { return "controller plan output failed" }

func validControllerReport(report *prerequisites.Report) bool {
	if report == nil || report.Platform.OS == "" || report.Platform.Release == "" || report.Platform.Architecture == "" || len(report.Checks) == 0 || (report.ContextName == "") != (report.Machine == "") {
		return false
	}
	switch report.Outcome {
	case "planned", "ready", "not-ready", "unchanged", "changed", "incomplete":
	default:
		return false
	}
	for _, check := range report.Checks {
		if check.ID == "" || check.Required == "" || check.Observed == "" {
			return false
		}
		if check.Status != "ready" && check.Status != "not-ready" && check.Status != "unverified" {
			return false
		}
		if (report.Outcome == "ready" || report.Outcome == "changed" || report.Outcome == "unchanged") && check.Status != "ready" {
			return false
		}
	}
	return !report.DryRun || report.Outcome == "planned"
}

func successfulControllerReport(command string, report *prerequisites.Report) bool {
	if command == "preflight bastion" {
		return report.Outcome == "ready" && !report.DryRun
	}
	return report.DryRun && report.Outcome == "planned" || !report.DryRun && (report.Outcome == "unchanged" || report.Outcome == "changed")
}

// controllerHeadline names what the reader is looking at. Readiness inspection
// never presents a plan, so only setup announces one.
func controllerHeadline(command string, report *prerequisites.Report) string {
	if command == "preflight bastion" {
		return "Bastion readiness"
	}
	if report.PlanPresented || !report.DryRun {
		return controllerSetupHeadline
	}
	return "Bastion setup plan"
}

// controllerActionLabel turns a receipt action identity into the prose the
// operator reads; resolution steps already arrive as prose.
func controllerActionLabel(id string) string {
	switch id {
	case "execution-bundle":
		return "Execution bundle"
	case "container-runtime":
		return "Container runtime"
	case "controller-binding":
		return "Controller binding"
	}
	return id
}

func writeControllerReport(out io.Writer, command string, report *prerequisites.Report) error {
	var text display
	presented := report.PlanPresented || report.ProgressPresented
	if !presented {
		text.headline("", controllerHeadline(command, report))
		controllerPlanText(&text, report)
	}
	// After the plan was presented, an unfinished setup must still say which of
	// its approved actions took effect.
	if report.PlanPresented && report.Outcome != "changed" && report.Outcome != "unchanged" && len(report.Progress) != 0 {
		text.section("Progress")
		rows := make([][]string, 0, len(report.Progress))
		for _, action := range report.Progress {
			rows = append(rows, []string{progressToken(action), controllerActionLabel(action.ID)})
		}
		text.rows(rows)
	}
	text.section("")
	text.fields(controllerOutcomeFields(command, report)...)
	if presented {
		// Streamed rows precede this block, so it separates itself from them.
		if _, err := io.WriteString(out, "\n"); err != nil {
			return err
		}
	}
	return text.writeTo(out)
}

func controllerOutcomeFields(command string, report *prerequisites.Report) []field {
	outcome := report.Outcome
	if report.DryRun {
		outcome += " (dry run)"
	}
	fields := []field{{Label: "Outcome", Value: outcome}}
	if report.PlanPresented && (report.Outcome == "changed" || report.Outcome == "unchanged") {
		fields = append(fields, field{Label: "Readiness", Value: "all required prerequisites verified"})
	}
	if report.Outcome != "ready" {
		next := "bootwright bastion setup"
		if report.Outcome == "changed" || report.Outcome == "unchanged" {
			next = "bootwright preflight bastion"
		}
		if report.ContextName != "" {
			next += " --context " + report.ContextName
		}
		fields = append(fields, field{Label: "Next", Value: next})
	}
	return fields
}

// Progress uses the same status vocabulary: an observed action reports its
// definite outcome, an intended one is unproved, and an untouched one did no
// work at all.
func progressToken(action prerequisites.ActionProgress) string {
	if action.Phase != "observed" {
		if action.Phase == "planned" {
			return "[SKIPPED]"
		}
		return "[UNKNOWN]"
	}
	switch action.Outcome {
	case "changed":
		return "[DONE]"
	case "unchanged":
		return "[OK]"
	case "failed":
		return "[FAIL]"
	case "canceled":
		return "[CANCELED]"
	default:
		return "[UNKNOWN]"
	}
}

// Readiness is reported with the output contract's status tokens: a check that
// holds is successful work, one that does not is a definite failure, and one the
// command is not permitted to observe cannot be proved either way.
func checkToken(status string) string {
	switch status {
	case "ready":
		return "[OK]"
	case "not-ready":
		return "[FAIL]"
	default:
		return "[UNKNOWN]"
	}
}

// A satisfied check reports only what was observed. Any other check states the
// requirement beside it, because the difference is the actionable part.
func checkDetail(check prerequisites.Check) string {
	if check.Status == "ready" {
		return check.Observed
	}
	return "required " + check.Required + "; observed " + check.Observed
}

func controllerPlanText(text *display, report *prerequisites.Report) {
	scope := []field{{Label: "Scope", Value: "baseline"}}
	if report.ContextName != "" {
		scope = []field{
			{Label: "Scope", Value: "context " + report.ContextName},
			{Label: "Controller", Value: report.Machine},
		}
	}
	scope = append(scope, field{Label: "Platform", Value: report.Platform.OS + " " + report.Platform.Release + "/" + report.Platform.Architecture})
	text.section("")
	text.fields(scope...)

	text.section("Checks")
	rows := make([][]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		rows = append(rows, []string{checkToken(check.Status), check.ID, checkDetail(check)})
	}
	text.rows(rows)

	if len(report.Actions) == 0 {
		text.section("Planned changes")
		text.lines([]string{"none"})
		return
	}
	if len(report.Dependencies) != 0 {
		text.section("Dependencies")
		text.lines(report.Dependencies)
	}
	text.section("Planned changes")
	text.steps(report.Actions)
}
