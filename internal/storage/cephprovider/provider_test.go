package cephprovider

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func TestVarsEmitsOSRegistrationRHSMForOSS(t *testing.T) {
	provider := Provider{
		Distribution: v1alpha1.StorageCephDistributionOSS,
		OSRegistration: entitlements.Resolved{
			Name: "rhel-satellite",
			RHSM: entitlements.RHSM{
				Management:        v1alpha1.EntitlementRHSMManagementManaged,
				OrganizationPath:  "/context/secrets/org",
				ActivationKeyPath: "/context/secrets/ak",
				Satellite:         entitlements.RHSMSatellite{Hostname: "satellite.example.com", ContentBaseURL: "https://satellite.example.com/pulp/content"},
			},
		},
	}
	vars := Vars(provider)
	if vars["requiresRHSM"] != false {
		t.Fatalf("requiresRHSM = %v, want false (OSS must not flip the vendor-RHSM flag)", vars["requiresRHSM"])
	}
	if vars["rhsmManagement"] != v1alpha1.EntitlementRHSMManagementManaged {
		t.Fatalf("rhsmManagement = %v, want managed", vars["rhsmManagement"])
	}
	rhsm, ok := vars["rhsm"].(map[string]any)
	if !ok {
		t.Fatalf("vars missing rhsm map: %#v", vars)
	}
	if rhsm["organizationPath"] != "/context/secrets/org" || rhsm["activationKeyPath"] != "/context/secrets/ak" {
		t.Fatalf("rhsm paths = %#v", rhsm)
	}
	sat, ok := rhsm["satellite"].(map[string]any)
	if !ok || sat["hostname"] != "satellite.example.com" {
		t.Fatalf("rhsm satellite = %#v", rhsm["satellite"])
	}
}

func TestVarsEmitsOSRegistrationRHSMForIBM(t *testing.T) {
	provider := Provider{
		Distribution: v1alpha1.StorageCephDistributionIBM,
		RequiresRHSM: true,
		OSRegistration: entitlements.Resolved{
			Name: "rhel",
			RHSM: entitlements.RHSM{
				Management:        v1alpha1.EntitlementRHSMManagementManaged,
				OrganizationPath:  "/context/secrets/ibm-org",
				ActivationKeyPath: "/context/secrets/ibm-key",
			},
		},
	}
	vars := Vars(provider)
	if vars["rhsmManagement"] != v1alpha1.EntitlementRHSMManagementManaged {
		t.Fatalf("rhsmManagement = %v, want managed", vars["rhsmManagement"])
	}
	rhsm, ok := vars["rhsm"].(map[string]any)
	if !ok {
		t.Fatalf("ibm registration rhsm must come from the profile subscription: %#v", vars)
	}
	if rhsm["organizationPath"] != "/context/secrets/ibm-org" || rhsm["activationKeyPath"] != "/context/secrets/ibm-key" {
		t.Fatalf("ibm rhsm from OSRegistration = %#v", rhsm)
	}
}

func TestSelectDefaultsToOSSProvider(t *testing.T) {
	release := "37.4.2"
	cluster := v1alpha1.StorageCluster{
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{Release: release},
		},
	}
	provider := Select(cluster, nil, secret.Index{}, "/context/secrets")
	if provider.Distribution != v1alpha1.StorageCephDistributionOSS {
		t.Fatalf("distribution = %q, want oss", provider.Distribution)
	}
	if provider.RequiresRHSM || provider.RequiresRegistry || provider.RequiresLicense {
		t.Fatalf("OSS provider requires vendor material: %#v", provider)
	}
	if provider.NativeCapabilityCandidates != (NativeCapabilityCandidates{}) {
		t.Fatalf("OSS provider carries vendor native capability candidates: %#v", provider.NativeCapabilityCandidates)
	}
	if provider.Community.Version != release {
		t.Fatalf("community version = %q, want authored %q", provider.Community.Version, release)
	}
	community, ok := Vars(provider)["community"].(map[string]any)
	if !ok {
		t.Fatalf("oss provider vars missing community map: %#v", Vars(provider))
	}
	if community["version"] != release {
		t.Fatalf("community vars version = %v, want authored release", community["version"])
	}
	if provider.Image != "quay.io/ceph/ceph:v"+release {
		t.Fatalf("default image = %q, want exact release image", provider.Image)
	}
	if _, hasMirror := community["mirror"]; hasMirror {
		t.Fatalf("community vars must omit mirror when unset: %#v", community)
	}
}

func TestIBMNativeCapabilityCandidatesAreReleaseAgnostic(t *testing.T) {
	want := NativeCapabilityCandidates{
		CephadmBootstrapLicenseOption: ibmCephadmBootstrapLicenseOption,
		CephOrchCallHomeConsentToken:  ibmCephOrchCallHomeConsentToken,
	}
	for _, release := range []string{"9.9.0.3", "9.9.1.0", "42.7.3.1"} {
		cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionIBM,
			Release:      release,
		}}}
		if got := Select(cluster, nil, secret.Index{}, "/context/secrets").NativeCapabilityCandidates; got != want {
			t.Fatalf("IBM release %s native capability candidates = %#v, want %#v", release, got, want)
		}
	}
}

func TestSelectOwnsStorageSupportReportPrerequisite(t *testing.T) {
	provider := Select(v1alpha1.StorageCluster{}, nil, secret.Index{}, "/context/secrets")
	if !slices.Contains(provider.PrerequisitePackages, "sos") {
		t.Fatalf("storage prerequisites = %v, want sos", provider.PrerequisitePackages)
	}
	projected, ok := Vars(provider)["prerequisitePackages"].([]string)
	if !ok || !slices.Contains(projected, "sos") {
		t.Fatalf("projected storage prerequisites = %#v, want sos", Vars(provider)["prerequisitePackages"])
	}
}

