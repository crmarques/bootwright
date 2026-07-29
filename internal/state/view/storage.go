package stateview

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func StorageClustersDomain(state v1alpha1.State) string {
	env := Environment(state)
	if env == nil {
		return ""
	}
	return strings.TrimSpace(env.Spec.Domains.StorageClustersDomain())
}

func StorageMgmtGatewayFQDN(state v1alpha1.State, cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.MgmtGateway == nil {
		return ""
	}
	domain := StorageClustersDomain(state)
	if domain == "" {
		return ""
	}
	label := cluster.Spec.Ceph.MgmtGateway.DNSLabel
	if label == "" {
		label = v1alpha1.StorageCephMgmtGatewayDefaultDNSLabel
	}
	return ComposeFQDN(label, cluster.Metadata.Name, domain)
}

func StorageGatewayFQDN(state v1alpha1.State, gateway v1alpha1.StorageObjectGateway) string {
	clusterName := gateway.Spec.StorageClusterRef.Name
	domain := StorageClustersDomain(state)
	if clusterName == "" || domain == "" {
		return ""
	}
	label := gateway.Spec.Public.DNSLabel
	if label == "" {
		label = gateway.Metadata.Name
	}
	return ComposeFQDN(label, clusterName, domain)
}

func ClusterByName(state v1alpha1.State, name string) (v1alpha1.StorageCluster, bool) {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.StorageCluster{}, false
}

func ExportByName(state v1alpha1.State, name string) (v1alpha1.StorageExport, bool) {
	for _, export := range state.StorageExports {
		if export.Metadata.Name == name {
			return export, true
		}
	}
	return v1alpha1.StorageExport{}, false
}

func FilesystemByName(state v1alpha1.State, name string) (v1alpha1.StorageFilesystem, bool) {
	for _, fs := range state.StorageFilesystems {
		if fs.Metadata.Name == name {
			return fs, true
		}
	}
	return v1alpha1.StorageFilesystem{}, false
}

func GatewayByName(state v1alpha1.State, name string) (v1alpha1.StorageObjectGateway, bool) {
	for _, gateway := range state.StorageObjectGateways {
		if gateway.Metadata.Name == name {
			return gateway, true
		}
	}
	return v1alpha1.StorageObjectGateway{}, false
}
