package cli

import (
	"path/filepath"
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
	// cephDashboardUser is cephadm's default initial dashboard user; Bootwright
	// does not override it at bootstrap, so it is always "admin".
	cephDashboardUser = "admin"
	// cephDashboardPasswordFile is the controller-side secrets filename the
	// storage_cluster_cephadm role writes the captured dashboard password to,
	// alongside the container clusters' kubeadmin-password.
	cephDashboardPasswordFile = "dashboard-password"
)

// storageDashboardPasswordPath returns the controller-local path where the
// install-time Ceph dashboard admin password is persisted for a storage cluster,
// mirroring <clustersDir>/<name>/secrets/kubeadmin-password for container
// clusters. It returns "" when the clusters dir is unknown.
func storageDashboardPasswordPath(clustersDir, clusterName string) string {
	if clustersDir == "" {
		return ""
	}
	return filepath.Join(clustersDir, clusterName, "secrets", cephDashboardPasswordFile)
}

// storageAccessSummary is the local, state-derived access surface for a Ceph
// StorageCluster. Unlike a container cluster, Bootwright keeps no admin
// credential file on the controller: the admin keyring and ceph.conf live on the
// seed node, so access is by SSH to that node plus `cephadm shell`. Every field
// is derived from desired state, so the summary is available without reading any
// remote artifact.
type storageAccessSummary struct {
	Name                     string                `json:"name"`
	Type                     string                `json:"type"`
	Management               string                `json:"management"`
	SeedHost                 string                `json:"seedHost,omitempty"`
	SeedAddress              string                `json:"seedAddress,omitempty"`
	SSHCommand               string                `json:"sshCommand,omitempty"`
	MonitorEndpoints         []string              `json:"monitorEndpoints,omitempty"`
	HealthCommand            string                `json:"healthCommand,omitempty"`
	ShellCommand             string                `json:"shellCommand,omitempty"`
	DashboardURL             string                `json:"dashboardURL,omitempty"`
	DashboardUser            string                `json:"dashboardUser,omitempty"`
	DashboardPasswordPath    string                `json:"dashboardPasswordPath,omitempty"`
	DashboardPasswordCommand string                `json:"dashboardPasswordCommand,omitempty"`
	DashboardPassword        clusterAccessArtifact `json:"dashboardPassword,omitempty"`
	ConfigPath               string                `json:"configPath"`
	KeyringPath              string                `json:"keyringPath"`
}

func storageAccessSummaries(state v1alpha1.State, clustersDir string) []storageAccessSummary {
	out := make([]storageAccessSummary, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		out = append(out, storageAccessSummaryFor(state, cluster, clustersDir))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func storageAccessSummaryFor(state v1alpha1.State, cluster v1alpha1.StorageCluster, clustersDir string) storageAccessSummary {
	management := cluster.Spec.Management
	if management == "" {
		management = v1alpha1.StorageClusterManagementManaged
	}
	summary := storageAccessSummary{
		Name:             cluster.Metadata.Name,
		Type:             cluster.Spec.Type,
		Management:       management,
		SeedHost:         cluster.Spec.Ceph.Cephadm.Bootstrap.Host,
		MonitorEndpoints: topology.MonitorEndpoints(state, cluster),
		ConfigPath:       cephAdminConfigPath,
		KeyringPath:      cephAdminKeyringPath,
	}
	if summary.SeedHost != "" {
		summary.SeedAddress = topology.NodeAddress(state, cluster, summary.SeedHost)
	}
	if summary.SeedAddress != "" {
		summary.SSHCommand = "ssh " + storageSeedSSHTarget(state, cluster, summary.SeedHost, summary.SeedAddress)
		summary.HealthCommand = summary.SSHCommand + " sudo cephadm shell -- ceph -s"
		summary.ShellCommand = summary.SSHCommand + " sudo cephadm shell"
		summary.DashboardURL = "https://" + summary.SeedAddress + ":" + cephDashboardPort
	}
	// Bootwright captures the dashboard admin password at install for managed Ceph
	// clusters and persists it like kubeadmin-password. The summary only reports
	// the file's location and presence — never its bytes (see clusterAccessFileStatus,
	// which stats the file without reading it).
	if management == v1alpha1.StorageClusterManagementManaged {
		if path := storageDashboardPasswordPath(clustersDir, cluster.Metadata.Name); path != "" {
			summary.DashboardUser = cephDashboardUser
			summary.DashboardPasswordPath = path
			summary.DashboardPasswordCommand = "sudo cat " + workflow.ShellQuote([]string{path})
			summary.DashboardPassword = clusterAccessFileStatus(path)
		}
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
func storageAccessSummariesForApply(state v1alpha1.State, ledger workflow.RunLedger, clustersDir string) []storageAccessSummary {
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
	for _, summary := range storageAccessSummaries(state, clustersDir) {
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
// existing printer, so it composes with both the standalone cluster access command
// and the post-apply continuation.
func printStorageAccessSections(p *cliout.Printer, summaries []storageAccessSummary) {
	for _, summary := range summaries {
		p.Section("Storage cluster " + summary.Name)
		p.Fields(storageAccessFields(summary))
		if summary.DashboardPasswordPath != "" {
			p.Status(accessArtifactStatus(summary.DashboardPassword), "dashboard password", accessArtifactDetail(summary.DashboardPassword))
		}
		p.Status(cliout.StatusInfo, "health", "run the health check to confirm Ceph reports HEALTH_OK")
	}
}

func storageAccessFields(summary storageAccessSummary) []cliout.Field {
	fields := []cliout.Field{{Key: "Type", Value: storageAccessTypeDetail(summary)}}
	if summary.SeedHost != "" {
		fields = append(fields, cliout.Field{Key: "Seed node", Value: summary.SeedHost})
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
	if summary.DashboardPasswordPath != "" {
		fields = append(fields,
			cliout.Field{Key: "Dashboard user", Value: summary.DashboardUser},
			cliout.Field{Key: "Dashboard password file", Value: summary.DashboardPasswordPath},
			cliout.Field{Key: "Show dashboard password", Value: summary.DashboardPasswordCommand},
		)
	}
	return append(fields,
		cliout.Field{Key: "ceph.conf", Value: storageAccessNodePath(summary.ConfigPath, summary.SeedHost)},
		cliout.Field{Key: "Admin keyring", Value: storageAccessNodePath(summary.KeyringPath, summary.SeedHost)},
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