func TestResolveReleaseCarriesTheAuthoredValueAndItsStream(t *testing.T) {
	cases := []struct {
		distribution string
		authored     string
		wantValue    string
		wantStream   string
	}{
		{v1alpha1.StorageCephDistributionRedHat, "9", "9", "9"},
		{v1alpha1.StorageCephDistributionRedHat, "9.0.3", "9.0.3", "9"},
		{v1alpha1.StorageCephDistributionRedHat, "9.2", "9.2", "9"},
		{v1alpha1.StorageCephDistributionRedHat, "10", "10", "10"},
		{v1alpha1.StorageCephDistributionIBM, "9.9.2.0", "9.9.2.0", "9"},
		{v1alpha1.StorageCephDistributionIBM, "10.1.0.0", "10.1.0.0", "10"},
	}
	for _, tc := range cases {
		release, ok := ResolveRelease(tc.distribution, tc.authored)
		if !ok {
			t.Fatalf("ResolveRelease(%q, %q) rejected a parseable release", tc.distribution, tc.authored)
		}
		if release.Value != tc.wantValue || release.Stream != tc.wantStream {
			t.Fatalf("ResolveRelease(%q, %q) = %#v, want value %q stream %q", tc.distribution, tc.authored, release, tc.wantValue, tc.wantStream)
		}
	}
}

func TestResolveReleaseRejectsMissingAuthoredVersion(t *testing.T) {
	for _, distribution := range []string{
		v1alpha1.StorageCephDistributionOSS,
		v1alpha1.StorageCephDistributionRedHat,
		v1alpha1.StorageCephDistributionIBM,
	} {
		if _, ok := ResolveRelease(distribution, ""); ok {
			t.Fatalf("ResolveRelease(%q, empty) selected a compiled default", distribution)
		}
	}
}

func TestArtifactPoliciesDriveExactCoordinatesWithoutReleaseBranches(t *testing.T) {
	cases := []struct {
		distribution string
		want         ArtifactPolicy
	}{
		{distribution: v1alpha1.StorageCephDistributionOSS, want: ArtifactPolicy{PackagePin: ArtifactPinForbidden, CephadmAnsiblePackagePin: ArtifactPinForbidden}},
		{distribution: v1alpha1.StorageCephDistributionRedHat, want: ArtifactPolicy{PackagePin: ArtifactPinOptional, CephadmAnsiblePackagePin: ArtifactPinOptional, ImageBaseRequired: true, NativePreparationMode: NativePreparationCephadmAnsibleLocal}},
		{distribution: v1alpha1.StorageCephDistributionIBM, want: ArtifactPolicy{
			PackagePin:                       ArtifactPinRequired,
			RPMReleaseRequired:               true,
			CephadmAnsiblePackagePin:         ArtifactPinRequired,
			CephadmAnsibleRPMReleaseRequired: true,
			ImageBaseRequired:                true,
			ImagePinRequired:                 true,
			NativeParityMode:                 NativeParityCephVersion,
			NativePreparationMode:            NativePreparationCephadmAnsibleLocal,
		}},
	}
	for _, tc := range cases {
		policy, ok := ArtifactPolicyFor(tc.distribution)
		if !ok || policy != tc.want {
			t.Fatalf("ArtifactPolicyFor(%q) = %#v, %t; want %#v", tc.distribution, policy, ok, tc.want)
		}
		release := "42.7.3.1"
		if tc.distribution == v1alpha1.StorageCephDistributionOSS {
			release = "future"
		}
		cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: tc.distribution,
			Release:      release,
		}}}
		provider := Select(cluster, nil, secret.Index{}, "/context/secrets")
		if provider.ArtifactPolicy != tc.want {
			t.Fatalf("Select(%q) artifact policy = %#v, want %#v", tc.distribution, provider.ArtifactPolicy, tc.want)
		}
		projected := Vars(provider)["artifactPolicy"].(map[string]any)
		if projected["packagePin"] != string(tc.want.PackagePin) || projected["cephadmAnsiblePackagePin"] != string(tc.want.CephadmAnsiblePackagePin) || projected["nativeParityMode"] != tc.want.NativeParityMode || projected["nativePreparationMode"] != tc.want.NativePreparationMode {
			t.Fatalf("Vars(%q) artifact policy = %#v", tc.distribution, projected)
		}
	}
}

func TestResolveReleaseAcceptsAnyParseableOSSRelease(t *testing.T) {
	for _, release := range []string{"tentacle", "20.2.2", "squid", "19.2.1", "reef", "18.2.7", "21.2.0", "unicorn"} {
		if _, ok := ResolveRelease(v1alpha1.StorageCephDistributionOSS, release); !ok {
			t.Fatalf("ResolveRelease(oss, %q) rejected a parseable release", release)
		}
	}
	for _, release := range []string{"", "20.2", "Squid", "19.2.1-rc1"} {
		if _, ok := ResolveRelease(v1alpha1.StorageCephDistributionOSS, release); ok {
			t.Fatalf("ResolveRelease(oss, %q) accepted an underivable release", release)
		}
	}
}

