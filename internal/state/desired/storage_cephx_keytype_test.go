package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestStorageCephCephxKeyTypeVocabulary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cephx *v1alpha1.StorageCephCephx
		want  string
	}{
		{name: "absent-block-is-allowed"},
		{name: "aes", cephx: &v1alpha1.StorageCephCephx{KeyType: v1alpha1.StorageCephCephxKeyTypeAES}},
		{name: "aes256k", cephx: &v1alpha1.StorageCephCephx{KeyType: v1alpha1.StorageCephCephxKeyTypeAES256K}},
		{name: "empty-key-type", cephx: &v1alpha1.StorageCephCephx{}, want: "keyType is required when the cephx block is present"},
		{name: "unknown-key-type", cephx: &v1alpha1.StorageCephCephx{KeyType: "aes512"}, want: `keyType "aes512" must be one of`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateStorageCephCephx("StorageCluster/ceph spec.ceph.security.cephx", tc.cephx)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("expected an error containing %q", tc.want)
			}
			if !strings.Contains(errs[0], tc.want) {
				t.Fatalf("error %q does not contain %q", errs[0], tc.want)
			}
		})
	}
}

func TestStorageClusterCephxKeyTypeReadsThroughOptionalBlocks(t *testing.T) {
	if got := v1alpha1.StorageClusterCephxKeyType(v1alpha1.StorageCluster{}); got != "" {
		t.Fatalf("a StorageCluster with no ceph block reported keyType %q", got)
	}
	noCephx := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}}}
	if got := v1alpha1.StorageClusterCephxKeyType(noCephx); got != "" {
		t.Fatalf("a ceph cluster with no cephx block reported keyType %q", got)
	}
	declared := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Security: v1alpha1.StorageCephSecurity{Cephx: &v1alpha1.StorageCephCephx{KeyType: v1alpha1.StorageCephCephxKeyTypeAES}},
	}}}
	if got := v1alpha1.StorageClusterCephxKeyType(declared); got != v1alpha1.StorageCephCephxKeyTypeAES {
		t.Fatalf("declared keyType = %q, want aes", got)
	}
}
