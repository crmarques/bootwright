package cli

import (
	"io"

	"github.com/crmarques/bootwright/internal/managedos/diskdiscovery"
)

type machineDiscoveryPresentation struct {
	Operation   operationPresentation            `json:"operation"`
	Inventories []discoveryInventoryPresentation `json:"inventories"`
	Next        string                           `json:"next,omitempty"`
}

type discoveryInventoryPresentation struct {
	Machine string                      `json:"machine"`
	Disks   []discoveryDiskPresentation `json:"disks"`
}

type discoveryDiskPresentation struct {
	WWN                  string `json:"wwn,omitempty"`
	SerialNumber         string `json:"serialNumber,omitempty"`
	SizeBytes            int64  `json:"sizeBytes"`
	Model                string `json:"model"`
	ControllerPCIAddress string `json:"controllerPCIAddress,omitempty"`
	Transport            string `json:"transport"`
	Fixed                bool   `json:"fixed"`
	Writable             bool   `json:"writable"`
	Whole                bool   `json:"whole"`
	Hidden               bool   `json:"hidden"`
}

func (machineDiscoveryPresentation) documentedResult() {}

func displayMachineDiscovery(result *diskdiscovery.DiscoverResult) machineDiscoveryPresentation {
	out := machineDiscoveryPresentation{Operation: displayOperation(result.Operation), Inventories: []discoveryInventoryPresentation{}}
	for _, inventory := range result.Inventories {
		row := discoveryInventoryPresentation{Machine: escapeDisplayLine(inventory.Machine), Disks: []discoveryDiskPresentation{}}
		for _, d := range inventory.Disks {
			row.Disks = append(row.Disks, discoveryDiskPresentation{WWN: escapeDisplayLine(d.WWN), SerialNumber: escapeDisplayLine(d.SerialNumber), SizeBytes: d.SizeBytes, Model: escapeDisplayLine(d.Model), ControllerPCIAddress: escapeDisplayLine(d.ControllerPCIAddress), Transport: escapeDisplayLine(d.Transport), Fixed: d.Fixed, Writable: d.Writable, Whole: d.Whole, Hidden: d.Hidden})
		}
		out.Inventories = append(out.Inventories, row)
	}
	if result.Operation.Receipt.State == "done" {
		out.Next = "bootwright plan --context " + escapeDisplayLine(result.Operation.Context.Name)
	}
	return out
}

func writeMachineDiscovery(out io.Writer, path string, result *diskdiscovery.DiscoverResult, structured bool) error {
	shown := displayMachineDiscovery(result)
	if structured {
		return writeStructuredResult(out, path, shown, result.Operation.Warnings, nil, 0)
	}
	if err := writeLifecycleOperation(out, result.Operation); err != nil {
		return err
	}
	var text display
	for _, inventory := range result.Inventories {
		text.section("Disk inventory · " + inventory.Machine)
		rows := make([][]string, 0, len(inventory.Disks))
		for _, d := range inventory.Disks {
			identity := "wwn:" + d.WWN
			if d.WWN == "" {
				identity = "serial:" + d.SerialNumber
			}
			rows = append(rows, []string{identity, humanBytes(d.SizeBytes), displayValue(d.Model), displayValue(d.ControllerPCIAddress), displayValue(d.Transport)})
		}
		text.resourceTable([]string{"DISK", "SIZE", "MODEL", "CONTROLLER", "TRANSPORT"}, rows, tableStyle{Kind: "disk", Identity: 0})
	}
	if result.Operation.Receipt.State == "done" {
		text.section("")
		text.verbatimFields(field{Label: "Next", Value: "bootwright plan --context " + result.Operation.Context.Name, Tone: "command"})
	}
	if len(text.blocks) != 0 {
		if _, err := io.WriteString(out, "\n"); err != nil {
			return err
		}
	}
	return text.writeTo(out)
}