func TestResolveReleaseJudgesNoVendorSupportMatrix(t *testing.T) {
	release, ok := ResolveRelease(v1alpha1.StorageCephDistributionIBM, "9.9.2.0")
	if !ok {
		t.Fatal("a vendor release newer than Bootwright was rejected")
	}
	if release != (ResolvedRelease{Value: "9.9.2.0", Stream: "9"}) {
		t.Fatalf("ResolveRelease carries more than the value and its stream: %#v", release)
	}
	if _, ok := ResolveRelease(v1alpha1.StorageCephDistributionIBM, "9.9.1-beta"); ok {
		t.Fatal("non-numeric vendor product version accepted")
	}
}

func TestSelectIBMProviderProjectsCallHomeIntent(t *testing.T) {
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution: v1alpha1.StorageCephDistributionIBM,
		Release:      "9.9.1.0",
		IBM:          &v1alpha1.StorageCephIBMSpec{CallHome: v1alpha1.StorageCephIBMCallHomeDisabled},
	}}}
	provider := Select(cluster, nil, secret.Index{}, "/context/secrets")
	ibm, ok := Vars(provider)["ibm"].(map[string]any)
	if !ok || ibm["callHome"] != v1alpha1.StorageCephIBMCallHomeDisabled {
		t.Fatalf("IBM provider vars = %#v", Vars(provider))
	}
}

func TestSelectOSSProviderHonorsCommunityOverride(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: v1alpha1.StorageCephDistributionOSS,
				Release:      "squid",
				Community: &v1alpha1.StorageCephCommunitySpec{
					Mirror: "https://mirror.example.test/ceph",
				},
			},
		},
	}
	provider := Select(cluster, nil, secret.Index{}, "/context/secrets")
	if provider.Community.Release != "squid" || provider.Community.Mirror != "https://mirror.example.test/ceph" {
		t.Fatalf("community override not projected: %#v", provider.Community)
	}
	community := Vars(provider)["community"].(map[string]any)
	if community["release"] != "squid" || community["mirror"] != "https://mirror.example.test/ceph" {
		t.Fatalf("community vars = %#v", community)
	}
}

func imageSpec(base, version string) *v1alpha1.StorageCephImageSpec {
	if base == "" && version == "" {
		return nil
	}
	return &v1alpha1.StorageCephImageSpec{Base: base, Version: version}
}

func TestSelectOSSProviderClassifiesVersionAndDerivesImage(t *testing.T) {
	oss := func(release, base, version string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionOSS,
			Release:      release,
			Image:        imageSpec(base, version),
		}}}
	}

	provider := Select(oss("19.2.1", "", ""), nil, secret.Index{}, "/context/secrets")
	if provider.Community.Version != "19.2.1" || provider.Community.Release != "" {
		t.Fatalf("version not classified: %#v", provider.Community)
	}
	if provider.Image != "quay.io/ceph/ceph:v19.2.1" {
		t.Fatalf("derived image = %q, want quay.io/ceph/ceph:v19.2.1", provider.Image)
	}
	vars := Vars(provider)
	community := vars["community"].(map[string]any)
	if community["version"] != "19.2.1" {
		t.Fatalf("community vars = %#v, want version 19.2.1", community)
	}
	if _, ok := community["release"]; ok {
		t.Fatalf("community vars must omit release for a version pin: %#v", community)
	}
	if vars["image"] != "quay.io/ceph/ceph:v19.2.1" {
		t.Fatalf("image var = %v, want derived image", vars["image"])
	}

	nameProvider := Select(oss("squid", "", ""), nil, secret.Index{}, "/context/secrets")
	if nameProvider.Community.Release != "squid" || nameProvider.Image != "" {
		t.Fatalf("name release derived an image: %#v image=%q", nameProvider.Community, nameProvider.Image)
	}
	if _, ok := Vars(nameProvider)["image"]; ok {
		t.Fatalf("name release must omit image var")
	}

	digest := "sha256:" + strings.Repeat("a", 64)
	pinned := Select(oss("19.2.1", "", digest), nil, secret.Index{}, "/context/secrets")
	if pinned.Image != "quay.io/ceph/ceph@"+digest {
		t.Fatalf("authored digest not honored: %q", pinned.Image)
	}

	mirrored := Select(oss("19.2.1", "mirror.example.test/ceph/ceph", "v19.2.1"), nil, secret.Index{}, "/context/secrets")
	if mirrored.Image != "mirror.example.test/ceph/ceph:v19.2.1" || mirrored.ImageBase != "mirror.example.test/ceph/ceph" {
		t.Fatalf("authored base not honored: image=%q base=%q", mirrored.Image, mirrored.ImageBase)
	}
}

func TestSelectComposesImageVersionOntoTheAuthoredBase(t *testing.T) {
	cluster := func(distribution, release, base, version string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: distribution,
			Release:      release,
			Image:        imageSpec(base, version),
		}}}
	}
	cases := []struct {
		distribution string
		release      string
		base         string
		version      string
		want         string
	}{
		{v1alpha1.StorageCephDistributionIBM, "9.9.1.0", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9", "9.9.1.0-123", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:9.9.1.0-123"},
		{v1alpha1.StorageCephDistributionRedHat, "9.1", "registry.redhat.io/rhceph/rhceph-9-rhel9", "9-1234", "registry.redhat.io/rhceph/rhceph-9-rhel9:9-1234"},
		{v1alpha1.StorageCephDistributionOSS, "squid", "quay.io/ceph/ceph", "v19.2.1", "quay.io/ceph/ceph:v19.2.1"},
	}
	for _, tc := range cases {
		provider := Select(cluster(tc.distribution, tc.release, tc.base, tc.version), nil, secret.Index{}, "/context/secrets")
		if provider.Image != tc.want {
			t.Fatalf("%s composed image = %q, want %q", tc.distribution, provider.Image, tc.want)
		}
		if provider.ImageBase != ImageRepository(tc.want) {
			t.Fatalf("%s image base = %q, want %q", tc.distribution, provider.ImageBase, ImageRepository(tc.want))
		}
	}
}

