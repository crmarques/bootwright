package v1alpha1

import (
	"slices"
	"testing"
)

func TestPackageRepositoryTrustBundleMustBeACABundleSecret(t *testing.T) {
	ceph, _ := Schema(StorageCluster).Field("ceph")
	index := slices.IndexFunc(ceph.Shape.Fields, func(f Field) bool { return f.Name == "packageRepositoryTrustBundleRef" })
	if index < 1 || ceph.Shape.Fields[index-1].Name != "packageRepository" {
		t.Fatal("ceph.packageRepositoryTrustBundleRef must follow ceph.packageRepository")
	}
	field := ceph.Shape.Fields[index]
	if field.Required || field.Default.Present() {
		t.Fatal("ceph.packageRepositoryTrustBundleRef must be optional without a default")
	}
	if !slices.Equal(field.Shape.Reference, []Kind{Secret}) || !slices.Equal(field.Shape.SecretTypes, []string{"caBundle"}) {
		t.Fatalf("ceph.packageRepositoryTrustBundleRef must reference only a caBundle Secret: %v %v", field.Shape.Reference, field.Shape.SecretTypes)
	}
}
