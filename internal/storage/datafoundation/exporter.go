package datafoundation

import (
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ExternalDetailsExporterArgs builds the argv for Rook's
// ceph-external-cluster-details-exporter.py from a StorageExport's exporter
// config. This is a render-time tool input: it owns the exporter flag vocabulary
// for the Data Foundation external-mode attachment, alongside the rest of the
// Data Foundation external-details domain, rather than living in the apply
// scheduler that merely runs it.
func ExternalDetailsExporterArgs(config v1alpha1.StorageExportExternalDetailsExporterConfig, containerCluster string) []string {
	format := strings.TrimSpace(config.Format)
	if format == "" {
		format = "json"
	}
	k8sClusterName := strings.TrimSpace(config.K8sClusterName)
	if k8sClusterName == "" {
		k8sClusterName = containerCluster
	}
	args := []string{"python3", "ceph-external-cluster-details-exporter.py", "--format", format}
	appendValue := func(flag, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, flag, value)
		}
	}
	appendValue("--rbd-data-pool-name", config.RBDDataPoolName)
	appendValue("--rados-namespace", config.RadosNamespace)
	appendValue("--rbd-metadata-ec-pool-name", config.RBDMetadataECPoolName)
	appendValue("--cephfs-filesystem-name", config.CephFSFilesystemName)
	appendValue("--cephfs-data-pool-name", config.CephFSDataPoolName)
	appendValue("--cephfs-metadata-pool-name", config.CephFSMetadataPoolName)
	appendValue("--rgw-endpoint", config.RGWEndpoint)
	appendValue("--rgw-pool-prefix", config.RGWPoolPrefix)
	if len(config.MonitoringEndpoint) > 0 {
		args = append(args, "--monitoring-endpoint", strings.Join(config.MonitoringEndpoint, ","))
	}
	if config.MonitoringEndpointPort > 0 {
		args = append(args, "--monitoring-endpoint-port", strconv.Itoa(config.MonitoringEndpointPort))
	}
	appendValue("--cluster-name", config.ClusterName)
	appendValue("--k8s-cluster-name", k8sClusterName)
	if config.RestrictedAuthPermission {
		args = append(args, "--restricted-auth-permission", "true")
	}
	return args
}
