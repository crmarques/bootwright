package v1alpha1

import (
	"slices"
	"testing"
)

// The kinds that exist only within one parent are read from the registry, so
// every storage resource kind is a StorageCluster child and the add-on binding
// a ContainerCluster child without a list naming them.
func TestChildrenAreReadFromTheSchemaRegistry(t *testing.T) {
	storage := []Kind{StoragePlacementPolicy, StoragePool, StorageRBDImage, StorageClient, StorageFilesystem, StorageSubvolume, StorageObjectGateway,
		StorageObjectUser, StorageObjectBucket, StorageNFSExport, StorageSMBResource, StorageNVMeoFSubsystem, StorageReplication, StorageExport}
	if got := Children(StorageCluster); !slices.Equal(got, storage) {
		t.Errorf("StorageCluster children = %v", got)
	}
	if got := Children(ContainerCluster); !slices.Equal(got, []Kind{ClusterAddonBinding}) {
		t.Errorf("ContainerCluster children = %v", got)
	}
	if got := Children(Machine); len(got) != 0 {
		t.Errorf("Machine children = %v", got)
	}
}