func TestSelectAuthoredImageBaseMatchesTheVendorPrefixGuard(t *testing.T) {
	for _, distribution := range []string{v1alpha1.StorageCephDistributionIBM, v1alpha1.StorageCephDistributionRedHat} {
		for _, release := range []string{"9", "9.1", "10.0.0.1"} {
			prefix, ok := ImageRepositoryPrefix(distribution, release, "")
			if !ok {
				t.Fatalf("%s release %q has no vendor prefix", distribution, release)
			}
			base := prefix + "14"
			cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: distribution,
				Release:      release,
				Image:        imageSpec(base, "42-future-build"),
			}}}
			provider := Select(cluster, nil, secret.Index{}, "/context/secrets")
			if provider.ImageBase != base {
				t.Fatalf("%s release %q image base = %q, want authored %q", distribution, release, provider.ImageBase, base)
			}
		}
	}
}

func TestSelectKeepsTheOwnershipPackageNameBareWhilePinningTheBuild(t *testing.T) {
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionIBM,
		Release:        "9.9.1.0",
		PackageVersion: "19.2.1-245.el9cp",
		Cephadm: v1alpha1.StorageCephadmSpec{Ansible: &v1alpha1.StorageCephadmAnsible{
			PackageVersion: "5.0.2-1.el9cp",
		}},
	}}}
	provider := Select(cluster, nil, secret.Index{}, "/context/secrets")
	if provider.CephadmPackage != "cephadm" {
		t.Fatalf("ownership package name = %q, want bare cephadm", provider.CephadmPackage)
	}
	if provider.CephadmPackageSpec != "cephadm-19.2.1-245.el9cp" {
		t.Fatalf("pinned install spec = %q", provider.CephadmPackageSpec)
	}
	if provider.CephadmAnsiblePackage != "cephadm-ansible" || provider.CephadmAnsiblePackageSpec != "cephadm-ansible-5.0.2-1.el9cp" {
		t.Fatalf("cephadm-ansible package coordinates = %q %q", provider.CephadmAnsiblePackage, provider.CephadmAnsiblePackageSpec)
	}
	wantArtifacts := []PackageArtifact{
		{Name: "cephadm", Spec: "cephadm-19.2.1-245.el9cp", DesiredStatePath: "spec.ceph.packageVersion"},
		{Name: "ceph-common", Spec: "ceph-common-19.2.1-245.el9cp", DesiredStatePath: "spec.ceph.packageVersion"},
		{Name: "cephadm-ansible", Spec: "cephadm-ansible-5.0.2-1.el9cp", DesiredStatePath: "spec.ceph.cephadm.ansible.packageVersion"},
	}
	if !slices.Equal(provider.PackageArtifacts, wantArtifacts) {
		t.Fatalf("IBM package artifacts = %#v, want %#v", provider.PackageArtifacts, wantArtifacts)
	}
	if !slices.Equal(provider.NativePreparation.RuntimePackages, []string{"ceph-common"}) || !slices.Equal(provider.NativePreparation.RuntimePackageSpecs, []string{"ceph-common-19.2.1-245.el9cp"}) {
		t.Fatalf("IBM native preparation runtime packages = %#v", provider.NativePreparation)
	}
	vars := Vars(provider)
	if vars["cephadmPackage"] != "cephadm" || vars["cephadmPackageSpec"] != "cephadm-19.2.1-245.el9cp" || vars["cephadmAnsiblePackage"] != "cephadm-ansible" || vars["cephadmAnsiblePackageSpec"] != "cephadm-ansible-5.0.2-1.el9cp" {
		t.Fatalf("package vars = %#v", vars)
	}
	if projected, ok := vars["packageArtifacts"].([]map[string]any); !ok || len(projected) != len(wantArtifacts) {
		t.Fatalf("projected IBM package artifacts = %#v", vars["packageArtifacts"])
	}

	cluster.Spec.Ceph.PackageVersion = ""
	cluster.Spec.Ceph.Cephadm.Ansible = nil
	if _, ok := Vars(Select(cluster, nil, secret.Index{}, "/context/secrets"))["cephadmPackageSpec"]; ok {
		t.Fatalf("unpinned provider must omit cephadmPackageSpec")
	}
	if _, ok := Vars(Select(cluster, nil, secret.Index{}, "/context/secrets"))["cephadmAnsiblePackageSpec"]; ok {
		t.Fatalf("unpinned provider must omit cephadmAnsiblePackageSpec")
	}
}

