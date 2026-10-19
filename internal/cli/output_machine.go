package cli

import (
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/inventory"
)

type machineListPresentation struct {
	Context   string                   `json:"context"`
	Machines  []machineRowPresentation `json:"machines"`
	PowerRead bool                     `json:"powerRead"`
}

// A row omits a scalar it does not have rather than writing it empty: no
// contact, no provider, or no power reading, which is distinct from a reading
// that came back unknown.
type machineRowPresentation struct {
	Name      string   `json:"name"`
	Contact   string   `json:"contact,omitempty"`
	Addresses []string `json:"addresses"`
	OS        string   `json:"os"`
	Provider  string   `json:"provider,omitempty"`
	Clusters  []string `json:"clusters"`
	Lifecycle string   `json:"lifecycle"`
	Power     string   `json:"power,omitempty"`
}

func (machineListPresentation) documentedResult() {}

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
		return writeEnvelope(out, commandEnvelope{
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
	headings := []string{"NAME", "CONTACT", "ADDRESSES", "OS", "PROVIDER", "CLUSTERS", "LIFECYCLE"}
	if result.PowerRead {
		headings = append(headings, "POWER")
	}
	rows := make([][]string, 0, len(result.Machines))
	for _, row := range result.Machines {
		cells := []string{
			row.Name, displayValue(row.Address), displayValue(strings.Join(row.IPs, ",")),
			row.OS, displayValue(row.Provider),
			displayValue(strings.Join(row.Clusters, ",")), row.Lifecycle,
		}
		if result.PowerRead {
			cells = append(cells, displayValue(row.Power))
		}
		rows = append(rows, cells)
	}
	states := []int{6}
	if result.PowerRead {
		states = append(states, 7)
	}
	text.resourceTable(headings, rows, tableStyle{Kind: "machine", Identity: 0, States: states})
	return text.writeTo(out)
}

func displayMachineList(result *inventory.ListResult) machineListPresentation {
	rows := make([]machineRowPresentation, 0, len(result.Machines))
	for _, row := range result.Machines {
		rows = append(rows, machineRowPresentation{
			Name: escapeDisplayLine(row.Name), Contact: escapeDisplayLine(row.Address),
			// Declared order, not sorted: the addresses read the way the
			// Machine authors them, and the table shows the same sequence.
			Addresses: displayLines(row.IPs), OS: escapeDisplayLine(row.OS),
			Provider: escapeDisplayLine(row.Provider),
			Clusters: displayNames(row.Clusters), Lifecycle: escapeDisplayLine(row.Lifecycle),
			Power: escapeDisplayLine(row.Power),
		})
	}
	return machineListPresentation{
		Context: escapeDisplayLine(result.Context), Machines: rows, PowerRead: result.PowerRead,
	}
}

// Machine info has its own documented DTO while preserving the inventory facts
// used by the list presentation.
type machineInfoPresentation machineListPresentation

func (machineInfoPresentation) documentedResult() {}

func writeMachineInfo(out io.Writer, command string, result *inventory.ListResult, jsonMode bool) error {
	if jsonMode {
		return writeEnvelope(out, commandEnvelope{SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result: machineInfoPresentation(displayMachineList(result)), Diagnostics: []diagnostic{}, Logs: []string{}})
	}
	var text display
	if len(result.Machines) == 0 {
		text.headline("OK", "No machines are selected")
		return text.writeTo(out)
	}
	full := presentationFor(out).Detail == "full"
	for index, row := range result.Machines {
		if index > 0 {
			text.separate()
		}
		text.resourceHeading("machine", row.Name, row.Lifecycle)
		text.subsection("Placement")
		fields := []field{{Label: "OS", Value: displayValue(row.OS)}, {Label: "Clusters", Value: displayValue(strings.Join(row.Clusters, ", "))}}
		if full {
			fields = append(fields, field{Label: "Provider", Value: displayValue(row.Provider)})
		}
		text.mutedFields(fields...)
		text.subsection("Access")
		access := []field{{Label: "Contact", Value: displayValue(row.Address)}}
		if full {
			access = append(access, field{Label: "Addresses", Value: displayValue(strings.Join(row.IPs, ", "))})
		}
		text.mutedFields(access...)
		text.subsection("Evidence")
		text.mutedFields(field{Label: "Lifecycle", Value: row.Lifecycle}, field{Label: "Power", Value: "not read"})
	}
	return text.writeTo(out)
}
