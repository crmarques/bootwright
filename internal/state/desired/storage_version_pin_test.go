package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func versionPinCluster(distribution, release, packageVersion, base, version string) v1alpha1.StorageCluster {
	ceph := &v1alpha1.StorageClusterCephSpec{
		Distribution:   distribution,
		Release:        release,
		PackageVersion: packageVersion,
	}
	if base != "" || version != "" {
		ceph.Image = &v1alpha1.StorageCephImageSpec{Base: base, Version: version}
	}
	return v1alpha1.StorageCluster{Metadata: v1alpha1.Metadata{Name: "ceph"}, Spec: v1alpha1.StorageClusterSpec{Ceph: ceph}}
}

func withCephadmAnsiblePackageVersion(cluster v1alpha1.StorageCluster, version string) v1alpha1.StorageCluster {
	cluster.Spec.Ceph.Cephadm.Ansible = &v1alpha1.StorageCephadmAnsible{PackageVersion: version}
	return cluster
}

func TestValidateStorageCephImageVersionSyntax(t *testing.T) {
	cases := []struct {
		version   string
		wantError bool
	}{
		{"9.9.1.0-123", false},
		{"9-1234", false},
		{"v19.2.1", false},
		{"19.2.1-245.el9cp", false},
		{"sha256:" + strings.Repeat("a", 64), false},
		{"latest", true},
		{"-1", true},
		{"cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:9", true},
		{"sha256:abc", true},
		{"9 1234", true},
	}
	for _, tc := range cases {
		cluster := versionPinCluster(v1alpha1.StorageCephDistributionIBM, "9.9.1.0", "", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9", tc.version)
		errs := validateStorageCephImage("spec.ceph.image", cluster, v1alpha1.State{})
		if got := len(errs) > 0; got != tc.wantError {
			t.Fatalf("version %q error = %v (%v), want error %v", tc.version, got, errs, tc.wantError)
		}
	}
}

func TestValidateStorageCephImageVersionAcceptsAnyVendorBuild(t *testing.T) {
	for _, distribution := range []string{v1alpha1.StorageCephDistributionRedHat, v1alpha1.StorageCephDistributionIBM} {
		for _, version := range []string{"99-99999", "99.99.99.99-99999", "v0"} {
			base := "registry.redhat.io/rhceph/rhceph-99-rhel14"
			if distribution == v1alpha1.StorageCephDistributionIBM {
				base = "cp.icr.io/cp/ibm-ceph/ceph-99-rhel14"
			}
			cluster := versionPinCluster(distribution, "99.99.99.99", "", base, version)
			if errs := validateStorageCephImage("spec.ceph.image", cluster, v1alpha1.State{}); len(errs) != 0 {
				t.Fatalf("%s build tag %q rejected: %v; Bootwright holds no catalog of vendor builds and must accept any coordinate the operator read off the vendor matrix", distribution, version, errs)
			}
		}
	}
}

func TestValidateStorageCephPackageVersionSyntax(t *testing.T) {
	cases := []struct {
		version   string
		wantError bool
	}{
		{"19.2.1-245.el9cp", false},
		{"2:19.2.1-245.el9cp", false},
		{"19.2.1", true},
		{"19.2.1-245.0.hotfix.BYOK.el9cp", false},
		{"*", true},
		{"19.2.1 245", true},
		{"19.2.1,19.2.2", true},
		{"cephadm-19.2.1-245.el9cp", true},
		{"-245.el9cp", true},
	}
	for _, tc := range cases {
		cluster := versionPinCluster(v1alpha1.StorageCephDistributionIBM, "9.9.1.0", tc.version, "", "")
		errs := validateStorageCephPackageVersion("spec.ceph", cluster)
		if got := len(errs) > 0; got != tc.wantError {
			t.Fatalf("packageVersion %q error = %v (%v), want error %v", tc.version, got, errs, tc.wantError)
		}
	}
}

func TestValidateStorageCephPackageVersionAcceptsAnyBuild(t *testing.T) {
	for _, version := range []string{"99.99.99-99999.el99cp", "0", "1:0.0.1-1.el10cp"} {
		cluster := versionPinCluster(v1alpha1.StorageCephDistributionRedHat, "99.99", version, "", "")
		if errs := validateStorageCephPackageVersion("spec.ceph", cluster); len(errs) != 0 {
			t.Fatalf("package build %q rejected: %v; Bootwright holds no release-to-package-version matrix and must take the operator's build verbatim", version, errs)
		}
	}
}

func TestValidateStorageCephadmAnsiblePackageVersionSyntax(t *testing.T) {
	cases := []struct {
		version   string
		wantError bool
	}{
		{"5.0.2-1.el9cp", false},
		{"2:5.0.2-1.el9cp", false},
		{"5.0.2", true},
		{"42.7.3-456.0.future.el14", false},
		{"*", true},
		{"5.0.2 1", true},
		{"cephadm-ansible-5.0.2-1.el9cp", true},
	}
	for _, tc := range cases {
		cluster := withCephadmAnsiblePackageVersion(versionPinCluster(v1alpha1.StorageCephDistributionIBM, "9.9.1.0", "20.1.0-221.el9cp", "", ""), tc.version)
		errs := validateStorageCephadmAnsiblePackageVersion("spec.ceph.cephadm.ansible", cluster)
		if got := len(errs) > 0; got != tc.wantError {
			t.Fatalf("cephadm ansible packageVersion %q error = %v (%v), want error %v", tc.version, got, errs, tc.wantError)
		}
	}
}

func TestValidateStorageCephadmAnsiblePackageVersionUsesProviderPolicy(t *testing.T) {
	ibm := versionPinCluster(v1alpha1.StorageCephDistributionIBM, "42.7.3.1", "31.7.3-456.el14", "", "")
	if errs := validateStorageCephadmAnsiblePackageVersion("spec.ceph.cephadm.ansible", ibm); len(errs) == 0 || !strings.Contains(strings.Join(errs, "; "), "is required") {
		t.Fatalf("missing required IBM cephadm-ansible coordinate = %v", errs)
	}
	ibm = withCephadmAnsiblePackageVersion(ibm, "17.3.2-9.el14")
	if errs := validateStorageCephadmAnsiblePackageVersion("spec.ceph.cephadm.ansible", ibm); len(errs) != 0 {
		t.Fatalf("synthetic future IBM cephadm-ansible coordinate rejected: %v", errs)
	}

	redhat := versionPinCluster(v1alpha1.StorageCephDistributionRedHat, "42.1", "", "", "")
	if errs := validateStorageCephadmAnsiblePackageVersion("spec.ceph.cephadm.ansible", redhat); len(errs) != 0 {
		t.Fatalf("optional unpinned Red Hat cephadm-ansible artifact rejected: %v", errs)
	}
	redhat = withCephadmAnsiblePackageVersion(redhat, "17.3.2")
	if errs := validateStorageCephadmAnsiblePackageVersion("spec.ceph.cephadm.ansible", redhat); len(errs) != 0 {
		t.Fatalf("optional Red Hat version-only cephadm-ansible coordinate rejected: %v", errs)
	}

	oss := withCephadmAnsiblePackageVersion(versionPinCluster(v1alpha1.StorageCephDistributionOSS, "future", "", "", ""), "17.3.2-9.el14")
	if errs := validateStorageCephadmAnsiblePackageVersion("spec.ceph.cephadm.ansible", oss); len(errs) == 0 || !strings.Contains(strings.Join(errs, "; "), "artifact policy forbids") {
		t.Fatalf("OSS cephadm-ansible coordinate = %v", errs)
	}
}

func TestValidateStorageCephPackageVersionRejectedForOSS(t *testing.T) {
	cluster := versionPinCluster(v1alpha1.StorageCephDistributionOSS, "19.2.1", "19.2.1-245.el9cp", "", "")
	errs := validateStorageCephPackageVersion("spec.ceph", cluster)
	if len(errs) == 0 || !strings.Contains(errs[0], "artifact policy forbids") {
		t.Fatalf("oss packageVersion = %v", errs)
	}
}

func TestValidateStorageCephArtifactPolicy(t *testing.T) {
	cases := []struct {
		name                  string
		distribution          string
		packageVersion        string
		ansiblePackageVersion string
		imageBase             string
		imageVersion          string
		want                  []string
	}{
		{name: "all IBM pins missing", distribution: v1alpha1.StorageCephDistributionIBM, want: []string{"spec.ceph.packageVersion is required", "spec.ceph.cephadm.ansible.packageVersion is required", "image.base is required", "image.version is required"}},
		{name: "IBM package missing", distribution: v1alpha1.StorageCephDistributionIBM, ansiblePackageVersion: "17.3.2-9.el14", imageBase: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel14", imageVersion: "42-future-build", want: []string{"spec.ceph.packageVersion is required"}},
		{name: "IBM cephadm-ansible package missing", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "31.7.3-456.el14", imageBase: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel14", imageVersion: "42-future-build", want: []string{"spec.ceph.cephadm.ansible.packageVersion is required"}},
		{name: "IBM image pin missing", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "31.7.3-456.el14", ansiblePackageVersion: "17.3.2-9.el14", imageBase: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel14", want: []string{"image.version is required"}},
		{name: "IBM package release missing", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "31.7.3", ansiblePackageVersion: "17.3.2-9.el14", imageBase: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel14", imageVersion: "42-future-build", want: []string{"spec.ceph.packageVersion must include the RPM release component"}},
		{name: "IBM cephadm-ansible release missing", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "31.7.3-456.el14", ansiblePackageVersion: "17.3.2", imageBase: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel14", imageVersion: "42-future-build", want: []string{"spec.ceph.cephadm.ansible.packageVersion must include the RPM release component"}},
		{name: "future IBM tuple", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "31.7.3-456.el14", ansiblePackageVersion: "17.3.2-9.el14", imageBase: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel14", imageVersion: "42-future-build"},
		{name: "IBM epoch declared", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "2:31.7.3-456.el14", ansiblePackageVersion: "2:17.3.2-9.el14", imageBase: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel14", imageVersion: "42-future-build"},
		{name: "Red Hat package and image pins remain optional", distribution: v1alpha1.StorageCephDistributionRedHat, imageBase: "registry.redhat.io/rhceph/rhceph-9-rhel14"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := versionPinCluster(tc.distribution, "9.9.0.3", tc.packageVersion, tc.imageBase, tc.imageVersion)
			if tc.ansiblePackageVersion != "" {
				cluster = withCephadmAnsiblePackageVersion(cluster, tc.ansiblePackageVersion)
			}
			errs := validateStorageCephPackageVersion("spec.ceph", cluster)
			errs = append(errs, validateStorageCephadmAnsiblePackageVersion("spec.ceph.cephadm.ansible", cluster)...)
			errs = append(errs, validateStorageCephImage("spec.ceph.image", cluster, v1alpha1.State{})...)
			got := strings.Join(errs, "; ")
			if len(tc.want) != len(errs) {
				t.Fatalf("errors = %v, want %v", errs, tc.want)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("errors = %v, want substring %q", errs, want)
				}
			}
		})
	}
}

func TestValidateStorageCephImageBaseDoesNotRequireAnOptionalPin(t *testing.T) {
	for _, cluster := range []v1alpha1.StorageCluster{
		versionPinCluster(v1alpha1.StorageCephDistributionOSS, "future", "", "mirror.example.test/ceph/ceph", ""),
		versionPinCluster(v1alpha1.StorageCephDistributionRedHat, "42.1", "", "registry.redhat.io/rhceph/rhceph-42-rhel14", ""),
	} {
		if errs := validateStorageCephImage("spec.ceph.image", cluster, v1alpha1.State{}); len(errs) != 0 {
			t.Fatalf("base-only image for %s was rejected: %v", cluster.Spec.Ceph.Distribution, errs)
		}
	}
}

func TestValidateStorageCephImageVersionDoesNotSatisfyARegistryOverride(t *testing.T) {
	state := v1alpha1.State{Entitlements: []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhcs"},
		Spec:     v1alpha1.EntitlementSpec{Registry: &v1alpha1.EntitlementRegistry{URL: "mirror.example.test/vendor"}},
	}}}
	cluster := versionPinCluster(v1alpha1.StorageCephDistributionRedHat, "9.1", "", "", "9-1234")
	cluster.Spec.Ceph.EntitlementRef = v1alpha1.LocalObjectReference{Name: "rhcs"}
	errs := validateStorageCephImage("spec.ceph.image", cluster, state)
	if len(errs) == 0 || !strings.Contains(strings.Join(errs, "; "), "base is required") {
		t.Fatalf("a mirrored registry with only a version = %v; the derived base names the vendor registry, which the mirror cannot serve", errs)
	}

	cluster.Spec.Ceph.Image.Base = "mirror.example.test/vendor/rhceph/rhceph-9-rhel9"
	if errs := validateStorageCephImage("spec.ceph.image", cluster, state); len(errs) != 0 {
		t.Fatalf("a mirrored base under the override was rejected: %v", errs)
	}
}

func TestNormalizeKeepsAnAuthoredOSSImageVersion(t *testing.T) {
	state := v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{
		versionPinCluster(v1alpha1.StorageCephDistributionOSS, "20.2.2", "", "", "v20.2.1"),
	}}
	state.StorageClusters[0].Spec.Type = v1alpha1.StorageClusterTypeCeph
	Normalize(&state)
	if got := v1alpha1.StorageCephImageVersion(state.StorageClusters[0].Spec.Ceph); got != "v20.2.1" {
		t.Fatalf("normalized oss image version = %q, want the authored v20.2.1", got)
	}
}
