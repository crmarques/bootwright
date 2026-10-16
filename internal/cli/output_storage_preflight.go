package cli

import (
	"fmt"
	"io"

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
	if _, err := fmt.Fprintf(out, "Storage preflight  %s\n", result.Context); err != nil {
		return err
	}
	for _, check := range result.Checks {
		if _, err := fmt.Fprintf(out, "  %s  %s  %s: %s\n", check.Status, check.Target, check.ID, check.Reason); err != nil {
			return err
		}
		if c := check.Capacity; c != nil {
			available := "unknown"
			if c.Observed {
				available = fmt.Sprint(c.Available)
			}
			if _, err := fmt.Fprintf(out, "    Required %d %s; available %s; reserve %d; deficit %d\n", c.Required, c.Unit, available, c.Reserve, c.Deficit); err != nil {
				return err
			}
		}
		if check.Next != "" {
			if _, err := fmt.Fprintf(out, "    Next: %s\n", check.Next); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(out, "  Summary  %d passed, %d failed, %d unknown, %d pending\n", result.Summary.Passed, result.Summary.Failed, result.Summary.Unknown, result.Summary.Pending); err != nil {
		return err
	}
	return writeHumanDiagnostics(errOut, displayDiagnostics(diagnostics))
}

func (r *Runner) writeNegativeReadinessResults(command *cobra.Command, path string, result commandResult, diagnostics []diagnostic) (bool, error) {
	if path == "preflight storage-cluster" && result.storagePreflight != nil {
		return true, writeStoragePreflight(r.config.Out, r.config.ErrOut, path, result.storagePreflight, diagnostics, 1, selectedJSON(command))
	}
	return r.writeNegativeSecretCheck(command, path, result, diagnostics)
}
