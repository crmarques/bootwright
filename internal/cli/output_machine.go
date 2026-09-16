package cli

import (
	"encoding/json"
	"io"
	"slices"
	"strings"

	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
)

type machineListPresentation struct {
	Context  string                   `json:"context"`
	Machines []machineRowPresentation `json:"machines"`
}

type machineRowPresentation struct {
	Name     string   `json:"name"`
	Address  string   `json:"address"`
	OS       string   `json:"os"`
	Provider string   `json:"provider"`
	Clusters []string `json:"clusters"`
	State    string   `json:"state"`
}

type machinePowerPresentation struct {
	Context  string `json:"context"`
	Machine  string `json:"machine"`
	Verb     string `json:"verb"`
	Power    string `json:"power"`
	Previous string `json:"previous"`
	Changed  bool   `json:"changed"`
}

func validMachineList(result *inventory.ListResult) bool {
	if result == nil || result.Context == "" {
		return false
	}
	return !slices.ContainsFunc(result.Machines, func(row inventory.MachineRow) bool { return row.Name == "" || row.State == "" })
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
	rows := make([][]string, 0, len(result.Machines))
	for _, row := range result.Machines {
		rows = append(rows, []string{
			escapeDisplayLine(row.Name), displayValue(row.Address), escapeDisplayLine(row.OS),
			displayValue(row.Provider), displayValue(strings.Join(row.Clusters, ",")), escapeDisplayLine(row.State),
		})
	}
	text.table([]string{"NAME", "ADDRESS", "OS", "PROVIDER", "CLUSTERS", "STATE"}, rows)
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
			OS: escapeDisplayLine(row.OS), Provider: escapeDisplayLine(row.Provider),
			Clusters: displayNames(row.Clusters), State: escapeDisplayLine(row.State),
		})
	}
	return machineListPresentation{Context: escapeDisplayLine(result.Context), Machines: rows}
}

func validDescriptor(descriptor *machineaccess.Descriptor) bool {
	return descriptor != nil && descriptor.Client != "" && len(descriptor.Arguments) != 0
}

// writeDescriptor emits exactly one handoff line for independent operator
// execution. Values are quoted so the line an operator runs carries the exact
// argument vector this command resolved, and nothing else is printed with it.
func writeDescriptor(out, errOut io.Writer, command string, descriptor *machineaccess.Descriptor) error {
	words := make([]string, 0, len(descriptor.Arguments)+1)
	for _, value := range append([]string{descriptor.Client}, descriptor.Arguments...) {
		words = append(words, shellWord(value))
	}
	if _, err := io.WriteString(out, strings.Join(words, " ")+"\n"); err != nil {
		return err
	}
	if descriptor.Advisory == "" {
		return nil
	}
	return writeDiagnostics(out, errOut, command, []diagnostic{{
		Severity: "warning", Code: "access.unavailable", Message: descriptor.Advisory,
	}}, 0, false)
}

// shellWord quotes one argument for a POSIX shell. Values that need no quoting
// are left exactly as they are, so a readable descriptor stays readable.
func shellWord(value string) string {
	if value != "" && !strings.ContainsFunc(value, func(r rune) bool { return !shellSafe(r) }) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func shellSafe(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune("-_=+:,./@%", r)
}

func validMachinePower(result *power.Result) bool {
	if result == nil || result.Machine == "" || result.Verb == "" {
		return false
	}
	return result.Power == power.StateOn || result.Power == power.StateOff
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
			Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	var text display
	status, headline := "OK", "Machine is "+result.Power
	if !result.Changed {
		status = "SKIPPED"
	}
	text.headline(status, headline)
	text.section("")
	text.fields(
		field{Label: "Machine", Value: escapeDisplayLine(result.Machine)},
		field{Label: "Power", Value: escapeDisplayLine(result.Power)},
		field{Label: "Previous", Value: displayValue(result.Previous)},
	)
	return text.writeTo(out)
}
