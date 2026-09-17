package cli

import (
	"encoding/json"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
)

type machineListPresentation struct {
	Context   string                   `json:"context"`
	Machines  []machineRowPresentation `json:"machines"`
	PowerRead bool                     `json:"powerRead"`
}

type machineRowPresentation struct {
	Name      string   `json:"name"`
	Address   string   `json:"address"`
	IPs       []string `json:"ips"`
	OS        string   `json:"os"`
	Provider  string   `json:"provider"`
	Clusters  []string `json:"clusters"`
	Lifecycle string   `json:"lifecycle"`
	Power     string   `json:"power"`
}

type machinePowerPresentation struct {
	Context  string `json:"context"`
	Machine  string `json:"machine"`
	Verb     string `json:"verb"`
	Power    string `json:"power"`
	Previous string `json:"previous"`
	Changed  bool   `json:"changed"`
}

// A row is presentable when it names the Machine and where this context's own
// operations left it. A power reading is the controller's answer, so an empty
// one is a Machine no controller answered for and never a defective row.
func validMachineList(result *inventory.ListResult) bool {
	if result == nil || result.Context == "" {
		return false
	}
	return !slices.ContainsFunc(result.Machines, func(row inventory.MachineRow) bool {
		return row.Name == "" || row.Lifecycle == "" || !presentablePower(row.Power) || row.Power != "" && !result.PowerRead
	})
}

func presentablePower(reading string) bool {
	return reading == "" || slices.Contains([]string{machine.PowerOn, machine.PowerOff, machine.PowerUnknown}, reading)
}

func writeMachineList(out io.Writer, command string, result *inventory.ListResult, silent, jsonMode bool) error {
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result: displayMachineList(result), Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	if silent {
		// A silent invocation is read by another program, so it carries the
		// names alone: no status, no layout and no surrounding prose. An empty
		// selection writes nothing at all rather than one empty line.
		names := displayNames(inventory.Names(result.Machines))
		if len(names) == 0 {
			return nil
		}
		_, err := io.WriteString(out, strings.Join(names, "\n")+"\n")
		return err
	}
	var text display
	if len(result.Machines) == 0 {
		text.headline("OK", "No machines are selected")
		return text.writeTo(out)
	}
	// The power column appears only where a reading was taken, so the table
	// never shows a column of absences for the answer nobody asked for.
	headings := []string{"NAME", "ADDRESS", "IP", "OS", "PROVIDER", "CLUSTERS", "LIFECYCLE"}
	if result.PowerRead {
		headings = append(headings, "POWER")
	}
	rows := make([][]string, 0, len(result.Machines))
	for _, row := range result.Machines {
		cells := []string{
			escapeDisplayLine(row.Name), displayValue(row.Address), displayValue(strings.Join(row.IPs, ",")),
			escapeDisplayLine(row.OS), displayValue(row.Provider),
			displayValue(strings.Join(row.Clusters, ",")), escapeDisplayLine(row.Lifecycle),
		}
		if result.PowerRead {
			cells = append(cells, displayValue(row.Power))
		}
		rows = append(rows, cells)
	}
	text.table(headings, rows)
	return text.writeTo(out)
}

// displayValue keeps a table column aligned when a Machine declares nothing
// for it, so an absent value reads as absent rather than as an empty cell.
func displayValue(value string) string {
	if value == "" {
		return "-"
	}
	return escapeDisplayLine(value)
}

func displayMachineList(result *inventory.ListResult) machineListPresentation {
	rows := make([]machineRowPresentation, 0, len(result.Machines))
	for _, row := range result.Machines {
		rows = append(rows, machineRowPresentation{
			Name: escapeDisplayLine(row.Name), Address: escapeDisplayLine(row.Address),
			// Declared order, not sorted: the addresses read the way the
			// Machine authors them, and the table shows the same sequence.
			IPs: displayLines(row.IPs), OS: escapeDisplayLine(row.OS),
			Provider: escapeDisplayLine(row.Provider),
			Clusters: displayNames(row.Clusters), Lifecycle: escapeDisplayLine(row.Lifecycle),
			Power: escapeDisplayLine(row.Power),
		})
	}
	return machineListPresentation{
		Context: escapeDisplayLine(result.Context), Machines: rows, PowerRead: result.PowerRead,
	}
}

func validMachinePower(result *power.Result) bool {
	if result == nil || result.Machine == "" || result.Verb == "" {
		return false
	}
	return result.Power == machine.PowerOn || result.Power == machine.PowerOff
}

func writeMachinePower(out io.Writer, command string, result *power.Result, jsonMode bool) error {
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result: machinePowerPresentation{
				Context: escapeDisplayLine(result.Context), Machine: escapeDisplayLine(result.Machine),
				Verb: escapeDisplayLine(result.Verb), Power: escapeDisplayLine(result.Power),
				Previous: escapeDisplayLine(result.Previous), Changed: result.Changed,
			},
			Diagnostics: []diagnostic{}, Logs: displayLines(result.Logs),
		})
	}
	var text display
	status, headline := "OK", "Machine is "+result.Power
	if !result.Changed {
		status = "SKIPPED"
	}
	text.headline(status, headline)
	text.section("")
	fields := []field{
		{Label: "Machine", Value: escapeDisplayLine(result.Machine)},
		{Label: "Power", Value: escapeDisplayLine(result.Power)},
		{Label: "Previous", Value: displayValue(result.Previous)},
	}
	if result.LogLocation != "" {
		fields = append(fields, field{Label: logLocationLabel, Value: escapeDisplayLine(result.LogLocation)})
	}
	text.fields(fields...)
	return text.writeTo(out)
}
