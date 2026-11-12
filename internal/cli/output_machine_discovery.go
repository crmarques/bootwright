package cli

import (
	"fmt"
	"io"

	"github.com/crmarques/bootwright/internal/managedos/installation"
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

func displayMachineDiscovery(result *installation.DiscoverResult) machineDiscoveryPresentation {
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

func writeMachineDiscovery(out io.Writer, path string, result *installation.DiscoverResult, structured bool) error {
	shown := displayMachineDiscovery(result)
	if structured {
		return writeStructuredResult(out, path, shown, result.Operation.Warnings, nil, 0)
	}
	if err := writeLifecycleOperation(out, result.Operation); err != nil {
		return err
	}
	for _, inventory := range shown.Inventories {
		if _, err := fmt.Fprintf(out, "\nDisk inventory for %s\n", inventory.Machine); err != nil {
			return err
		}
		for _, d := range inventory.Disks {
			identity := "wwn:" + d.WWN
			if d.WWN == "" {
				identity = "serial:" + d.SerialNumber
			}
			if _, err := fmt.Fprintf(out, "  %s  sizeBytes=%d  model=%q  controller=%s  transport=%s\n", identity, d.SizeBytes, d.Model, d.ControllerPCIAddress, d.Transport); err != nil {
				return err
			}
		}
	}
	if shown.Next != "" {
		_, err := fmt.Fprintf(out, "\nNext: %s\n", shown.Next)
		return err
	}
	return nil
}
