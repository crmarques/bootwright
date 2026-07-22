package clusteraccess

import (
	"path/filepath"
	"sort"
	"strconv"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/shellquote"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

const cephDashboardPasswordFile = "dashboard-password"

var cephDashboardPort = strconv.Itoa(topology.CephManagementDefaultPort)

func StorageDashboardPasswordPath(clustersDir, clusterName string) string {
	if clustersDir == "" {
		return ""
	}
	return filepath.Join(clustersDir, clusterName, "secrets", cephDashboardPasswordFile)
}

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
		SeedHost:         cluster.Spec.Ceph.Cephadm.Bootstrap.Node,
		MonitorEndpoints: topology.MonitorEndpoints(state, cluster),
		ConfigPath:       topology.CephAdminConfigPath,
		KeyringPath:      topology.CephAdminKeyringPath,
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
	if mgmt := cluster.Spec.Ceph.Management; mgmt != nil && mgmt.DNSName != "" {
		summary.DashboardURL = "https://" + mgmt.DNSName + ":" + cephManagementPort(mgmt.Port)
	}
	if management == v1alpha1.StorageClusterManagementManaged {
		if path := StorageDashboardPasswordPath(clustersDir, cluster.Metadata.Name); path != "" {
			summary.DashboardUser = topology.CephDashboardDefaultUser
			summary.DashboardPasswordPath = path
			summary.DashboardPasswordCommand = "sudo cat " + shellquote.Quote([]string{path})
			summary.DashboardPassword = FileStatus(path)
		}
	}
	return summary
}

func cephManagementPort(port int) string {
	if port == 0 {
		return cephDashboardPort
	}
	return strconv.Itoa(port)
}

func storageSeedSSHTarget(state v1alpha1.State, cluster v1alpha1.StorageCluster, node, address string) string {
	if v1alpha1.StorageClusterManagesNodeAccount(cluster) {
		return v1alpha1.StorageClusterCephadmSSHUser(cluster) + "@" + address
	}
	if machine, ok := topology.NodeMachine(state, cluster, node); ok && machine.Spec.Access.SSH != nil && machine.Spec.Access.SSH.User != "" {
		return machine.Spec.Access.SSH.User + "@" + address
	}
	return address
}

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
