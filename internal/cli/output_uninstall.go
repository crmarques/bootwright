package cli

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

var _ prerequisites.UninstallPresenter = (*ControllerPresenter)(nil)

const controllerUninstallHeadline = "Controller uninstall"

type uninstallPresentation struct {
	Outcome  string   `json:"outcome"`
	Packages []string `json:"packages"`
	Paths    []string `json:"paths"`
	Kept     []string `json:"kept"`
}

func (uninstallPresentation) documentedResult() {}

func (p *ControllerPresenter) PresentUninstallPlan(ctx context.Context, plan prerequisites.UninstallPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.progress.out == nil || len(plan.Actions) == 0 {
		return &controllerOutputFailure{}
	}
	var text display
	text.headline("", controllerUninstallHeadline)
	if plan.Preview != nil {
		text.section("dnf preview")
		text.verbatim(previewLines(plan.Preview))
	}
	text.section("Planned changes")
	actions := make([]step, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		actions = append(actions, step{Text: action.ID + ": " + action.Text})
	}
	text.steps(actions)
	text.section("Stays on this host")
	text.lines(displayLines(plan.Kept))
	if err := text.writeTo(p.progress.out); err != nil {
		return &controllerOutputFailure{}
	}
	p.headline = true
	return nil
}

func (p *ControllerPresenter) RemovalOutput() prerequisites.RunOutput {
	if p == nil || p.progress.out == nil {
		return io.Discard
	}
	return p.progress.out
}

func validUninstall(result *prerequisites.UninstallResult) bool {
	return result != nil && (result.Outcome == "removed" || result.Outcome == "unchanged")
}

func displayUninstall(result *prerequisites.UninstallResult) uninstallPresentation {
	return uninstallPresentation{Outcome: escapeDisplayLine(result.Outcome), Packages: displayLines(result.Packages),
		Paths: displayLines(result.Paths), Kept: displayLines(result.Kept)}
}

func writeUninstall(out io.Writer, result *prerequisites.UninstallResult) error {
	var text display
	if result.Outcome == "unchanged" {
		text.headline("OK", controllerUninstallHeadline)
		text.section("")
		text.lines([]string{"Nothing to do."})
		return text.writeTo(out)
	}
	shown := displayUninstall(result)
	text.headline("OK", "Controller uninstalled from this host")
	if len(shown.Packages) != 0 {
		text.section("Removed host packages")
		text.lines(shown.Packages)
	}
	if len(shown.Paths) != 0 {
		text.section("Removed paths")
		text.lines(shown.Paths)
	}
	if len(shown.Kept) != 0 {
		text.section("Kept")
		text.lines(shown.Kept)
	}
	return text.writeTo(out)
}
