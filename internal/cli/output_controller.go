package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// ControllerPlanPresenter writes the complete setup plan before confirmation.
// Construction performs no output; write failure prevents setup mutation.
type ControllerPlanPresenter struct{ out io.Writer }

func NewControllerPlanPresenter(out io.Writer) *ControllerPlanPresenter {
	return &ControllerPlanPresenter{out: out}
}

func (p *ControllerPlanPresenter) PresentControllerPlan(ctx context.Context, report prerequisites.Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.out == nil || !validControllerReport(&report) {
		return &controllerOutputFailure{}
	}
	var text display
	text.headline("", controllerHeadline("bastion setup", &report))
	controllerPlanText(&text, &report)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := text.writeTo(p.out); err != nil {
		return &controllerOutputFailure{}
	}
	return nil
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
		return "Bastion setup"
	}
	return "Bastion setup plan"
}

func writeControllerReport(out io.Writer, command string, report *prerequisites.Report) error {
	var text display
	if !report.PlanPresented {
		text.headline("", controllerHeadline(command, report))
		controllerPlanText(&text, report)
	}
	// After the plan was presented, an unfinished setup must still say which of
	// its approved actions took effect.
	if report.PlanPresented && report.Outcome != "changed" && report.Outcome != "unchanged" && len(report.Progress) != 0 {
		text.section("Progress")
		rows := make([][]string, 0, len(report.Progress))
		for _, action := range report.Progress {
			rows = append(rows, []string{progressToken(action), action.ID})
		}
		text.rows(rows)
	}
	text.section("")
	text.fields(controllerOutcomeFields(command, report)...)
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

// ControllerProgressPresenter writes one line per setup step while setup runs,
// so a long acquisition is visibly working rather than silent. It writes to the
// same stream as the plan and never buffers, because an unflushed line during a
// ten-minute transfer would defeat its only purpose.
type ControllerProgressPresenter struct {
	out     io.Writer
	started bool
}

func NewControllerProgressPresenter(out io.Writer) *ControllerProgressPresenter {
	return &ControllerProgressPresenter{out: out}
}

func (p *ControllerProgressPresenter) ReportProgress(ctx context.Context, event prerequisites.ProgressEvent) {
	if p == nil || p.out == nil || ctx.Err() != nil || event.Action == "" {
		return
	}
	if !p.started {
		p.started = true
		io.WriteString(p.out, "\nProgress\n")
	}
	subject := event.Action
	if event.Detail != "" {
		subject = event.Action + ": " + event.Detail
	}
	if event.Steps > 0 && event.Step > 0 {
		subject += fmt.Sprintf(" (%d/%d)", event.Step, event.Steps)
	}
	io.WriteString(p.out, "  "+progressStatusToken(event.Status)+" "+escapeDisplayLine(subject)+"\n")
}

// Progress reuses the output contract's status tokens: work in flight has no
// terminal outcome, and a finished step reports the outcome it proved.
func progressStatusToken(status string) string {
	switch status {
	case "running":
		return "[RUNNING]"
	case "changed":
		return "[DONE]"
	case "unchanged":
		return "[OK]"
	case "failed":
		return "[FAIL]"
	default:
		return "[UNKNOWN]"
	}
}
