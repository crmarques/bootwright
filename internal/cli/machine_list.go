package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/ownership"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const (
	machineStateProvisioned    = "provisioned"
	machineStateNotProvisioned = "not-provisioned"
	machineStateExternal       = "external"
)

type machineListReport struct {
	Context  string             `json:"context"`
	Machines []machineListEntry `json:"machines"`
}

type machineListEntry struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Provisioned bool   `json:"provisioned"`
	OS          string `json:"os,omitempty"`
	Substrate   string `json:"substrate,omitempty"`
	Cluster     string `json:"cluster,omitempty"`
	ClusterKind string `json:"clusterKind,omitempty"`
	Role        string `json:"role,omitempty"`
	SSHUser     string `json:"sshUser,omitempty"`
	SSHAddress  string `json:"sshAddress,omitempty"`
}

func newMachineListCmd(stdout io.Writer) *cobra.Command {
	var (
		clusters string
		silent   bool
		format   string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List declared Machines with provisioning, OS, and cluster",
		Args:  cobra.NoArgs,
		Example: `  bootwright machine list
  bootwright machine list --clusters ceph-dc1,managed-01
  bootwright machine list --silent
  bootwright machine list --output json`,
	}
	cmd.Flags().StringVar(&clusters, "clusters", "", "comma-separated cluster names; list only their Machines (default: all)")
	registerClusterScopeCompletion(cmd, clusterKindAny)
	cmd.Flags().BoolVar(&silent, "silent", false, "print only Machine names, one per line")
	addOutputFlag(cmd, &format)
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(format); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		filter, err := resolveMachineClusterFilter(state, clusters)
		if err != nil {
			return failErr(2, err)
		}
		records, err := ownership.LoadContext(cf.ctx.OwnershipDir, cf.ctx.Name)
		if err != nil {
			return failErr(1, err)
		}
		entries := buildMachineList(state, records, filter)
		if silent {
			for _, entry := range entries {
				output.Write(stdout, entry.Name+"\n")
			}
			return nil
		}
		if format == outputJSON {
			return output.JSON(stdout, machineListReport{Context: cf.ctx.Name, Machines: entries})
		}
		printMachineList(stdout, entries)
		return nil
	}
	return cmd
}

func resolveMachineClusterFilter(state v1alpha1.State, clusters string) (map[string]bool, error) {
	if strings.TrimSpace(clusters) == "" {
		return nil, nil
	}
	requested, err := parseNameSet(clusters)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		known[cluster.Metadata.Name] = true
	}
	for _, cluster := range state.StorageClusters {
		known[cluster.Metadata.Name] = true
	}
	var missing []string
	for name := range requested {
		if !known[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		available := make([]string, 0, len(known))
		for name := range known {
			available = append(available, name)
		}
		sort.Strings(available)
		hint := "no clusters are declared"
		if len(available) > 0 {
			hint = "available: " + strings.Join(available, ", ")
		}
		return nil, fmt.Errorf("unknown cluster(s): %s; %s", strings.Join(missing, ", "), hint)
	}
	return requested, nil
}

func buildMachineList(state v1alpha1.State, records []ownership.ResourceRecord, filter map[string]bool) []machineListEntry {
	provisioned := provisionedMachineNames(records)
	entries := make([]machineListEntry, 0, len(state.Machines))
	for _, machine := range state.Machines {
		binding, bound := stateview.MachineCluster(state, machine.Metadata.Name)
		if filter != nil && (!bound || !filter[binding.Cluster]) {
			continue
		}
		entry := machineListEntry{
			Name:      machine.Metadata.Name,
			OS:        machineOSDescriptor(state, machine),
			Substrate: machineSubstrateType(state, machine),
		}
		if target, err := machineSSHTarget(state, machine.Metadata.Name); err == nil {
			entry.SSHUser, entry.SSHAddress = target.User, target.Address
		}
		if bound {
			entry.Cluster = binding.Cluster
			entry.ClusterKind = binding.Kind
			entry.Role = binding.Role
		}
		switch {
		case v1alpha1.MachineOSProvided(machine):
			entry.State = machineStateExternal
		case provisioned[machine.Metadata.Name]:
			entry.State = machineStateProvisioned
			entry.Provisioned = true
		default:
			entry.State = machineStateNotProvisioned
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func provisionedMachineNames(records []ownership.ResourceRecord) map[string]bool {
	out := map[string]bool{}
	for _, record := range records {
		if record.Machine != "" {
			out[record.Machine] = true
		}
	}
	return out
}

func machineOSDescriptor(state v1alpha1.State, machine v1alpha1.Machine) string {
	if v1alpha1.MachineOSProvided(machine) {
		return "provided"
	}
	profileName := machine.Spec.OS.InstallProfileRef.Name
	if profileName == "" {
		return ""
	}
	profile, ok := stateview.MachineInstallProfile(state, profileName)
	if !ok {
		return ""
	}
	os := profile.Spec.OS
	desc := strings.TrimSpace(os.Family + " " + os.Version)
	if os.Architecture != "" {
		if desc != "" {
			desc += " "
		}
		desc += "(" + os.Architecture + ")"
	}
	return desc
}

func machineSubstrateType(state v1alpha1.State, machine v1alpha1.Machine) string {
	if v1alpha1.MachineOSProvided(machine) {
		return ""
	}
	providerName := machine.Spec.Substrate.ProviderRef.Name
	if providerName == "" {
		return ""
	}
	if provider, ok := stateview.Provider(state, providerName); ok && provider.Spec.Type != "" {
		return provider.Spec.Type
	}
	return providerName
}

func printMachineList(stdout io.Writer, entries []machineListEntry) {
	p := output.New(stdout)
	p.Command("machine list")
	if len(entries) == 0 {
		p.Summary(output.StatusSkip, "machines", "none declared")
		return
	}
	p.Section("Machines")
	for _, entry := range entries {
		p.Status(machineListStatus(entry), entry.Name, machineListDetail(entry))
		if fields := machineListFields(entry); len(fields) > 0 {
			p.SubFields(fields)
		}
	}
}

func machineListStatus(entry machineListEntry) output.Status {
	switch entry.State {
	case machineStateProvisioned:
		return output.StatusOK
	case machineStateExternal:
		return output.StatusInfo
	default:
		return output.StatusPending
	}
}

func machineListDetail(entry machineListEntry) string {
	parts := []string{machineStateLabel(entry.State)}
	if entry.Substrate != "" {
		parts = append(parts, entry.Substrate)
	}
	if entry.OS != "" {
		parts = append(parts, entry.OS)
	}
	if entry.Cluster != "" {
		cluster := entry.Cluster
		if entry.Role != "" {
			cluster += " (" + entry.Role + ")"
		}
		parts = append(parts, cluster)
	}
	return strings.Join(parts, " · ")
}

func machineStateLabel(state string) string {
	switch state {
	case machineStateProvisioned:
		return "provisioned"
	case machineStateExternal:
		return "external OS"
	default:
		return "not provisioned"
	}
}

func machineListFields(entry machineListEntry) []output.Field {
	if entry.SSHAddress == "" {
		return nil
	}
	target := entry.SSHAddress
	if entry.SSHUser != "" {
		target = entry.SSHUser + "@" + entry.SSHAddress
	}
	return []output.Field{{Key: "SSH", Value: target}}
}
