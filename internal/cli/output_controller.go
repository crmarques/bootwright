package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

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
	var text strings.Builder
	text.WriteString("bastion setup plan\n")
	controllerPlanText(&text, &report)
	if err := ctx.Err(); err != nil {
		return err
	}
	if n, err := io.WriteString(p.out, text.String()); err != nil || n != text.Len() {
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

func writeControllerReport(out io.Writer, command string, report *prerequisites.Report) error {
	var text strings.Builder
	if !report.PlanPresented {
		controllerPlanText(&text, report)
	}
	fmt.Fprintf(&text, "outcome: %s\n", report.Outcome)
	if report.PlanPresented && (report.Outcome == "changed" || report.Outcome == "unchanged") {
		text.WriteString("readiness: all required prerequisites verified\n")
	}
	// After the plan was presented, an unfinished setup must still say which of
	// its approved actions took effect.
	if report.PlanPresented && report.Outcome != "changed" && report.Outcome != "unchanged" && len(report.Progress) != 0 {
		text.WriteString("progress:\n")
		for _, action := range report.Progress {
			fmt.Fprintf(&text, "  %s %s\n", progressToken(action), escapeDisplayLine(action.ID))
		}
	}
	if report.DryRun {
		text.WriteString("dry-run: prerequisite verification and setup remain pending\n")
	}
	if report.Outcome != "ready" {
		next := "bastion setup"
		if report.Outcome == "changed" || report.Outcome == "unchanged" {
			next = "preflight bastion"
		}
		fmt.Fprintf(&text, "next: bootwright %s", next)
		if report.ContextName != "" {
			fmt.Fprintf(&text, " --context %s", escapeDisplayLine(report.ContextName))
		}
		text.WriteByte('\n')
	}
	_, err := io.WriteString(out, text.String())
	return err
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

func controllerPlanText(text *strings.Builder, report *prerequisites.Report) {
	if report.ContextName == "" {
		text.WriteString("scope: baseline\n")
	} else {
		fmt.Fprintf(text, "scope: context %s\ncontroller: %s\n", escapeDisplayLine(report.ContextName), escapeDisplayLine(report.Machine))
	}
	fmt.Fprintf(text, "platform: %s %s/%s\nchecks:\n", escapeDisplayLine(report.Platform.OS), escapeDisplayLine(report.Platform.Release), escapeDisplayLine(report.Platform.Architecture))
	for _, check := range report.Checks {
		fmt.Fprintf(text, "  %s %s: required %s; observed %s\n", checkToken(check.Status), escapeDisplayLine(check.ID), escapeDisplayLine(check.Required), escapeDisplayLine(check.Observed))
	}
	if len(report.Actions) == 0 {
		text.WriteString("planned changes: none\n")
	} else {
		if len(report.Dependencies) != 0 {
			text.WriteString("dependencies:\n")
			for _, dependency := range report.Dependencies {
				fmt.Fprintf(text, "  %s\n", escapeDisplayLine(dependency))
			}
		}
		text.WriteString("planned changes:\n")
		for index, action := range report.Actions {
			fmt.Fprintf(text, "  %d. %s\n", index+1, escapeDisplayLine(action))
		}
	}
}
