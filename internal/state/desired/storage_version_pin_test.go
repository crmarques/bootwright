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
		cluster := versionPinCluster(v1alpha1.StorageCephDistributionIBM, "9.9.1.0", "", "", tc.version)
		errs := validateStorageCephImage("spec.ceph.image", cluster, v1alpha1.State{})
		if got := len(errs) > 0; got != tc.wantError {
			t.Fatalf("version %q error = %v (%v), want error %v", tc.version, got, errs, tc.wantError)
		}
	}
}

func TestValidateStorageCephImageVersionAcceptsAnyVendorBuild(t *testing.T) {
	for _, distribution := range []string{v1alpha1.StorageCephDistributionRedHat, v1alpha1.StorageCephDistributionIBM} {
		for _, version := range []string{"99-99999", "99.99.99.99-99999", "v0"} {
			cluster := versionPinCluster(distribution, "99.99.99.99", "", "", version)
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
		{"19.2.1", false},
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

func TestValidateStorageCephPackageVersionRejectedForOSS(t *testing.T) {
	cluster := versionPinCluster(v1alpha1.StorageCephDistributionOSS, "19.2.1", "19.2.1-245.el9cp", "", "")
	errs := validateStorageCephPackageVersion("spec.ceph", cluster)
	if len(errs) == 0 || !strings.Contains(errs[0], "must be empty when distribution=oss") {
		t.Fatalf("oss packageVersion = %v", errs)
	}
}

func TestValidateStorageCephIBMRequiresArtifactPins(t *testing.T) {
	cases := []struct {
		name           string
		distribution   string
		packageVersion string
		imageVersion   string
		want           []string
	}{
		{name: "both missing", distribution: v1alpha1.StorageCephDistributionIBM, want: []string{"packageVersion is required", "image.version is required"}},
		{name: "package missing", distribution: v1alpha1.StorageCephDistributionIBM, imageVersion: "v9.0-20201", want: []string{"packageVersion is required"}},
		{name: "image missing", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "20.1.0-221.el9cp", want: []string{"image.version is required"}},
		{name: "package release missing", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "20.1.0", imageVersion: "v9.0-20201", want: []string{"must include the RPM release component"}},
		{name: "both declared", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "20.1.0-221.el9cp", imageVersion: "v9.0-20201"},
		{name: "epoch declared", distribution: v1alpha1.StorageCephDistributionIBM, packageVersion: "2:20.1.0-221.el9cp", imageVersion: "v9.0-20201"},
		{name: "redhat remains optional", distribution: v1alpha1.StorageCephDistributionRedHat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cluster := versionPinCluster(tc.distribution, "9.9.0.3", tc.packageVersion, "", tc.imageVersion)
			errs := validateStorageCephIBMArtifactPins("spec.ceph", cluster)
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
