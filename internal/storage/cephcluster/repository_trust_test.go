package cephcluster

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestPackageRepositoryTrustBundleRequiresAnAuthoredRepository(t *testing.T) {
	const field = "$.spec.packageRepositoryTrustBundleRef"
	refuses := func(name string, cluster api.Object, catalog api.Catalog) {
		t.Helper()
		bundled := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("ca"), "packageRepositoryTrustBundleRef"))
		issues := issuesAt(Validate(bundled, replace(catalog, bundled)), field)
		if len(issues) != 1 || issues[0].Code != "api.invariant" ||
			issues[0].Message != "spec.packageRepositoryTrustBundleRef verifies only an authored spec.packageRepository, and this CephCluster sets none" ||
			issues[0].Remediation != "set spec.packageRepository to the HTTPS repository this bundle verifies, or remove spec.packageRepositoryTrustBundleRef" {
			t.Fatalf("%s: %+v", name, issues)
		}
		authored := bundled.WithSpec(bundled.Spec().WithPath(api.StringValue("https://packages.example.test/ceph"), "packageRepository").WithPath(api.StringValue("https://packages.example.test/keys/ibm.asc"), "packageRepositoryKey"))
		if issues := issuesAt(Validate(authored, replace(catalog, authored)), field); len(issues) != 0 {
			t.Fatalf("%s with an authored repository: %+v", name, issues)
		}
	}
	ibm, catalog := vendorCluster(t)
	refuses("ibm", ibm, catalog)
	oss := fixtureCatalog()
	refuses("oss", find(t, oss, api.CephCluster, "storage"), oss)
}

func TestPackageRepositoryKeyIsRequiredAndSharesRepositoryOrigin(t *testing.T) {
	cluster, catalog := vendorCluster(t)
	for _, tc := range []struct {
		name, key string
		refused   bool
	}{
		{"missing", "", true},
		{"mirror", "https://packages.example.test/keys/ibm.asc", false},
		{"explicit default port", "https://packages.example.test:443/ibm.asc", false},
		{"foreign host", "https://public.example.test/ibm.asc", true},
		{"foreign port", "https://packages.example.test:444/ibm.asc", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := cluster.Spec().WithPath(api.StringValue("https://packages.example.test/ceph"), "packageRepository")
			if tc.key != "" {
				spec = spec.WithPath(api.StringValue(tc.key), "packageRepositoryKey")
			}
			authored := cluster.WithSpec(spec)
			issues := issuesAt(Validate(authored, replace(catalog, authored)), "$.spec.packageRepositoryKey")
			if (len(issues) > 0) != tc.refused {
				t.Fatalf("issues: %+v", issues)
			}
		})
	}
}

func TestPackageRepositoryKeyWithoutRepositoryRefuses(t *testing.T) {
	cluster, catalog := vendorCluster(t)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("https://packages.example.test/keys/ibm.asc"), "packageRepositoryKey"))
	issues := issuesAt(Validate(cluster, replace(catalog, cluster)), "$.spec.packageRepositoryKey")
	if len(issues) != 1 || issues[0].Code != "api.invariant" {
		t.Fatalf("issues: %+v", issues)
	}
}
