package cli

import (
	"io"
	"slices"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type controllerPresentation struct {
	Context      string                         `json:"context,omitempty"`
	Machine      string                         `json:"machine,omitempty"`
	Route        string                         `json:"route,omitempty"`
	DryRun       bool                           `json:"dryRun"`
	Outcome      string                         `json:"outcome"`
	Platform     string                         `json:"platform"`
	Checks       []controllerCheckPresentation  `json:"checks"`
	Actions      []string                       `json:"actions"`
	Requirements []string                       `json:"requirements"`
	Files        []setupLockedWheel             `json:"files"`
	Preview      *controllerPreviewPresentation `json:"preview,omitempty"`
	Progress     []controllerActionPresentation `json:"progress"`
	Receipt      *controllerReceiptPresentation `json:"receipt,omitempty"`
	Next         string                         `json:"next"`
}

func (controllerPresentation) documentedResult() {}

type installCheckResult struct {
	Context  string                        `json:"context,omitempty"`
	Machine  string                        `json:"machine,omitempty"`
	Tools    []toolCheckPresentation       `json:"tools,omitempty"`
	Outcome  string                        `json:"outcome"`
	Platform string                        `json:"platform"`
	Checks   []controllerCheckPresentation `json:"checks"`
	Next     string                        `json:"next"`
}

func (installCheckResult) documentedResult() {}

type toolCheckPresentation struct {
	Executable    string   `json:"executable"`
	Version       string   `json:"version"`
	Compatibility string   `json:"compatibility,omitempty"`
	SelectedBy    []string `json:"selectedBy"`
	Status        string   `json:"status"`
	Observed      string   `json:"observed"`
}

type controllerCheckPresentation struct {
	ID       string `json:"id"`
	Required string `json:"required"`
	Observed string `json:"observed"`
	Status   string `json:"status"`
	Scope    string `json:"scope"`
}

type controllerActionPresentation struct {
	ID      string `json:"id"`
	Phase   string `json:"phase"`
	Outcome string `json:"outcome"`
}

type controllerPreviewPresentation struct {
	Text      string   `json:"text"`
	Truncated bool     `json:"truncated"`
	Notes     []string `json:"notes"`
}

type controllerReceiptPresentation struct {
	Host     string `json:"host"`
	Runtime  string `json:"runtime"`
	Packages string `json:"packages"`
}

func displayController(report *prerequisites.Report) controllerPresentation {
	plan := displaySetupPlan(report)
	shown := controllerPresentation{Context: escapeDisplayLine(report.ContextName), Machine: escapeDisplayLine(report.Machine), Route: escapeDisplayLine(report.Route), DryRun: plan.DryRun, Outcome: plan.Outcome, Platform: plan.Platform, Checks: displayControllerChecks(report), Actions: plan.Actions, Requirements: plan.Requirements, Files: plan.Files, Progress: []controllerActionPresentation{}, Next: escapeDisplayLine(report.Next)}
	for _, p := range report.Progress {
		shown.Progress = append(shown.Progress, controllerActionPresentation{ID: escapeDisplayLine(p.ID), Phase: escapeDisplayLine(p.Phase), Outcome: escapeDisplayLine(p.Outcome)})
	}
	if p := report.Preview; p != nil {
		shown.Preview = &controllerPreviewPresentation{Text: escapeDisplayLine(p.Text), Truncated: p.Truncated, Notes: displayLines(p.Notes)}
	}
	if r := report.Table; r != nil {
		shown.Receipt = &controllerReceiptPresentation{Host: escapeDisplayLine(r.Host), Runtime: escapeDisplayLine(r.Runtime), Packages: escapeDisplayLine(r.Packages)}
	}
	return shown
}

func displayControllerChecks(report *prerequisites.Report) []controllerCheckPresentation {
	shown := make([]controllerCheckPresentation, 0, len(report.Checks))
	for _, c := range report.Checks {
		shown = append(shown, controllerCheckPresentation{ID: escapeDisplayLine(c.ID), Required: escapeDisplayLine(c.Required), Observed: escapeDisplayLine(c.Observed), Status: escapeDisplayLine(c.Status), Scope: escapeDisplayLine(c.Scope)})
	}
	return shown
}

func writeStructuredController(out io.Writer, path string, report *prerequisites.Report, check bool, reported []diagnostic, code int) error {
	var result jsonResult
	switch {
	case check:
		checkResult := installCheckResult{Context: escapeDisplayLine(report.ContextName), Machine: escapeDisplayLine(report.Machine), Outcome: escapeDisplayLine(report.Outcome), Platform: controllerPlatform(report), Checks: displayControllerChecks(report), Next: escapeDisplayLine(report.Next)}
		for _, tool := range report.Tools {
			checkResult.Tools = append(checkResult.Tools, toolCheckPresentation{
				Executable: escapeDisplayLine(tool.Executable), Version: escapeDisplayLine(tool.Version),
				Compatibility: escapeDisplayLine(tool.Compatibility), SelectedBy: displayLines(tool.SelectedBy),
				Status: escapeDisplayLine(tool.Status), Observed: escapeDisplayLine(tool.Observed),
			})
		}
		result = checkResult
	case report.DryRun:
		result = displaySetupPlan(report)
	default:
		result = displayController(report)
	}
	return writeStructuredResult(out, path, result, append(slices.Clone(report.Warnings), reported...), nil, code)
}
