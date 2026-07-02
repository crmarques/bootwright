package cephprovider

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ossChecksumCluster is an oss-distribution StorageCluster carrying the given
// spec.ceph.community.checksum.
func ossChecksumCluster(checksum string) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: v1alpha1.StorageCephDistributionOSS,
				Community: &v1alpha1.StorageCephCommunitySpec{
					Checksum: checksum,
				},
			},
		},
	}
}

func TestSelectOSSProviderProjectsCommunityChecksum(t *testing.T) {
	sum := strings.Repeat("a", 64)
	provider := Select(ossChecksumCluster("sha256:"+sum), nil, "/context/secrets")
	// The provider normalizes the operator value to bare sha256 hex (the shape
	// the community role re-prefixes into get_url's checksum).
	if provider.Community.Checksum != sum {
		t.Fatalf("community checksum = %q, want normalized %q", provider.Community.Checksum, sum)
	}
	community := Vars(provider)["community"].(map[string]any)
	if community["checksum"] != sum {
		t.Fatalf("community vars checksum = %v, want %q", community["checksum"], sum)
	}
}

func TestSelectOSSProviderOmitsUnsetCommunityChecksum(t *testing.T) {
	// Absent checksum keeps today's behavior: no checksum var reaches the role.
	provider := Select(ossChecksumCluster(""), nil, "/context/secrets")
	if provider.Community.Checksum != "" {
		t.Fatalf("unset checksum projected: %q", provider.Community.Checksum)
	}
	community := Vars(provider)["community"].(map[string]any)
	if _, ok := community["checksum"]; ok {
		t.Fatalf("community vars must omit checksum when unset: %#v", community)
	}

	// A community block with only a mirror also omits checksum.
	noChecksum := v1alpha1.StorageCluster{
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: v1alpha1.StorageCephDistributionOSS,
				Community:    &v1alpha1.StorageCephCommunitySpec{Mirror: "https://mirror.example.test/ceph"},
			},
		},
	}
	community = Vars(Select(noChecksum, nil, "/context/secrets"))["community"].(map[string]any)
	if _, ok := community["checksum"]; ok {
		t.Fatalf("community vars must omit checksum when only mirror is set: %#v", community)
	}
}
