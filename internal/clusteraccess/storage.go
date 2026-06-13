package clusteraccess

import (
	"path/filepath"
	"sort"
	"strconv"

	"github.com/crmarques/bootwright/api/v1alpha1"
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

// StorageDashboardPasswordPath returns the controller-local path where the
// install-time Ceph dashboard admin password is persisted for a storage cluster,
// mirroring <clustersDir>/<name>/secrets/kubeadmin-password for container
// clusters. It returns "" when the clusters dir is unknown.
func StorageDashboardPasswordPath(clustersDir, clusterName string) string {
	if clustersDir == "" {
		return ""
	}
	return filepath.Join(clustersDir, clusterName, "secrets", cephDashboardPasswordFile)
}

// StorageSummary is the local, state-derived access surface for a Ceph
// StorageCluster. Unlike a container cluster, Bootwright keeps no admin
// credential file on the controller: the admin keyring and ceph.conf live on the
// seed node, so access is by SSH to that node plus `cephadm shell`. Every field
// is derived from desired state, so the summary is available without reading any
// remote artifact.
type StorageSummary struct {
	Name                     string   `json:"name"`
	Type                     string   `json:"type"`
	Management               string   `json:"management"`
	SeedHost                 string   `json:"seedHost,omitempty"`
	SeedAddress              string   `json:"seedAddress,omitempty"`
	SSHCommand               string   `json:"sshCommand,omitempty"`
	MonitorEndpoints         []string `json:"monitorEndpoints,omitempty"`
	HealthCommand            string   `json:"healthCommand,omitempty"`
	ShellCommand             string   `json:"shellCommand,omitempty"`
	DashboardURL             string   `json:"dashboardURL,omitempty"`
	DashboardUser            string   `json:"dashboardUser,omitempty"`
	DashboardPasswordPath    string   `json:"dashboardPasswordPath,omitempty"`
	DashboardPasswordCommand string   `json:"dashboardPasswordCommand,omitempty"`
	DashboardPassword        Artifact `json:"dashboardPassword,omitempty"`
	ConfigPath               string   `json:"configPath"`
	KeyringPath              string   `json:"keyringPath"`
}

func StorageSummaries(state v1alpha1.State, clustersDir string) []StorageSummary {
	out := make([]StorageSummary, 0, len(state.StorageClusters))
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		out = append(out, storageSummaryFor(state, cluster, clustersDir))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func storageSummaryFor(state v1alpha1.State, cluster v1alpha1.StorageCluster, clustersDir string) StorageSummary {
	management := cluster.Spec.Management
	if management == "" {
		management = v1alpha1.StorageClusterManagementManaged
	}
	summary := StorageSummary{
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
	// A configured management VIP supersedes the per-node dashboard address: the
	// mgmt-gateway VIP is the HA, user-facing entry, resolvable at its FQDN and
	// independent of which mgr is active, so it does not depend on the seed node.
	if mgmt := cluster.Spec.Ceph.Management; mgmt != nil && mgmt.DNSName != "" {
		summary.DashboardURL = "https://" + mgmt.DNSName + ":" + cephManagementPort(mgmt.Port)
	}
	// Bootwright captures the dashboard admin password at install for managed Ceph
	// clusters and persists it like kubeadmin-password. The summary only reports
	// the file's location and presence — never its bytes (see FileStatus,
	// which stats the file without reading it).
	if management == v1alpha1.StorageClusterManagementManaged {
		if path := StorageDashboardPasswordPath(clustersDir, cluster.Metadata.Name); path != "" {
			summary.DashboardUser = cephDashboardUser
			summary.DashboardPasswordPath = path
			summary.DashboardPasswordCommand = "sudo cat " + workflow.ShellQuote([]string{path})
			summary.DashboardPassword = FileStatus(path)
		}
	}
	return summary
}

// cephManagementPort renders the management dashboard port, defaulting to the
// Ceph dashboard's own port when spec.ceph.management.port is unset (the same
// default the renderer applies to the mgmt-gateway frontend).
func cephManagementPort(port int) string {
	if port == 0 {
		return cephDashboardPort
	}
	return strconv.Itoa(port)
}

func storageSeedSSHTarget(state v1alpha1.State, cluster v1alpha1.StorageCluster, node, address string) string {
	if machine, ok := topology.NodeMachine(state, cluster, node); ok && machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.User != "" {
		return machine.Spec.Access.SSH.User + "@" + address
	}
	return address
}

// StorageSummariesForApply mirrors ClusterSummariesForApply: it
// reports access only for storage clusters whose install task actually ran to
// completion in this apply, so a skipped or failed cluster is not advertised as
// reachable.
func StorageSummariesForApply(state v1alpha1.State, ledger workflow.RunLedger, clustersDir string) []StorageSummary {
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
	out := make([]StorageSummary, 0, len(succeeded))
	for _, summary := range StorageSummaries(state, clustersDir) {
		if succeeded[summary.Name] {
			out = append(out, summary)
		}
	}
	return out
}

func FilterStorageSummaries(summaries []StorageSummary, name string) []StorageSummary {
	if name == "" {
		return summaries
	}
	for _, summary := range summaries {
		if summary.Name == name {
			return []StorageSummary{summary}
		}
	}
	return nil
}
