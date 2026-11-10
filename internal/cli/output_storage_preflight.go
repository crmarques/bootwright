package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/spf13/cobra"
)

type storageCapacityResult struct {
	Resource  string `json:"resource"`
	Unit      string `json:"unit"`
	Required  int64  `json:"required"`
	Available int64  `json:"available"`
	Reserve   int64  `json:"reserve"`
	Deficit   int64  `json:"deficit"`
	Observed  bool   `json:"observed"`
}
type storageCheckResult struct {
	ID       string                 `json:"id"`
	Target   string                 `json:"target"`
	Status   string                 `json:"status"`
	Reason   string                 `json:"reason"`
	Next     string                 `json:"next,omitempty"`
	Capacity *storageCapacityResult `json:"capacity,omitempty"`
}
type storageSummaryResult struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Unknown int `json:"unknown"`
	Pending int `json:"pending"`
}
type storagePreflightResult struct {
	Context string               `json:"context"`
	Target  string               `json:"target"`
	Checks  []storageCheckResult `json:"checks"`
	Summary storageSummaryResult `json:"summary"`
}

func (storagePreflightResult) documentedResult() {}

func writeStoragePreflight(out, errOut io.Writer, command string, report *preflight.Report, diagnostics []diagnostic, exitCode int, jsonMode bool) error {
	result := storagePreflightResult{Context: escapeDisplayLine(report.Context), Target: escapeDisplayLine(report.Target), Checks: []storageCheckResult{}, Summary: storageSummaryResult{report.Summary.Passed, report.Summary.Failed, report.Summary.Unknown, report.Summary.Pending}}
	for _, check := range report.Checks {
		row := storageCheckResult{ID: escapeDisplayLine(check.ID), Target: escapeDisplayLine(check.Target), Status: escapeDisplayLine(check.Status), Reason: escapeDisplayLine(check.Reason), Next: escapeDisplayLine(check.Next)}
		if c := check.Capacity; c != nil {
			row.Capacity = &storageCapacityResult{Resource: escapeDisplayLine(c.Resource), Unit: escapeDisplayLine(c.Unit), Required: c.Required, Available: c.Available, Reserve: c.Reserve, Deficit: c.Deficit, Observed: c.Observed}
		}
		result.Checks = append(result.Checks, row)
	}
	if jsonMode {
		return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: command, OK: exitCode == 0, ExitCode: exitCode, Result: result, Diagnostics: displayDiagnostics(diagnostics), Logs: []string{}})
	}
	if err := writeStoragePreflightText(out, report); err != nil {
		return err
	}
	return writeHumanDiagnostics(errOut, displayDiagnostics(diagnostics))
}

func writeStoragePreflightText(out io.Writer, report *preflight.Report) error {
	var text display
	text.headline(storagePreflightHeadlineStatus(report.Summary), "Preflight "+report.Context)
	text.fields(field{Label: "Stages", Value: strings.ReplaceAll(report.Target, ",", ", ")})
	for n, check := range report.Checks {
		if n == 0 {
			text.section("Checks")
		} else {
			text.separate()
		}
		text.resourceHeading("", check.ID, check.Status)
		details := []field{{Label: "Target", Value: check.Target}, {Label: "Reason", Value: check.Reason}}
		if c := check.Capacity; c != nil {
			available := "unknown"
			if c.Observed {
				available = fmt.Sprint(c.Available)
			}
			details = append(details, field{Label: "Capacity", Value: fmt.Sprintf("required %d %s; available %s; reserve %d; deficit %d", c.Required, c.Unit, available, c.Reserve, c.Deficit)})
		}
		if check.Next != "" {
			details = append(details, field{Label: "Next", Value: check.Next})
		}
		text.mutedFields(details...)
	}
	text.section("")
	text.fields(field{Label: "Summary", Value: fmt.Sprintf("%d passed, %d failed, %d unknown, %d pending", report.Summary.Passed, report.Summary.Failed, report.Summary.Unknown, report.Summary.Pending)})
	return text.writeTo(out)
}

func storagePreflightHeadlineStatus(summary preflight.Summary) string {
	switch {
	case summary.Failed > 0:
		return "FAIL"
	case summary.Unknown > 0:
		return "UNKNOWN"
	case summary.Pending > 0:
		return "PENDING"
	}
	return "OK"
}

func (r *Runner) writeNegativeReadinessResults(command *cobra.Command, path string, result commandResult, diagnostics []diagnostic) (bool, error) {
	if path == "preflight" && result.storagePreflight != nil {
		return true, writeStoragePreflight(r.config.Out, r.config.ErrOut, path, result.storagePreflight, diagnostics, 1, selectedJSON(command))
	}
	return r.writeNegativeSecretCheck(command, path, result, diagnostics)
}
