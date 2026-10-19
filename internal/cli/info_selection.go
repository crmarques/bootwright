package cli

import (
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// An omitted selector means all; names match exactly after the shared
// whitespace and duplicate normalization.
func selectedInfoNames(values *requestValues) []string {
	raw := values.text("name")
	if !values.flags.Changed("name") {
		return nil
	}
	return append([]string{}, normalizeNames(raw)...)
}

func selectMachineInfo(result *inventory.ListResult, names []string) *inventory.ListResult {
	if result == nil {
		return nil
	}
	selected := *result
	selected.Machines = make([]inventory.MachineRow, 0, len(result.Machines))
	for _, row := range result.Machines {
		if names != nil && !slices.Contains(names, row.Name) {
			continue
		}
		row.IPs = slices.Clone(row.IPs)
		row.Clusters = slices.Clone(row.Clusters)
		selected.Machines = append(selected.Machines, row)
	}
	return &selected
}

func selectSecretInfo(result *custody.ListResult, names []string) *custody.ListResult {
	if result == nil {
		return nil
	}
	selected := *result
	selected.Secrets = make([]custody.ListRow, 0, len(result.Secrets))
	for _, row := range result.Secrets {
		if names != nil && !slices.Contains(names, row.Name) {
			continue
		}
		row.Parts = slices.Clone(row.Parts)
		if row.CurrentVersion != nil {
			version := *row.CurrentVersion
			row.CurrentVersion = &version
		}
		selected.Secrets = append(selected.Secrets, row)
	}
	return &selected
}

func refuseUnknownInfo(kind, contextName string, names, present []string) error {
	var unknown []string
	for _, name := range names {
		if !slices.Contains(present, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	subject := "no " + kind + " named "
	if len(unknown) > 1 {
		subject = "no " + kind + "s named "
	}
	return diagnostics.NewFailureWithRemediation("access.target",
		"the context "+contextName+" selects "+subject+strings.Join(unknown, ", "), "",
		"name a "+kind+" the context selects; bootwright "+strings.ToLower(kind)+" list --context "+contextName+" lists them")
}

func machineInfoNames(result *inventory.ListResult) []string {
	names := make([]string, 0, len(result.Machines))
	for _, row := range result.Machines {
		names = append(names, row.Name)
	}
	return names
}

func secretInfoNames(result *custody.ListResult) []string {
	names := make([]string, 0, len(result.Secrets))
	for _, row := range result.Secrets {
		names = append(names, row.Name)
	}
	return names
}
