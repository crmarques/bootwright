package cli

import (
	"fmt"
	"io"
	"slices"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/spf13/cobra"
)

type machinePowerPresentation struct {
	Context  string                        `json:"context"`
	Verb     string                        `json:"verb"`
	Machines []machinePowerRowPresentation `json:"machines"`
}

type machinePowerRowPresentation struct {
	Machine  string `json:"machine"`
	Power    string `json:"power,omitempty"`
	Previous string `json:"previous,omitempty"`
	Media    string `json:"media,omitempty"`
	// PreviousMedia is what the device presented before an eject.
	PreviousMedia string   `json:"previousMedia,omitempty"`
	Changed       bool     `json:"changed"`
	Logs          []string `json:"logs"`
}

func (machinePowerPresentation) documentedResult() {}

func validMachinePower(result *power.Results, failed bool) bool {
	if result == nil || result.Context == "" || result.Verb == "" || len(result.Machines) == 0 {
		return false
	}
	return !slices.ContainsFunc(result.Machines, func(row power.Result) bool {
		settled := row.Power == machine.PowerOn || row.Power == machine.PowerOff
		return row.Machine == "" || !settled && !failed
	})
}

func powerLogs(result *power.Results) []string {
	var logs []string
	if result != nil {
		for _, row := range result.Machines {
			logs = append(logs, row.Logs...)
		}
	}
	return logs
}

func displayMachinePower(result *power.Results) machinePowerPresentation {
	rows := make([]machinePowerRowPresentation, 0, len(result.Machines))
	for _, row := range result.Machines {
		rows = append(rows, machinePowerRowPresentation{
			Machine: escapeDisplayLine(row.Machine), Power: escapeDisplayLine(row.Power),
			Previous: escapeDisplayLine(row.Previous), Media: escapeDisplayLine(row.Media), PreviousMedia: escapeDisplayLine(row.PreviousMedia),
			Changed: row.Changed, Logs: displayLines(row.Logs),
		})
	}
	return machinePowerPresentation{
		Context: escapeDisplayLine(result.Context), Verb: escapeDisplayLine(string(result.Verb)), Machines: rows,
	}
}

func writeMachinePower(out io.Writer, command string, result *power.Results, jsonMode bool) error {
	if jsonMode {
		return writeEnvelope(out, commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result: displayMachinePower(result), Diagnostics: []diagnostic{}, Logs: displayLines(powerLogs(result)),
		})
	}
	if len(result.Machines) == 1 {
		if result.Verb == power.EjectMedia {
			return writeSingleMachineEject(out, result.Machines[0])
		}
		return writeSingleMachinePower(out, result.Machines[0])
	}
	var text display
	status := "SKIPPED"
	if slices.ContainsFunc(result.Machines, func(row power.Result) bool { return row.Changed }) {
		status = "OK"
	}
	settled, _ := settledWords(result)
	text.headline(status, fmt.Sprintf("%d machines %s", len(result.Machines), settled))
	text.section("")
	writePowerTable(&text, result)
	return text.writeTo(out)
}

func settledWords(result *power.Results) (string, string) {
	if result.Verb == power.EjectMedia {
		return "ejected", "eject"
	}
	return "settled", "settle"
}

func writeMachinePowerFailure(out, errOut io.Writer, command string, result *power.Results, requested int, diagnostics []diagnostic, jsonMode bool) error {
	if jsonMode {
		return writeEnvelope(out, commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: false, ExitCode: 1,
			Result: displayMachinePower(result), Diagnostics: displayDiagnosticsInOrder(diagnostics), Logs: displayLines(powerLogs(result)),
		})
	}
	unsettled := requested
	for _, row := range result.Machines {
		if row.Power == machine.PowerOn || row.Power == machine.PowerOff {
			unsettled--
		}
	}
	var text display
	_, settle := settledWords(result)
	text.headline("FAIL", fmt.Sprintf("%d of %d machines did not %s", unsettled, requested, settle))
	text.section("")
	writePowerTable(&text, result)
	if err := text.writeTo(out); err != nil {
		return err
	}
	return writeHumanDiagnostics(errOut, displayDiagnosticsInOrder(diagnostics))
}

