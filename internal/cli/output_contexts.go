package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func validContextSummary(summary contexts.Summary) bool {
	return summary.Name != "" && summary.ID != "" && (summary.Mode == contexts.Ready || summary.Mode == contexts.Initializing || summary.Mode == contexts.Deleting)
}

func writeContextSummary(out io.Writer, summary contexts.Summary) error {
	_, err := fmt.Fprintf(out, "name: %s\nid: %s\nmode: %s\ncurrent: %t\ninput configured: %t\n", escapeDisplayLine(summary.Name), escapeDisplayLine(summary.ID), escapeDisplayLine(string(summary.Mode)), summary.Current, summary.Configured)
	return err
}

func writeAdmission(out, errOut io.Writer, command string, result *contexts.AdmissionResult) error {
	if err := writeHumanDiagnostics(errOut, displayDiagnostics(result.Diagnostics)); err != nil {
		return err
	}
	action := "initialized"
	if command == "context update" {
		action = "updated"
	}
	if !result.InputChanged {
		if command == "context update" {
			action = "unchanged"
		}
		if _, err := fmt.Fprintf(out, "[OK] Context %s\n", action); err != nil {
			return err
		}
		return writeContextSummary(out, result.Context)
	}
	if _, err := fmt.Fprintf(out, "[OK] Context %s (files copied: %d, files seen: %d, objects decoded: %d)\n", action, result.FilesCopied, result.Counts.FilesSeen, result.Counts.ObjectsDecoded); err != nil {
		return err
	}
	return writeContextSummary(out, result.Context)
}

func writeContextUse(out io.Writer, result *contexts.UseResult) error {
	if _, err := io.WriteString(out, "[OK] Current context selected\n"); err != nil {
		return err
	}
	return writeContextSummary(out, result.Context)
}

func writeContextList(out io.Writer, result *contexts.ListResult) error {
	if len(result.Contexts) == 0 {
		_, err := io.WriteString(out, "[OK] No contexts\n")
		return err
	}
	summaries := slices.Clone(result.Contexts)
	slices.SortStableFunc(summaries, func(a, b contexts.Summary) int { return strings.Compare(a.Name, b.Name) })
	if _, err := io.WriteString(out, "NAME\tID\tMODE\tCURRENT\tINPUT\n"); err != nil {
		return err
	}
	for _, summary := range summaries {
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%t\t%t\n", escapeDisplayLine(summary.Name), escapeDisplayLine(summary.ID), escapeDisplayLine(string(summary.Mode)), summary.Current, summary.Configured); err != nil {
			return err
		}
	}
	return nil
}

func writeContextCurrent(out io.Writer, result *contexts.CurrentResult, short bool) error {
	if short {
		_, err := fmt.Fprintln(out, escapeDisplayLine(result.Context.Name))
		return err
	}
	return writeContextSummary(out, result.Context)
}

func writeContextDelete(out io.Writer, result *contexts.DeleteResult) error {
	_, err := fmt.Fprintf(out, "[OK] Context deleted (name: %s, id: %s, current cleared: %t)\n", escapeDisplayLine(result.Name), escapeDisplayLine(result.ID), result.CurrentCleared)
	return err
}