func TestSelectRedHatNativePreparationKeepsOptionalPinsIndependent(t *testing.T) {
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution: v1alpha1.StorageCephDistributionRedHat,
		Release:      "42.1",
	}}}

	unpinned := Select(cluster, nil, secret.Index{}, "/context/secrets")
	if len(unpinned.PackageArtifacts) != 0 {
		t.Fatalf("unpinned Red Hat package artifacts = %#v, want none", unpinned.PackageArtifacts)
	}
	if !slices.Equal(unpinned.NativePreparation.RuntimePackages, []string{"ceph-common"}) || len(unpinned.NativePreparation.RuntimePackageSpecs) != 0 {
		t.Fatalf("unpinned Red Hat native preparation = %#v", unpinned.NativePreparation)
	}
	if _, ok := Vars(unpinned)["packageArtifacts"]; ok {
		t.Fatalf("unpinned Red Hat provider must omit exact packageArtifacts")
	}

	cluster.Spec.Ceph.PackageVersion = "31.7.3-456.el14"
	runtimePinned := Select(cluster, nil, secret.Index{}, "/context/secrets")
	wantRuntime := []PackageArtifact{
		{Name: "cephadm", Spec: "cephadm-31.7.3-456.el14", DesiredStatePath: "spec.ceph.packageVersion"},
		{Name: "ceph-common", Spec: "ceph-common-31.7.3-456.el14", DesiredStatePath: "spec.ceph.packageVersion"},
	}
	if !slices.Equal(runtimePinned.PackageArtifacts, wantRuntime) || !slices.Equal(runtimePinned.NativePreparation.RuntimePackageSpecs, []string{"ceph-common-31.7.3-456.el14"}) {
		t.Fatalf("runtime-pinned Red Hat provider = artifacts %#v preparation %#v", runtimePinned.PackageArtifacts, runtimePinned.NativePreparation)
	}

	cluster.Spec.Ceph.PackageVersion = ""
	cluster.Spec.Ceph.Cephadm.Ansible = &v1alpha1.StorageCephadmAnsible{PackageVersion: "17.3.2-9.el14"}
	ansiblePinned := Select(cluster, nil, secret.Index{}, "/context/secrets")
	wantAnsible := []PackageArtifact{{Name: "cephadm-ansible", Spec: "cephadm-ansible-17.3.2-9.el14", DesiredStatePath: "spec.ceph.cephadm.ansible.packageVersion"}}
	if !slices.Equal(ansiblePinned.PackageArtifacts, wantAnsible) || len(ansiblePinned.NativePreparation.RuntimePackageSpecs) != 0 {
		t.Fatalf("ansible-pinned Red Hat provider = artifacts %#v preparation %#v", ansiblePinned.PackageArtifacts, ansiblePinned.NativePreparation)
	}
}

func TestSelectSubscriptionProviderResolvesStreamAndImage(t *testing.T) {
	redhat := func(release, base, version string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionRedHat,
			Release:      release,
			Image:        imageSpec(base, version),
		}}}
	}

	repos := Select(redhat("9.0", "registry.redhat.io/rhceph/rhceph-9-rhel9", "9"), nil, secret.Index{}, "/context/secrets").Repository.RedHatRepos
	if got := repos[len(repos)-1]; got != "rhceph-9-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms" {
		t.Fatalf("stream tools repo = %q, want rhceph-9-tools", got)
	}
	provider := Select(redhat("9.0", "registry.redhat.io/rhceph/rhceph-9-rhel9", "9"), nil, secret.Index{}, "/context/secrets")
	if provider.Image != "registry.redhat.io/rhceph/rhceph-9-rhel9:9" {
		t.Fatalf("explicit image not honored: %q", provider.Image)
	}
	if Vars(provider)["image"] != "registry.redhat.io/rhceph/rhceph-9-rhel9:9" {
		t.Fatalf("image var missing for redhat: %#v", Vars(provider))
	}

	ibm := func(release string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionIBM,
			Release:      release,
		}}}
	}
	if url := Select(ibm("9"), nil, secret.Index{}, "/context/secrets").Repository.IBMRepoURL; url != "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-9-rhel-{{ ansible_distribution_major_version }}.repo" {
		t.Fatalf("stream alias ibm repo url = %q, want stream 9", url)
	}
	if url := Select(ibm("9.9.1.0"), nil, secret.Index{}, "/context/secrets").Repository.IBMRepoURL; url != "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-9-rhel-{{ ansible_distribution_major_version }}.repo" {
		t.Fatalf("full-version ibm repo url = %q, want stream 9 from 9.9.1.0", url)
	}
}

func TestSelectUsesAuthoredContainerImageBase(t *testing.T) {
	cluster := func(distribution, release, base string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: distribution,
			Release:      release,
			Image:        imageSpec(base, ""),
		}}}
	}
	cases := []struct {
		name         string
		distribution string
		release      string
		base         string
		want         string
	}{
		{"ibm omitted base", v1alpha1.StorageCephDistributionIBM, "9", "", ""},
		{"redhat omitted base", v1alpha1.StorageCephDistributionRedHat, "9", "", ""},
		{"oss release name", v1alpha1.StorageCephDistributionOSS, "squid", "", "quay.io/ceph/ceph"},
		{"oss version", v1alpha1.StorageCephDistributionOSS, "19.2.1", "", "quay.io/ceph/ceph"},
		{"ibm authored base", v1alpha1.StorageCephDistributionIBM, "9", "mirror.example.test/ibm-ceph/ceph-9-rhel14", "mirror.example.test/ibm-ceph/ceph-9-rhel14"},
		{"redhat authored base", v1alpha1.StorageCephDistributionRedHat, "9.1", "mirror.example.test/rhceph/rhceph-9-rhel9", "mirror.example.test/rhceph/rhceph-9-rhel9"},
		{"oss authored base", v1alpha1.StorageCephDistributionOSS, "19.2.1", "mirror.example.test/ceph/ceph", "mirror.example.test/ceph/ceph"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := Select(cluster(tc.distribution, tc.release, tc.base), nil, secret.Index{}, "/context/secrets")
			if provider.ImageBase != tc.want {
				t.Fatalf("ImageBase = %q, want %q", provider.ImageBase, tc.want)
			}
			got, present := Vars(provider)["imageBase"]
			if tc.want == "" && present {
				t.Fatalf("imageBase var = %v, want omission", got)
			}
			if tc.want != "" && got != tc.want {
				t.Fatalf("imageBase var = %v, want %q", got, tc.want)
			}
		})
	}
}