func writePowerTable(text *display, result *power.Results) {
	if result.Verb == power.EjectMedia {
		writeEjectTable(text, result)
		return
	}
	rows := make([][]string, 0, len(result.Machines))
	for _, row := range result.Machines {
		rows = append(rows, []string{row.Machine, displayValue(row.Power), displayValue(row.Previous), displayValue(row.LogLocation)})
	}
	text.resourceTable([]string{"MACHINE", "POWER", "PREVIOUS", "LOGS"}, rows, tableStyle{Kind: "machine", Identity: 0, States: []int{1}})
}

func writeSingleMachinePower(out io.Writer, result power.Result) error {
	var text display
	status := "OK"
	if !result.Changed {
		status = "SKIPPED"
	}
	text.headline(status, "Machine is "+result.Power)
	text.section("")
	fields := []field{
		{Label: "Machine", Value: result.Machine},
		{Label: "Power", Value: result.Power},
		{Label: "Previous", Value: displayValue(result.Previous)},
	}
	fields = append(fields, logFields(result.LogLocation, logsHint{Context: result.Context, Run: result.Run})...)
	text.fields(fields...)
	return text.writeTo(out)
}

func writeEjectTable(text *display, result *power.Results) {
	rows := make([][]string, 0, len(result.Machines))
	for _, row := range result.Machines {
		rows = append(rows, []string{row.Machine, displayValue(row.Media), displayValue(row.PreviousMedia), displayValue(row.Power), displayValue(row.LogLocation)})
	}
	text.resourceTable([]string{"MACHINE", "MEDIA", "PREVIOUS", "POWER", "LOGS"}, rows, tableStyle{Kind: "machine", Identity: 0, States: []int{1, 3}})
}

func writeSingleMachineEject(out io.Writer, result power.Result) error {
	var text display
	status := "OK"
	if !result.Changed {
		status = "SKIPPED"
	}
	text.headline(status, "Machine presents no media")
	text.section("")
	fields := []field{
		{Label: "Machine", Value: result.Machine},
		{Label: "Media", Value: displayValue(result.Media)},
		{Label: "Previous", Value: displayValue(result.PreviousMedia)},
		{Label: "Power", Value: displayValue(result.Power)},
	}
	fields = append(fields, logFields(result.LogLocation, logsHint{Context: result.Context, Run: result.Run})...)
	text.fields(fields...)
	return text.writeTo(out)
}

func (r *Runner) writeFailedPowerBatch(command *cobra.Command, path string, result commandResult, diagnostics []diagnostic) (bool, error) {
	requested := len(normalizeNames(stringValue(command.Flags(), "name")))
	if requested < 2 || !validMachinePower(result.power, true) {
		return false, nil
	}
	return true, writeMachinePowerFailure(r.config.Out, r.config.ErrOut, path, result.power, requested, diagnostics, selectedJSON(command))
}

type machineMediaStatusPresentation struct {
	Context  string                              `json:"context"`
	Machines []machineMediaStatusRowPresentation `json:"machines"`
}

type machineMediaStatusRowPresentation struct {
	Machine string `json:"machine"`
	Media   string `json:"media"`
}

func (machineMediaStatusPresentation) documentedResult() {}

func writeMachineMediaStatus(out io.Writer, command string, result *power.MediaStatusResult, jsonMode bool) error {
	if jsonMode {
		rows := make([]machineMediaStatusRowPresentation, 0, len(result.Machines))
		for _, row := range result.Machines {
			rows = append(rows, machineMediaStatusRowPresentation{
				Machine: escapeDisplayLine(row.Machine), Media: escapeDisplayLine(row.Media),
			})
		}
		return writeEnvelope(out, commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result:      machineMediaStatusPresentation{Context: escapeDisplayLine(result.Context), Machines: rows},
			Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	var text display
	rows := make([][]string, 0, len(result.Machines))
	for _, row := range result.Machines {
		rows = append(rows, []string{row.Machine, row.Media})
	}
	text.resourceTable([]string{"MACHINE", "MEDIA"}, rows, tableStyle{Kind: "machine", Identity: 0, States: []int{1}})
	return text.writeTo(out)
}
