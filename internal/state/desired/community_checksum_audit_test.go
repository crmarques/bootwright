package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ossCommunityChecksumCluster(checksum string) v1alpha1.StorageCluster {
	return v1alpha1.StorageCluster{
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: v1alpha1.StorageCephDistributionOSS,
				Release:      "tentacle",
				Community: &v1alpha1.StorageCephCommunitySpec{
					Checksum: checksum,
				},
			},
		},
	}
}

func TestValidateStorageCephCommunityChecksumAccepted(t *testing.T) {
	const prefix = "StorageCluster/x spec.ceph.community"
	cases := map[string]string{
		"sha256-prefixed hex": "sha256:" + strings.Repeat("a", 64),
		"bare hex":            strings.Repeat("b", 64),
		"unset (absent)":      "",
	}
	for name, sum := range cases {
		if errs := validateStorageCephCommunity(prefix, ossCommunityChecksumCluster(sum)); len(errs) != 0 {
			t.Fatalf("%s: well-formed checksum rejected: %v", name, errs)
		}
	}
}

func TestValidateStorageCephCommunityChecksumRejectsMalformed(t *testing.T) {
	const prefix = "StorageCluster/x spec.ceph.community"
	for name, sum := range map[string]string{
		"non-hex":   "sha256:zzzz",
		"too short": "sha256:" + strings.Repeat("a", 63),
	} {
		errs := validateStorageCephCommunity(prefix, ossCommunityChecksumCluster(sum))
		if len(errs) == 0 {
			t.Fatalf("%s: malformed checksum accepted", name)
		}
		if joined := strings.Join(errs, "; "); !strings.Contains(joined, ".checksum") {
			t.Fatalf("%s: error missing checksum context: %v", name, errs)
		}
	}
}
