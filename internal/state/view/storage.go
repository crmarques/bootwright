package stateview

import "github.com/crmarques/bootwright/api/v1alpha1"

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