func TestImageRepositoryStripsTagAndDigest(t *testing.T) {
	cases := map[string]string{
		"cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:abc": "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9",
		"registry.redhat.io/rhceph/rhceph-10-rhel9:10":  "registry.redhat.io/rhceph/rhceph-10-rhel9",
		"quay.io/ceph/ceph:v19.2.1":                     "quay.io/ceph/ceph",
		"quay.io/ceph/ceph":                             "quay.io/ceph/ceph",
		"registry.example.test:5000/ceph/ceph:v1":       "registry.example.test:5000/ceph/ceph",
	}
	for in, want := range cases {
		if got := ImageRepository(in); got != want {
			t.Fatalf("ImageRepository(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestImageRepositoryPrefixLeavesTheBuildBaseOpen(t *testing.T) {
	prefix, ok := ImageRepositoryPrefix(v1alpha1.StorageCephDistributionRedHat, "10.0", "")
	if !ok || prefix != "registry.redhat.io/rhceph/rhceph-10-rhel" {
		t.Fatalf("ImageRepositoryPrefix = %q, %t", prefix, ok)
	}
	for _, image := range []string{"registry.redhat.io/rhceph/rhceph-10-rhel10", "registry.redhat.io/rhceph/rhceph-10-rhel11"} {
		if !strings.HasPrefix(image, prefix) {
			t.Fatalf("vendor build %q does not match prefix %q", image, prefix)
		}
	}
}

func TestSelectRedHatProviderProjectsEntitlement(t *testing.T) {
	ents := []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhcs"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatCeph,
			RHSM: &v1alpha1.EntitlementRHSM{
				OrganizationRef:   v1alpha1.SecretRef{Name: "redhat-org"},
				ActivationKeyRef:  v1alpha1.SecretRef{Name: "redhat-key"},
				ConnectToInsights: true,
			},
			Registry: &v1alpha1.EntitlementRegistry{
				CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
			},
		},
	}}
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionRedHat,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
	}}}
	provider := Select(cluster, ents, secret.Index{}, "/context/secrets")
	vars := Vars(provider)
	registry := vars["registry"].(map[string]any)
	if registry["url"] != RedHatRegistryURL {
		t.Fatalf("registry url = %v, want %s", registry["url"], RedHatRegistryURL)
	}
	if registry["credentialsPath"] != "/context/secrets/redhat-registry" {
		t.Fatalf("registry credentialsPath = %v", registry["credentialsPath"])
	}
	rhsm := vars["rhsm"].(map[string]any)
	if rhsm["organizationPath"] != "/context/secrets/redhat-org" || rhsm["activationKeyPath"] != "/context/secrets/redhat-key" {
		t.Fatalf("rhsm vars = %#v", rhsm)
	}
	if _, ok := vars["community"]; ok {
		t.Fatalf("redhat provider must not project community vars: %#v", vars["community"])
	}
	if _, ok := rhsm["satellite"]; ok {
		t.Fatalf("public-CDN rhsm must carry no satellite key: %#v", rhsm["satellite"])
	}
	if vars["rhsmManagement"] != v1alpha1.EntitlementRHSMManagementManaged {
		t.Fatalf("rhsmManagement = %v, want managed", vars["rhsmManagement"])
	}
}

func TestSelectExternalRHSMManagementProjectsNoRHSMVars(t *testing.T) {
	ents := []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhcs"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatCeph,
			RHSM: &v1alpha1.EntitlementRHSM{Management: v1alpha1.EntitlementRHSMManagementExternal},
			Registry: &v1alpha1.EntitlementRegistry{
				CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
			},
		},
	}}
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionRedHat,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
	}}}
	vars := Vars(Select(cluster, ents, secret.Index{}, "/context/secrets"))
	if vars["rhsmManagement"] != v1alpha1.EntitlementRHSMManagementExternal {
		t.Fatalf("rhsmManagement = %v, want external", vars["rhsmManagement"])
	}
	if _, ok := vars["rhsm"]; ok {
		t.Fatalf("external rhsm management must project no rhsm vars: %#v", vars["rhsm"])
	}
	registry := vars["registry"].(map[string]any)
	if registry["credentialsPath"] != "/context/secrets/redhat-registry" {
		t.Fatalf("registry vars must survive external rhsm management: %#v", registry)
	}
}

func TestSelectRedHatProviderProjectsSatellite(t *testing.T) {
	ents := []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "rhcs"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeRedHatCeph,
			RHSM: &v1alpha1.EntitlementRHSM{
				OrganizationRef:  v1alpha1.SecretRef{Name: "redhat-org"},
				ActivationKeyRef: v1alpha1.SecretRef{Name: "redhat-key"},
				Satellite: &v1alpha1.EntitlementRHSMSatellite{
					Hostname:       "satellite.corp.example.com",
					ContentBaseURL: "https://satellite.corp.example.com/pulp/content",
					TrustBundleRef: v1alpha1.SecretRef{Name: "corp-satellite-ca"},
				},
			},
			Registry: &v1alpha1.EntitlementRegistry{CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"}},
		},
	}}
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionRedHat,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
	}}}
	rhsm := Vars(Select(cluster, ents, secret.Index{}, "/context/secrets"))["rhsm"].(map[string]any)
	satellite, ok := rhsm["satellite"].(map[string]any)
	if !ok {
		t.Fatalf("day-2 rhsm.satellite missing: %#v", rhsm)
	}
	if satellite["hostname"] != "satellite.corp.example.com" ||
		satellite["contentBaseURL"] != "https://satellite.corp.example.com/pulp/content" ||
		satellite["caPath"] != "/context/secrets/corp-satellite-ca" {
		t.Fatalf("day-2 rhsm.satellite = %#v", satellite)
	}
}

