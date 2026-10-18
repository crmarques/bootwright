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
	shown := controllerPresentation{Context: escapeDisplayLine(report.ContextName), Machine: escapeDisplayLine(report.Machine), Route: escapeDisplayLine(report.Route), DryRun: plan.DryRun, Outcome: plan.Outcome, Platform: plan.Platform, Checks: []controllerCheckPresentation{}, Actions: plan.Actions, Requirements: plan.Requirements, Files: plan.Files, Progress: []controllerActionPresentation{}, Next: escapeDisplayLine(report.Next)}
	for _, c := range report.Checks {
		shown.Checks = append(shown.Checks, controllerCheckPresentation{ID: escapeDisplayLine(c.ID), Required: escapeDisplayLine(c.Required), Observed: escapeDisplayLine(c.Observed), Status: escapeDisplayLine(c.Status), Scope: escapeDisplayLine(c.Scope)})
	}
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

func writeStructuredController(out io.Writer, path string, report *prerequisites.Report, reported []diagnostic, code int) error {
	return writeStructuredResult(out, path, displayController(report), append(slices.Clone(report.Warnings), reported...), nil, code)
}
