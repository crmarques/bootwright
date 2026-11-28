package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const diskCheckPath = "machine disk-check"

func diskCheckCommand() commandSpec {
	return available(commandSpec{
		path: diskCheckPath, contextFree: true, short: "Check a disk identity in the running Linux boot",
		example: "--wwn 0x5000000000000001 --size-bytes 8192",
		long:    "Compare the specified disk identity with whole devices visible to this Linux boot. The report shows redacted identity comparisons, exact capacity and stability. Run on the target machine from the RHCOS live shell for installation rehearsal.",
		flags: []flagSpec{
			requiredFlag("wwn", "Expected disk WWN as a nonzero 0x-prefixed NAA identity"),
			requiredFlag("size-bytes", "Expected exact capacity in bytes"),
			stringFlag("serial-number", "Expected exact disk serial number"),
		},
		validate: validateDiskCheck, invoke: Services.invokeDiskCheck, render: renderDiskCheck(),
		results: []jsonResult{diskCheckPresentation{}},
	})
}

func diskCheckIdentity(flags *pflag.FlagSet) inventory.DiskCheckRequest {
	size, _ := strconv.ParseInt(stringValue(flags, "size-bytes"), 10, 64)
	return inventory.DiskCheckRequest{
		WWN: stringValue(flags, "wwn"), SizeBytes: size, SerialNumber: stringValue(flags, "serial-number"),
	}
}

func validateDiskCheck(command *cobra.Command) string {
	if command.Flags().Changed("context") {
		return "omit --context when checking the running Linux boot"
	}
	if err := inventory.ValidateDiskCheck(diskCheckIdentity(command.Flags())); err != nil {
		return err.Error()
	}
	return ""
}

func (s Services) invokeDiskCheck(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.DiskCheck == nil {
		return commandResult{}, errMissingService
	}
	report, err := s.DiskCheck(ctx, diskCheckIdentity(values.flags))
	return commandResult{diskCheck: report}, err
}

type diskCheckPresentation struct {
	Status        string                     `json:"status"`
	ExpectedBytes int64                      `json:"expectedBytes"`
	WWNMatches    int                        `json:"wwnMatches"`
	SerialMatches *int                       `json:"serialMatches,omitempty"`
	ExactMatches  int                        `json:"exactMatches"`
	Disks         []diskCheckPresentationRow `json:"disks"`
}

type diskCheckPresentationRow struct {
	Device   string `json:"device"`
	Bytes    int64  `json:"bytes"`
	WWN      string `json:"wwn"`
	Serial   string `json:"serial"`
	Eligible bool   `json:"eligible"`
	Stable   bool   `json:"stable"`
}

func (diskCheckPresentation) documentedResult() {}

func renderDiskCheck() commandRenderer {
	return commandRenderer{human: func(r *Runner, c renderCall) (bool, error) {
		report := c.result.diskCheck
		if report == nil {
			return false, nil
		}
		if selectedStructured(c.command) {
			value := diskCheckPresentation{
				Status: report.Status, ExpectedBytes: report.ExpectedBytes, WWNMatches: report.WWNMatches,
				SerialMatches: report.SerialMatches, ExactMatches: report.ExactMatches, Disks: []diskCheckPresentationRow{},
			}
			for _, row := range report.Disks {
				value.Disks = append(value.Disks, diskCheckPresentationRow(row))
			}
			return true, writeEnvelope(r.config.Out, commandEnvelope{
				SchemaVersion: "v1alpha1", Command: c.path, OK: true, ExitCode: 0,
				Result: value, Diagnostics: []diagnostic{}, Logs: []string{},
			})
		}
		var shown display
		var rows [][]string
		for _, row := range report.Disks {
			rows = append(rows, []string{row.Device, strconv.FormatInt(row.Bytes, 10), row.WWN, row.Serial, strconv.FormatBool(row.Eligible), strconv.FormatBool(row.Stable)})
		}
		if _, err := fmt.Fprintf(r.config.Out, "Disk check %s · %d exact matches · %d expected bytes\n", report.Status, report.ExactMatches, report.ExpectedBytes); err != nil {
			return true, err
		}
		shown.resourceTable([]string{"DEVICE", "BYTES", "WWN", "SERIAL", "ELIGIBLE", "STABLE"}, rows, tableStyle{Kind: "disk", Identity: 0})
		return true, shown.writeTo(r.config.Out)
	}}
}
