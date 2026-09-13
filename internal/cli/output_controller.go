package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const controllerSetupHeadline = "Bastion setup"

// ControllerPresenter owns everything bastion setup and readiness show while
// they work: the scope, the host checks as they are verified, the resolution
// steps, the plan and the approved actions after confirmation. One object
// presents all of them so the headline and each section appear exactly once.
// Construction performs no output; a scope or plan write failure prevents
// setup mutation.
type ControllerPresenter struct {
	progress progressPresenter
	headline bool
	scope    bool
	checks   bool
}

// NewControllerPresenter streams to out. On a terminal the running row is
// rewritten in place; anywhere else rows are appended.
func NewControllerPresenter(out io.Writer, terminal bool) *ControllerPresenter {
	return &ControllerPresenter{progress: progressPresenter{out: out, clock: systemProgressClock(), terminal: terminal}}
}

// PresentControllerScope opens the result before inspection starts streaming
// its checks, so the operator knows what is being inspected while it runs.
func (p *ControllerPresenter) PresentControllerScope(ctx context.Context, phase string, report prerequisites.Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.progress.out == nil || !validControllerScope(&report) {
		return &controllerOutputFailure{}
	}
	if p.headline {
		return nil
	}
	var text display
	text.headline("", phaseHeadline(phase))
	controllerScopeText(&text, &report)
	if err := text.writeTo(p.progress.out); err != nil {
		return &controllerOutputFailure{}
	}
	p.headline, p.scope = true, true
	return nil
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
	controllerPlanText(&text, &report, !p.scope, !p.checks)
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.headline {
		// The plan follows the streamed rows as its own block.
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

// ReportProgress streams one row under the heading its phase owns. A phase
// that arrives before the scope also opens the headline.
func (p *ControllerPresenter) ReportProgress(ctx context.Context, event prerequisites.ProgressEvent) {
	if p == nil || p.progress.out == nil || ctx.Err() != nil || event.Action == "" {
		return
	}
	heading, nested := "Progress", event.Detail != ""
	switch event.Phase {
	case prerequisites.InspectionPhase, prerequisites.ReadinessPhase:
		heading, nested = "Checks", false
		p.checks = true
	case prerequisites.ResolutionPhase:
		heading, nested = "Resolving", false
	}
	if !p.headline {
		p.headline = true
		io.WriteString(p.progress.out, phaseHeadline(event.Phase)+"\n")
	}
	p.progress.report(ctx, progressEvent{
		Heading: heading, Label: controllerActionLabel(event.Action), Detail: event.Detail,
		Status: event.Status, Position: event.Step, Total: event.Steps, Nested: nested,
	})
}

// Finish terminates a row a terminal is still rewriting. The runner calls it
// once the operation returns, before any result or diagnostic is written.
func (p *ControllerPresenter) Finish() {
	if p != nil {
		p.progress.finish()
	}
}

type controllerOutputFailure struct{}

func (*controllerOutputFailure) Error() string { return "controller plan output failed" }

func validControllerScope(report *prerequisites.Report) bool {
	return report != nil && report.Platform.OS != "" && report.Platform.Release != "" && report.Platform.Architecture != "" && (report.ContextName == "") == (report.Machine == "")
}

func validControllerReport(report *prerequisites.Report) bool {
	if !validControllerScope(report) || len(report.Checks) == 0 {
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

func phaseHeadline(phase string) string {
	if phase == prerequisites.ReadinessPhase {
		return "Bastion readiness"
	}
	return controllerSetupHeadline
}

// controllerActionLabel turns a check or receipt action identity into the
// prose the operator reads; resolution steps already arrive as prose.
func controllerActionLabel(id string) string {
	switch id {
	case "host":
		return "Host"
	case "installed-host":
		return "Installed host"
	case "execution-bundle":
		return "Execution bundle"
	case "container-runtime":
		return "Container runtime"
	case "target-tools":
		return "Target tools"
	case "controller-binding":
		return "Controller binding"
	case "setup-recovery":
		return "Setup recovery"
	case "setup-state":
		return "Setup state"
	}
	return id
}

// writeControllerReport closes the result. Whatever the presenter already
// streamed is not repeated: the scope, checks and plan after a streamed
// inspection, or everything but the outcome after a presented plan.
func writeControllerReport(out io.Writer, command string, report *prerequisites.Report) error {
	var text display
	presented := report.PlanPresented || report.ProgressPresented
	if !presented {
		text.headline("", controllerHeadline(command, report))
	}
	// A setup that failed before its plan settled has nothing to plan; every
	// other unpresented plan still names what would change.
	if !report.PlanPresented && !(report.ProgressPresented && report.Outcome == "planned") {
		controllerPlanText(&text, report, !presented, !presented)
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

func controllerScopeText(text *display, report *prerequisites.Report) {
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
}

// controllerPlanText writes the plan body. The scope and checks are skipped
// when the presenter already streamed them.
func controllerPlanText(text *display, report *prerequisites.Report, scope, checks bool) {
	if scope {
		controllerScopeText(text, report)
	}
	if checks {
		text.section("Checks")
		rows := make([][]string, 0, len(report.Checks))
		for _, check := range report.Checks {
			rows = append(rows, []string{checkToken(check.Status), controllerActionLabel(check.ID), check.Summary()})
		}
		text.rows(rows)
	}
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
