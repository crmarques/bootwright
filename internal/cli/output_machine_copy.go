package cli

import (
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/spf13/cobra"
)

type machineCopyPresentation struct {
	Context  string                       `json:"context"`
	Source   string                       `json:"source"`
	Machines []machineCopyRowPresentation `json:"machines"`
}

type machineCopyRowPresentation struct {
	Machine     string `json:"machine"`
	Destination string `json:"destination"`
	Status      string `json:"status"`
	Files       int64  `json:"files"`
	Bytes       int64  `json:"bytes"`
}

func (machineCopyPresentation) documentedResult() {}

func displayMachineCopy(result *machineaccess.CopyResult) machineCopyPresentation {
	source := "local:" + result.Source.Path
	if result.Source.Remote {
		source = "remote:" + result.Source.Path
	}
	presentation := machineCopyPresentation{Context: escapeDisplayLine(result.Context), Source: escapeDisplayLine(source), Machines: []machineCopyRowPresentation{}}
	for _, row := range result.Machines {
		presentation.Machines = append(presentation.Machines, machineCopyRowPresentation{
			Machine: escapeDisplayLine(row.Machine), Destination: escapeDisplayLine(row.Destination),
			Status: row.Status, Files: row.Files, Bytes: row.Bytes,
		})
	}
	return presentation
}

func writeMachineCopy(out, errOut io.Writer, path string, result *machineaccess.CopyResult, reported []diagnostic, status int, structured bool) error {
	if structured {
		return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: path, OK: status == 0, ExitCode: status,
			Result: displayMachineCopy(result), Diagnostics: displayDiagnosticsInOrder(reported), Logs: []string{}})
	}
	var text display
	state, message := "OK", "Copy completed"
	if status != 0 {
		state, message = "FAIL", "Copy incomplete; completed files remain"
	}
	text.headline(state, message)
	text.section("")
	rows := make([][]string, 0, len(result.Machines))
	for _, row := range result.Machines {
		rows = append(rows, []string{row.Machine, row.Status, strconv.FormatInt(row.Files, 10), strconv.FormatInt(row.Bytes, 10), row.Destination})
	}
	text.table([]string{"MACHINE", "STATUS", "FILES", "BYTES", "DESTINATION"}, rows)
	if err := text.writeTo(out); err != nil {
		return err
	}
	return writeHumanDiagnostics(errOut, displayDiagnosticsInOrder(reported))
}

func (r *Runner) writeCopyOutcome(ctx context.Context, command *cobra.Command, path string, result commandResult, err error) (bool, int) {
	if path != "machine cp" || result.copy == nil || len(result.copy.Machines) == 0 {
		return false, 0
	}
	status := 0
	reported := diagnostics.Of(err)
	if err != nil {
		status = 1
		if len(reported) == 0 {
			code, message := "access.copy", "copy failed; inspect the destinations before retrying"
			if errors.Is(err, context.Canceled) {
				code, message = "runtime.canceled", "copy canceled; completed files remain"
			}
			if errors.Is(err, context.DeadlineExceeded) {
				code, message = "runtime.deadline", "copy deadline exceeded; completed files remain"
			}
			reported = []diagnostic{{Severity: "error", Code: code, Message: message}}
		}
	}
	if errors.Is(context.Cause(ctx), ErrInterrupted) {
		status = 130
		reported = []diagnostic{{Severity: "error", Code: "runtime.interrupted", Message: "copy interrupted; completed files remain"}}
	}
	if writeMachineCopy(r.config.Out, r.config.ErrOut, path, result.copy, reported, status, selectedStructured(command)) != nil {
		return true, 1
	}
	return true, status
}