func TestSelectIBMProviderProjectsLicenseAndRegistry(t *testing.T) {
	ents := []v1alpha1.Entitlement{{
		Metadata: v1alpha1.Metadata{Name: "ibm-ceph"},
		Spec: v1alpha1.EntitlementSpec{
			Type: v1alpha1.EntitlementTypeIBMStorageCeph,
			Registry: &v1alpha1.EntitlementRegistry{
				CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
			},
			License: &v1alpha1.EntitlementLicense{Accept: true},
		},
	}}
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionIBM,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "ibm-ceph"},
	}}}
	vars := Vars(Select(cluster, ents, secret.Index{}, "/context/secrets"))
	if _, ok := vars["rhsm"]; ok {
		t.Fatalf("ibm Select alone must project no rhsm; RHEL registration comes from the profile subscription, got %#v", vars["rhsm"])
	}
	registry := vars["registry"].(map[string]any)
	if registry["url"] != IBMRegistryURL {
		t.Fatalf("registry url = %v, want %s", registry["url"], IBMRegistryURL)
	}
	license := vars["license"].(map[string]any)
	if license["accepted"] != true {
		t.Fatalf("license vars = %#v", license)
	}
	candidates, ok := vars["nativeCapabilityCandidates"].(map[string]any)
	if !ok || candidates["cephadmBootstrapLicenseOption"] != ibmCephadmBootstrapLicenseOption || candidates["cephOrchCallHomeConsentToken"] != ibmCephOrchCallHomeConsentToken {
		t.Fatalf("IBM native capability candidates = %#v", vars["nativeCapabilityCandidates"])
	}
}

func TestSelectProjectsRedHatReposPerDistribution(t *testing.T) {
	rhel := func(suffix string) string {
		return "rhel-{{ ansible_distribution_major_version }}-for-x86_64-" + suffix
	}
	cases := []struct {
		distribution string
		want         []string
	}{
		{v1alpha1.StorageCephDistributionRedHat, []string{
			rhel("baseos-rpms"),
			rhel("appstream-rpms"),
			"rhceph-9-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms",
		}},
		{v1alpha1.StorageCephDistributionIBM, []string{
			rhel("baseos-rpms"),
			rhel("appstream-rpms"),
		}},
	}
	for _, tc := range cases {
		cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: tc.distribution,
			Release:      "9.1",
		}}}
		repos := Select(cluster, nil, secret.Index{}, "/context/secrets").Repository.RedHatRepos
		if len(repos) != len(tc.want) {
			t.Fatalf("%s redhatRepos = %#v, want %#v", tc.distribution, repos, tc.want)
		}
		for i := range tc.want {
			if repos[i] != tc.want[i] {
				t.Fatalf("%s redhatRepos[%d] = %q, want %q", tc.distribution, i, repos[i], tc.want[i])
			}
		}
		repo := Vars(Select(cluster, nil, secret.Index{}, "/context/secrets"))["repository"].(map[string]any)
		if _, ok := repo["redhatRepos"].([]string); !ok {
			t.Fatalf("%s Vars repository.redhatRepos missing or wrong type: %#v", tc.distribution, repo)
		}
	}
	oss := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}}}
	if repos := Select(oss, nil, secret.Index{}, "/context/secrets").Repository.RedHatRepos; len(repos) != 0 {
		t.Fatalf("oss redhatRepos = %#v, want none", repos)
	}
}

func TestSelectIBMSubscriptionPackageSourceUsesDeclaredRepos(t *testing.T) {
	cluster := func(packages *v1alpha1.StorageCephIBMPackagesSpec) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionIBM,
			Release:      "9.9.1.0",
			IBM:          &v1alpha1.StorageCephIBMSpec{CallHome: v1alpha1.StorageCephIBMCallHomeDisabled, Packages: packages},
		}}}
	}

	subscription := Select(cluster(&v1alpha1.StorageCephIBMPackagesSpec{
		Source:            v1alpha1.StorageCephIBMPackageSourceSubscription,
		SubscriptionRepos: []string{"Org_IBM_ibm-storage-ceph-9"},
	}), nil, secret.Index{}, "/context/secrets")
	if subscription.Repository.IBMRepoURL != "" {
		t.Fatalf("subscription package source must suppress the vendor repo URL, got %q", subscription.Repository.IBMRepoURL)
	}
	repos := subscription.Repository.RedHatRepos
	if len(repos) != 3 || repos[len(repos)-1] != "Org_IBM_ibm-storage-ceph-9" {
		t.Fatalf("subscription package source repos = %#v, want base repos plus the declared label", repos)
	}
	repo := Vars(subscription)["repository"].(map[string]any)
	if _, ok := repo["ibmRepoURL"]; ok {
		t.Fatalf("subscription package source vars must omit ibmRepoURL: %#v", repo)
	}

	vendor := Select(cluster(&v1alpha1.StorageCephIBMPackagesSpec{Source: v1alpha1.StorageCephIBMPackageSourceVendor}), nil, secret.Index{}, "/context/secrets")
	if vendor.Repository.IBMRepoURL == "" {
		t.Fatal("explicit vendor package source must keep the vendor repo URL")
	}
	absent := Select(cluster(nil), nil, secret.Index{}, "/context/secrets")
	if absent.Repository.IBMRepoURL != vendor.Repository.IBMRepoURL {
		t.Fatalf("absent packages block = %q, explicit vendor = %q; both must fetch the vendor repo", absent.Repository.IBMRepoURL, vendor.Repository.IBMRepoURL)
	}
	if len(absent.Repository.RedHatRepos) != 2 {
		t.Fatalf("absent packages block repos = %#v, want base repos only", absent.Repository.RedHatRepos)
	}
}

