package storage

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPackageRepositoryTrustBundleRequiresAnAuthoredRepository(t *testing.T) {
	const field = "$.spec.ceph.packageRepositoryTrustBundleRef"
	refuses := func(name string, cluster api.Object, catalog api.Catalog) {
		t.Helper()
		bundled := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("ca"), "ceph", "packageRepositoryTrustBundleRef"))
		issues := issuesAt(Validate(bundled, replace(catalog, bundled)), field)
		if len(issues) != 1 || issues[0].Code != "api.invariant" ||
			issues[0].Message != "spec.ceph.packageRepositoryTrustBundleRef verifies only an authored spec.ceph.packageRepository, and this StorageCluster sets none" ||
			issues[0].Remediation != "set spec.ceph.packageRepository to the HTTPS repository this bundle verifies, or remove spec.ceph.packageRepositoryTrustBundleRef" {
			t.Fatalf("%s: %+v", name, issues)
		}
		authored := bundled.WithSpec(bundled.Spec().WithPath(api.StringValue("https://packages.example.test/ceph"), "ceph", "packageRepository"))
		if issues := issuesAt(Validate(authored, replace(catalog, authored)), field); len(issues) != 0 {
			t.Fatalf("%s with an authored repository: %+v", name, issues)
		}
	}
	ibm, catalog := vendorCluster(t)
	refuses("ibm", ibm, catalog)
	oss := fixtureCatalog()
	refuses("oss", find(t, oss, api.StorageCluster, "storage"), oss)
}
