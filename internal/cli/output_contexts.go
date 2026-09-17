package cli

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func validContextSummary(summary contexts.Summary) bool {
	return summary.Name != "" && (summary.Mode == contexts.Ready || summary.Mode == contexts.Initializing || summary.Mode == contexts.Deleting)
}

func writeContextSummary(out io.Writer, summary contexts.Summary) error {
	var text display
	contextSummaryFields(&text, summary)
	return text.writeTo(out)
}

func contextSummaryFields(text *display, summary contexts.Summary) {
	text.section("")
	text.fields(
		field{Label: "Name", Value: summary.Name},
		field{Label: "Mode", Value: string(summary.Mode)},
		field{Label: "Current", Value: strconv.FormatBool(summary.Current)},
		field{Label: "Input configured", Value: strconv.FormatBool(summary.Configured)},
	)
}

func writeAdmission(out, errOut io.Writer, command string, result *contexts.AdmissionResult) error {
	if err := writeHumanDiagnostics(errOut, displayDiagnostics(result.Diagnostics)); err != nil {
		return err
	}
	action := "initialized"
	if command == "context update" {
		action = "updated"
	}
	var text display
	if !result.InputChanged {
		if command == "context update" {
			action = "unchanged"
		}
		text.headline("OK", "Context "+action)
		contextSummaryFields(&text, result.Context)
		return text.writeTo(out)
	}
	text.headline("OK", "Context "+action)
	contextSummaryFields(&text, result.Context)
	text.section("Admitted input")
	text.fields(
		field{Label: "Files copied", Value: strconv.Itoa(result.FilesCopied)},
		field{Label: "Files seen", Value: strconv.Itoa(result.Counts.FilesSeen)},
		field{Label: "Objects decoded", Value: strconv.Itoa(result.Counts.ObjectsDecoded)},
	)
	return text.writeTo(out)
}

func writeContextUse(out io.Writer, result *contexts.UseResult) error {
	var text display
	text.headline("OK", "Current context selected")
	contextSummaryFields(&text, result.Context)
	return text.writeTo(out)
}

func writeContextList(out io.Writer, result *contexts.ListResult) error {
	var text display
	if len(result.Contexts) == 0 {
		text.headline("OK", "No contexts")
		return text.writeTo(out)
	}
	summaries := slices.Clone(result.Contexts)
	slices.SortStableFunc(summaries, func(a, b contexts.Summary) int { return strings.Compare(a.Name, b.Name) })
	rows := make([][]string, 0, len(summaries))
	for _, summary := range summaries {
		rows = append(rows, []string{summary.Name, string(summary.Mode), strconv.FormatBool(summary.Current), strconv.FormatBool(summary.Configured)})
	}
	text.table([]string{"NAME", "MODE", "CURRENT", "INPUT"}, rows)
	return text.writeTo(out)
}

func writeContextCurrent(out io.Writer, result *contexts.CurrentResult, short bool) error {
	if short {
		_, err := fmt.Fprintln(out, escapeDisplayLine(result.Context.Name))
		return err
	}
	return writeContextSummary(out, result.Context)
}

func writeContextDelete(out, errOut io.Writer, result *contexts.DeleteResult) error {
	if result.OrphansAbandoned {
		warning := diagnostic{Severity: "warning", Code: "context.unsafe-delete", Message: "the objects this context owned were abandoned and are no longer managed"}
		if err := writeHumanDiagnostics(errOut, displayDiagnostics([]diagnostic{warning})); err != nil {
			return err
		}
	}
	var text display
	text.headline("OK", "Context deleted")
	text.section("")
	fields := []field{
		{Label: "Name", Value: result.Name},
		{Label: "Current cleared", Value: strconv.FormatBool(result.CurrentCleared)},
	}
	if result.OrphansAbandoned {
		fields = append(fields, field{Label: "Orphans abandoned", Value: "true"})
	}
	text.fields(fields...)
	return text.writeTo(out)
}
