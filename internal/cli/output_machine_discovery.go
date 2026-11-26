package cli

import (
	"io"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/managedos/diskdiscovery"
)

type machineDiscoveryPresentation struct {
	Operation   operationPresentation            `json:"operation"`
	Inventories []discoveryInventoryPresentation `json:"inventories"`
	Next        string                           `json:"next,omitempty"`
}

type diskInventoriesPresentation struct {
	Inventories []discoveryInventoryPresentation `json:"inventories"`
}

type discoveryInventoryPresentation struct {
	Machine   string                      `json:"machine"`
	Operation string                      `json:"operation,omitempty"`
	Source    string                      `json:"source,omitempty"`
	Disks     []discoveryDiskPresentation `json:"disks"`
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
func (diskInventoriesPresentation) documentedResult()  {}

func displayMachineDiscovery(result *diskdiscovery.DiscoverResult) machineDiscoveryPresentation {
	out := machineDiscoveryPresentation{Operation: displayOperation(result.Operation), Inventories: displayDiskInventories(result.Inventories)}
	if result.Operation.Receipt.State == "done" && len(out.Inventories) != 0 {
		names := make([]string, 0, len(out.Inventories))
		for _, inventory := range out.Inventories {
			names = append(names, inventory.Machine)
		}
		out.Next = "bootwright machine inventory --context " + escapeDisplayLine(result.Operation.Context.Name) + " --name " + strings.Join(names, ",")
	}
	return out
}

func displayDiskInventories(inventories []diskdiscovery.DiscoveryInventory) []discoveryInventoryPresentation {
	shown := make([]discoveryInventoryPresentation, 0, len(inventories))
	for _, inventory := range inventories {
		row := discoveryInventoryPresentation{
			Machine: escapeDisplayLine(inventory.Machine), Operation: escapeDisplayLine(inventory.Operation), Source: escapeDisplayLine(inventory.Source),
			Disks: make([]discoveryDiskPresentation, 0, len(inventory.Disks)),
		}
		for _, d := range inventory.Disks {
			row.Disks = append(row.Disks, discoveryDiskPresentation{WWN: escapeDisplayLine(d.WWN), SerialNumber: escapeDisplayLine(d.SerialNumber), SizeBytes: d.SizeBytes, Model: escapeDisplayLine(d.Model), ControllerPCIAddress: escapeDisplayLine(d.ControllerPCIAddress), Transport: escapeDisplayLine(d.Transport), Fixed: d.Fixed, Writable: d.Writable, Whole: d.Whole, Hidden: d.Hidden})
		}
		shown = append(shown, row)
	}
	return shown
}

func writeDiskInventory(out io.Writer, path string, inventories []diskdiscovery.DiscoveryInventory, structured bool) error {
	shown := displayDiskInventories(inventories)
	if structured {
		return writeStructuredResult(out, path, diskInventoriesPresentation{Inventories: shown}, nil, nil, 0)
	}
	var text display
	appendDiskInventories(&text, shown)
	return text.writeTo(out)
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
	appendDiskInventories(&text, shown.Inventories)
	if shown.Next != "" {
		text.section("")
		text.verbatimFields(field{Label: "Next", Value: shown.Next, Tone: "command"})
	}
	if len(text.blocks) != 0 {
		if _, err := io.WriteString(out, "\n"); err != nil {
			return err
		}
	}
	return text.writeTo(out)
}

func appendDiskInventories(text *display, inventories []discoveryInventoryPresentation) {
	for _, inventory := range inventories {
		text.section("Disk inventory · " + inventory.Machine)
		if inventory.Source == "" {
			text.lines([]string{"No completed disk inventory in the current apply lineage"})
			continue
		}
		text.verbatimFields(field{Label: "Source", Value: inventory.Source}, field{Label: "Operation", Value: inventory.Operation})
		rows := make([][]string, 0, len(inventory.Disks))
		for _, d := range inventory.Disks {
			var identities []string
			if d.WWN != "" {
				identities = append(identities, "wwn:"+d.WWN)
			}
			if d.SerialNumber != "" {
				identities = append(identities, "serial:"+d.SerialNumber)
			}
			rows = append(rows, []string{displayValue(strings.Join(identities, " ")), humanBytes(d.SizeBytes), strconv.FormatInt(d.SizeBytes, 10), displayValue(d.Model), displayValue(d.ControllerPCIAddress), displayValue(d.Transport)})
		}
		text.resourceTable([]string{"DISK", "SIZE", "BYTES", "MODEL", "CONTROLLER", "TRANSPORT"}, rows, tableStyle{Kind: "disk", Identity: 0})
	}
}
