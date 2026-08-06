package ceph

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cephxCluster(keyType string) v1alpha1.StorageCluster {
	cluster := v1alpha1.StorageCluster{
		Metadata: v1alpha1.Metadata{Name: "ceph-01"},
		Spec:     v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}},
	}
	if keyType != "" {
		cluster.Spec.Ceph.Security.Cephx = &v1alpha1.StorageCephCephx{KeyType: keyType}
	}
	return cluster
}

func cephxOpCommand(t *testing.T, ops []map[string]any, name string) []string {
	t.Helper()
	for _, op := range ops {
		if op["name"] != name {
			continue
		}
		return op["command"].([]string)
	}
	t.Fatalf("operation %s not found in %#v", name, ops)
	return nil
}

func TestCephxOperationsAreAbsentWhenNoKeyTypeIsDeclared(t *testing.T) {
	if ops := cephCephxOperations(cephxCluster("")); len(ops) != 0 {
		t.Fatalf("undeclared cephx key type emitted %#v; a cluster that declares nothing must keep the build's own policy", ops)
	}
}

func TestCephxAESWidensAllowedCiphersAndPrefersAES(t *testing.T) {
	ops := cephCephxOperations(cephxCluster(v1alpha1.StorageCephCephxKeyTypeAES))
	if len(ops) != 2 {
		t.Fatalf("expected two mon-map operations, got %#v", ops)
	}
	if got, want := cephxOpCommand(t, ops, "set-cephx-allowed-ciphers"), []string{"ceph", "mon", "set", "auth_allowed_ciphers", "aes,aes256k"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed ciphers = %v, want %v -- declaring aes must widen the set, never narrow it", got, want)
	}
	if got, want := cephxOpCommand(t, ops, "set-cephx-preferred-cipher"), []string{"ceph", "mon", "set", "auth_preferred_cipher", "aes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preferred cipher = %v, want %v", got, want)
	}
	if ops[0]["name"] != "set-cephx-allowed-ciphers" {
		t.Fatalf("allowed ciphers must be widened before a cipher is preferred; order was %v, %v", ops[0]["name"], ops[1]["name"])
	}
}

func TestCephxAES256KRestoresTheVendorDefault(t *testing.T) {
	ops := cephCephxOperations(cephxCluster(v1alpha1.StorageCephCephxKeyTypeAES256K))
	if got, want := cephxOpCommand(t, ops, "set-cephx-allowed-ciphers"), []string{"ceph", "mon", "set", "auth_allowed_ciphers", "aes256k"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed ciphers = %v, want %v", got, want)
	}
	if got, want := cephxOpCommand(t, ops, "set-cephx-preferred-cipher"), []string{"ceph", "mon", "set", "auth_preferred_cipher", "aes256k"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preferred cipher = %v, want %v", got, want)
	}
}

func TestCephxOperationsNeverTouchTheServiceCipher(t *testing.T) {
	for _, keyType := range v1alpha1.StorageCephCephxKeyTypes() {
		for _, op := range cephCephxOperations(cephxCluster(keyType)) {
			for _, arg := range op["command"].([]string) {
				if arg == "auth_service_cipher" {
					t.Fatalf("keyType %q emitted auth_service_cipher; rotating the mons' own service cipher invalidates every client's service key", keyType)
				}
			}
		}
	}
}

func TestCephxOperationsToleratesANonCephStorageCluster(t *testing.T) {
	if ops := cephCephxOperations(v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "no-ceph"}}); len(ops) != 0 {
		t.Fatalf("a StorageCluster with no ceph block emitted %#v", ops)
	}
}

func TestCephxKeyPrefixMatchesTheWireFormat(t *testing.T) {
	if got := v1alpha1.StorageCephCephxKeyPrefix(v1alpha1.StorageCephCephxKeyTypeAES); got != "AQ" {
		t.Fatalf("aes prefix = %q, want AQ", got)
	}
	if got := v1alpha1.StorageCephCephxKeyPrefix(v1alpha1.StorageCephCephxKeyTypeAES256K); got != "Ag" {
		t.Fatalf("aes256k prefix = %q, want Ag", got)
	}
}