func TestSelectDerivesPackageSourcesForAReleaseNewerThanBootwright(t *testing.T) {
	redhat := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution: v1alpha1.StorageCephDistributionRedHat,
		Release:      "10.0",
	}}}
	repos := Select(redhat, nil, secret.Index{}, "/context/secrets").Repository.RedHatRepos
	want := "rhceph-10-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms"
	if len(repos) == 0 || repos[len(repos)-1] != want {
		t.Fatalf("redhat 10.0 tools repo = %#v, want %q", repos, want)
	}

	ibm := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionIBM,
		Release:        "42.7.3.1",
		PackageVersion: "31.7.3-456.el14",
		Cephadm: v1alpha1.StorageCephadmSpec{Ansible: &v1alpha1.StorageCephadmAnsible{
			PackageVersion: "17.3.2-9.el14",
		}},
		Image: &v1alpha1.StorageCephImageSpec{
			Base:    "cp.icr.io/cp/ibm-ceph/ceph-42-rhel14",
			Version: "v42.7-12345",
		},
		IBM: &v1alpha1.StorageCephIBMSpec{CallHome: v1alpha1.StorageCephIBMCallHomeDisabled},
	}}}
	provider := Select(ibm, nil, secret.Index{}, "/context/secrets")
	wantURL := "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-42-rhel-{{ ansible_distribution_major_version }}.repo"
	if provider.Repository.IBMRepoURL != wantURL {
		t.Fatalf("future IBM repo URL = %q, want %q", provider.Repository.IBMRepoURL, wantURL)
	}
	if provider.ImageBase != ibm.Spec.Ceph.Image.Base || provider.Image != ibm.Spec.Ceph.Image.Base+":"+ibm.Spec.Ceph.Image.Version {
		t.Fatalf("future IBM image coordinates were rewritten: base=%q image=%q", provider.ImageBase, provider.Image)
	}
	if provider.CephadmPackageSpec != "cephadm-31.7.3-456.el14" || provider.CephadmAnsiblePackageSpec != "cephadm-ansible-17.3.2-9.el14" {
		t.Fatalf("future IBM native package coordinates were rewritten: cephadm=%q cephadm-ansible=%q", provider.CephadmPackageSpec, provider.CephadmAnsiblePackageSpec)
	}
	if provider.NativeCapabilityCandidates.CephadmBootstrapLicenseOption != ibmCephadmBootstrapLicenseOption || provider.NativeCapabilityCandidates.CephOrchCallHomeConsentToken != ibmCephOrchCallHomeConsentToken {
		t.Fatalf("future IBM release lost live native capability candidates: %#v", provider.NativeCapabilityCandidates)
	}
	policy := Vars(provider)["artifactPolicy"].(map[string]any)
	if policy["packagePin"] != string(ArtifactPinRequired) || policy["cephadmAnsiblePackagePin"] != string(ArtifactPinRequired) || policy["nativeParityMode"] != NativeParityCephVersion {
		t.Fatalf("future IBM tuple lost distribution artifact policy: %#v", policy)
	}
	runtimeOS := Vars(provider)["runtimeOS"].(map[string]any)
	if runtimeOS["family"] != "rhel" || len(runtimeOS) != 1 {
		t.Fatalf("runtimeOS must carry the implemented family and nothing else, got %#v", runtimeOS)
	}
}

func TestUpstreamCephMajorDerivesOnlyWhatTheReleaseActuallyNames(t *testing.T) {
	cases := []struct {
		distribution string
		release      string
		want         int
		ok           bool
	}{
		{distribution: v1alpha1.StorageCephDistributionOSS, release: "reef", want: 18, ok: true},
		{distribution: v1alpha1.StorageCephDistributionOSS, release: "squid", want: 19, ok: true},
		{distribution: v1alpha1.StorageCephDistributionOSS, release: "tentacle", want: 20, ok: true},
		{distribution: v1alpha1.StorageCephDistributionOSS, release: "19.2.3", want: 19, ok: true},
		{distribution: v1alpha1.StorageCephDistributionOSS, release: ""},
		{distribution: v1alpha1.StorageCephDistributionOSS, release: "umbriel"},
		{distribution: v1alpha1.StorageCephDistributionOSS, release: "not a release"},
		{distribution: v1alpha1.StorageCephDistributionIBM, release: "9.9.1.0"},
		{distribution: v1alpha1.StorageCephDistributionRedHat, release: "9.1"},
		{distribution: "nonesuch", release: "squid"},
	}
	for _, tc := range cases {
		got, ok := UpstreamCephMajor(tc.distribution, tc.release)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("UpstreamCephMajor(%q, %q) = (%d, %v), want (%d, %v)", tc.distribution, tc.release, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := UpstreamCephMajor(v1alpha1.StorageCephDistributionIBM, "9.9.1.0"); ok {
		t.Fatal("a vendor product version is not an upstream Ceph version; deriving a major from it would invent a floor the vendor never promised")
	}
}
