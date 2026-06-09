package cli

import (
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

const (
	cephAdminConfigPath  = "/etc/ceph/ceph.conf"
	cephAdminKeyringPath = "/etc/ceph/ceph.client.admin.keyring"
	cephDashboardPort    = "8443"
)

// storageAccessSummary is the local, state-derived access surface for a Ceph
// StorageCluster. Unlike a container cluster, Bootwright keeps no admin
// credential file on the controller: the admin keyring and ceph.conf live on the
// seed node, so access is by SSH to that node plus `cephadm shell`. Every field
// is derived from desired state, so the summary is available without reading any
// remote artifact.
type storageAccessSummary struct {
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	Management       string   `json:"management"`
	SeedNode         string   `json:"seedNode,omitempty"`
	SeedAddress      string   `json:"seedAddress,omitempty"`
	SSHCommand       string   `json:"sshCommand,omitempty"`
	MonitorEndpoints []string `json:"monitorEndpoints,omitempty"`
	HealthCommand    string   `json:"healthCommand,omitempty"`
	ShellCommand     string   `json:"shellCommand,omitempty"`
	DashboardURL     string   `json:"dashboardURL,omitempty"`
	ConfigPath       string   `json:"configPath"`
	KeyringPath      string   `json:"keyringPath"`
}

func storageAccessSummaries(state v1alpha1.State) []storageAccessSummary {
	out := make([]storageAccessSummary, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		out = append(out, storageAccessSummaryFor(state, cluster))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func storageAccessSummaryFor(state v1alpha1.State, cluster v1alpha1.StorageCluster) storageAccessSummary {
	management := cluster.Spec.Management
	if management == "" {
		management = v1alpha1.StorageClusterManagementManaged
	}
	summary := storageAccessSummary{
		Name:             cluster.Metadata.Name,
		Type:             cluster.Spec.Type,
		Management:       management,
		SeedNode:         cluster.Spec.Ceph.Cephadm.Bootstrap.SeedNode,
		MonitorEndpoints: topology.MonitorEndpoints(state, cluster),
		ConfigPath:       cephAdminConfigPath,
		KeyringPath:      cephAdminKeyringPath,
	}
	if summary.SeedNode != "" {
		summary.SeedAddress = topology.NodeAddress(state, cluster, summary.SeedNode)
	}
	if summary.SeedAddress != "" {
		summary.SSHCommand = "ssh " + storageSeedSSHTarget(state, cluster, summary.SeedNode, summary.SeedAddress)
		summary.HealthCommand = summary.SSHCommand + " sudo cephadm shell -- ceph -s"
		summary.ShellCommand = summary.SSHCommand + " sudo cephadm shell"
		summary.DashboardURL = "https://" + summary.SeedAddress + ":" + cephDashboardPort
	}
	return summary
}

func storageSeedSSHTarget(state v1alpha1.State, cluster v1alpha1.StorageCluster, node, address string) string {
	if machine, ok := topology.NodeMachine(state, cluster, node); ok && machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.User != "" {
		return machine.Spec.Access.SSH.User + "@" + address
	}
	return address
}

// storageAccessSummariesForApply mirrors clusterAccessSummariesForApply: it
// reports access only for storage clusters whose install task actually ran to
// completion in this apply, so a skipped or failed cluster is not advertised as
// reachable.
func storageAccessSummariesForApply(state v1alpha1.State, ledger workflow.RunLedger) []storageAccessSummary {
	if ledger.Status != workflow.RunStatusOK {
		return nil
	}
	succeeded := map[string]bool{}
	for _, task := range ledger.Tasks {
		if task.Kind == workflow.ApplyTaskKindStorageCluster && task.Status == workflow.TaskStatusOK && task.Cluster != "" {
			succeeded[task.Cluster] = true
		}
	}
	if len(succeeded) == 0 {
		return nil
	}
	out := make([]storageAccessSummary, 0, len(succeeded))
	for _, summary := range storageAccessSummaries(state) {
		if succeeded[summary.Name] {
			out = append(out, summary)
		}
	}
	return out
}

func filterStorageAccessSummaries(summaries []storageAccessSummary, name string) []storageAccessSummary {
	if name == "" {
		return summaries
	}
	for _, summary := range summaries {
		if summary.Name == name {
			return []storageAccessSummary{summary}
		}
	}
	return nil
}

// printStorageAccessSections renders one block per storage cluster into an
// existing printer, so it composes with both the standalone access-info command
// and the post-apply continuation.
func printStorageAccessSections(p *cliout.Printer, summaries []storageAccessSummary) {
	for _, summary := range summaries {
		p.Section("Storage cluster " + summary.Name)
		p.Fields(storageAccessFields(summary))
		p.Status(cliout.StatusInfo, "health", "run the health check to confirm Ceph reports HEALTH_OK")
	}
}

func storageAccessFields(summary storageAccessSummary) []cliout.Field {
	fields := []cliout.Field{{Key: "Type", Value: storageAccessTypeDetail(summary)}}
	if summary.SeedNode != "" {
		fields = append(fields, cliout.Field{Key: "Seed node", Value: summary.SeedNode})
	}
	if summary.SSHCommand != "" {
		fields = append(fields, cliout.Field{Key: "SSH", Value: summary.SSHCommand})
	}
	if len(summary.MonitorEndpoints) > 0 {
		fields = append(fields, cliout.Field{Key: "Monitors", Value: strings.Join(summary.MonitorEndpoints, ", ")})
	}
	if summary.HealthCommand != "" {
		fields = append(fields, cliout.Field{Key: "Health check", Value: summary.HealthCommand})
	}
	if summary.ShellCommand != "" {
		fields = append(fields, cliout.Field{Key: "Cluster shell", Value: summary.ShellCommand})
	}
	if summary.DashboardURL != "" {
		fields = append(fields, cliout.Field{Key: "Dashboard", Value: summary.DashboardURL})
	}
	return append(fields,
		cliout.Field{Key: "ceph.conf", Value: storageAccessNodePath(summary.ConfigPath, summary.SeedNode)},
		cliout.Field{Key: "Admin keyring", Value: storageAccessNodePath(summary.KeyringPath, summary.SeedNode)},
	)
}

func storageAccessTypeDetail(summary storageAccessSummary) string {
	if summary.Management == "" {
		return summary.Type
	}
	return summary.Type + " (" + summary.Management + ")"
}

func storageAccessNodePath(path, node string) string {
	if node == "" {
		return path
	}
	return path + " (on " + node + ")"
}
