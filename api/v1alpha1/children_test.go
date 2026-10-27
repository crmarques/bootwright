package v1alpha1

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// The kinds that exist only within one parent are read from the registry, so
// every storage resource kind is a CephCluster child and the add-on binding
// an OpenShiftCluster child without a list naming them.
func TestChildrenAreReadFromTheSchemaRegistry(t *testing.T) {
	storage := []Kind{CephPlacementPolicy, CephPool, CephRBDImage, CephClient, CephFilesystem, CephSubvolume, CephObjectGateway,
		CephObjectUser, CephObjectBucket, CephNFSExport, CephSMBResource, CephNVMeoFSubsystem, CephReplication, CephConsumer}
	if got := Children(CephCluster); !slices.Equal(got, storage) {
		t.Errorf("CephCluster children = %v", got)
	}
	if got := Children(OpenShiftCluster); !slices.Equal(got, []Kind{ClusterAddonBinding}) {
		t.Errorf("OpenShiftCluster children = %v", got)
	}
	if got := Children(Machine); len(got) != 0 {
		t.Errorf("Machine children = %v", got)
	}
}

// F05-02: the Kinds table of the Ceph API page lists exactly the storage
// family's kinds, so a kind added to or retired from the family moves the page.
func TestCephKindsTableNamesEveryStorageFamilyKind(t *testing.T) {
	data, err := os.ReadFile("../../specs/api/ceph.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n## Kinds\n")
	if !ok {
		t.Fatal("specs/api/ceph.md has no Kinds section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var listed []Kind
	for _, line := range strings.Split(section, "\n") {
		if name, found := strings.CutPrefix(line, "| `"); found {
			name, _, _ = strings.Cut(name, "`")
			listed = append(listed, Kind(name))
		}
	}
	var family []Kind
	for _, kind := range FamilyKinds() {
		if category, _ := CategoryOf(kind); category == StorageClusterCategory || slices.Contains(Children(CephCluster), kind) {
			family = append(family, kind)
		}
	}
	slices.Sort(listed)
	slices.Sort(family)
	if !slices.Equal(listed, family) {
		t.Errorf("specs/api/ceph.md Kinds table lists %v; the storage family registers %v", listed, family)
	}
}
