package cephprovider

import (
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
	cluster := v1alpha1.StorageCluster{
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{},
		},
	}
	provider := Select(cluster, nil, secret.Index{}, "/context/secrets")
	if provider.Distribution != v1alpha1.StorageCephDistributionOSS {
		t.Fatalf("distribution = %q, want oss", provider.Distribution)
	}
	if provider.RequiresRHSM || provider.RequiresRegistry || provider.RequiresLicense {
		t.Fatalf("OSS provider requires vendor material: %#v", provider)
	}
	if provider.Community.Version != v1alpha1.StorageCephCommunityDefaultRelease {
		t.Fatalf("community version = %q, want default %q", provider.Community.Version, v1alpha1.StorageCephCommunityDefaultRelease)
	}
	community, ok := Vars(provider)["community"].(map[string]any)
	if !ok {
		t.Fatalf("oss provider vars missing community map: %#v", Vars(provider))
	}
	if community["version"] != v1alpha1.StorageCephCommunityDefaultRelease {
		t.Fatalf("community vars version = %v, want default", community["version"])
	}
	if provider.Image != "quay.io/ceph/ceph:v"+v1alpha1.StorageCephCommunityDefaultRelease {
		t.Fatalf("default image = %q, want exact release image", provider.Image)
	}
	if _, hasMirror := community["mirror"]; hasMirror {
		t.Fatalf("community vars must omit mirror when unset: %#v", community)
	}
}

func TestResolveReleaseUsesDistributionVersionMatrix(t *testing.T) {
	cases := []struct {
		distribution string
		authored     string
		wantValue    string
		wantStream   string
		wantRHEL     string
		wantOK       bool
	}{
		{v1alpha1.StorageCephDistributionRedHat, "", "9.1", "9", "9.8", true},
		{v1alpha1.StorageCephDistributionRedHat, "9", "9.1", "9", "10.2", true},
		{v1alpha1.StorageCephDistributionRedHat, "9.0", "9.0", "9", "9.6", true},
		{v1alpha1.StorageCephDistributionIBM, "", "9.9.1", "9", "9.8", true},
		{v1alpha1.StorageCephDistributionIBM, "9", "9.9.1", "9", "10.2", true},
		{v1alpha1.StorageCephDistributionIBM, "9.9.0", "", "", "", false},
		{v1alpha1.StorageCephDistributionRedHat, "10", "", "", "", false},
	}
	for _, tc := range cases {
		release, ok := ResolveRelease(tc.distribution, tc.authored)
		if ok != tc.wantOK {
			t.Fatalf("ResolveRelease(%q, %q) ok = %t, want %t", tc.distribution, tc.authored, ok, tc.wantOK)
		}
		if !ok {
			continue
		}
		if release.Value != tc.wantValue || release.Stream != tc.wantStream {
			t.Fatalf("ResolveRelease(%q, %q) = %#v", tc.distribution, tc.authored, release)
		}
		found := false
		for _, version := range release.RuntimeOS.ExactVersions {
			found = found || version == tc.wantRHEL
		}
		if !found {
			t.Fatalf("ResolveRelease(%q, %q) RHEL versions = %v, want %q", tc.distribution, tc.authored, release.RuntimeOS.ExactVersions, tc.wantRHEL)
		}
	}
}

func TestResolveReleaseUsesActiveOSSSeries(t *testing.T) {
	cases := []struct {
		release string
		ok      bool
	}{
		{"", true},
		{"tentacle", true},
		{"20.2.2", true},
		{"squid", true},
		{"19.2.1", true},
		{"reef", false},
		{"18.2.7", false},
		{"bananas", false},
	}
	for _, tc := range cases {
		resolved, ok := ResolveRelease(v1alpha1.StorageCephDistributionOSS, tc.release)
		if ok != tc.ok {
			t.Fatalf("ResolveRelease(oss, %q) = %#v, %t; want ok=%t", tc.release, resolved, ok, tc.ok)
		}
	}
}

func TestSelectIBMProviderProjectsCallHomeIntent(t *testing.T) {
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution: v1alpha1.StorageCephDistributionIBM,
		Release:      "9.9.1",
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

func TestSelectOSSProviderClassifiesVersionAndDerivesImage(t *testing.T) {
	oss := func(release, image string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionOSS,
			Release:      release,
			Image:        image,
		}}}
	}

	provider := Select(oss("19.2.1", ""), nil, secret.Index{}, "/context/secrets")
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

	nameProvider := Select(oss("squid", ""), nil, secret.Index{}, "/context/secrets")
	if nameProvider.Community.Release != "squid" || nameProvider.Image != "" {
		t.Fatalf("name release derived an image: %#v image=%q", nameProvider.Community, nameProvider.Image)
	}
	if _, ok := Vars(nameProvider)["image"]; ok {
		t.Fatalf("name release must omit image var")
	}

	pinned := Select(oss("19.2.1", "quay.io/ceph/ceph@sha256:abc"), nil, secret.Index{}, "/context/secrets")
	if pinned.Image != "quay.io/ceph/ceph@sha256:abc" {
		t.Fatalf("explicit image not honored: %q", pinned.Image)
	}
}

func TestSelectSubscriptionProviderResolvesStreamAndImage(t *testing.T) {
	redhat := func(release, image string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionRedHat,
			Release:      release,
			Image:        image,
		}}}
	}

	def := Select(redhat("", ""), nil, secret.Index{}, "/context/secrets").Repository.RedHatRepos
	if got := def[len(def)-1]; got != "rhceph-9-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms" {
		t.Fatalf("default tools repo = %q", got)
	}

	repos := Select(redhat("9.0", "registry.redhat.io/rhceph/rhceph-9-rhel9:9"), nil, secret.Index{}, "/context/secrets").Repository.RedHatRepos
	if got := repos[len(repos)-1]; got != "rhceph-9-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms" {
		t.Fatalf("stream tools repo = %q, want rhceph-9-tools", got)
	}
	provider := Select(redhat("9.0", "registry.redhat.io/rhceph/rhceph-9-rhel9:9"), nil, secret.Index{}, "/context/secrets")
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
	if url := Select(ibm(""), nil, secret.Index{}, "/context/secrets").Repository.IBMRepoURL; url != "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-9-rhel-{{ ansible_distribution_major_version }}.repo" {
		t.Fatalf("default ibm repo url = %q", url)
	}
	if url := Select(ibm("9"), nil, secret.Index{}, "/context/secrets").Repository.IBMRepoURL; url != "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-9-rhel-{{ ansible_distribution_major_version }}.repo" {
		t.Fatalf("stream alias ibm repo url = %q, want stream 9", url)
	}
	if url := Select(ibm("9.9.1"), nil, secret.Index{}, "/context/secrets").Repository.IBMRepoURL; url != "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-9-rhel-{{ ansible_distribution_major_version }}.repo" {
		t.Fatalf("full-version ibm repo url = %q, want stream 9 from 9.9.1", url)
	}
}

func TestSelectResolvesContainerImageBase(t *testing.T) {
	cluster := func(distribution, release, image string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: distribution,
			Release:      release,
			Image:        image,
		}}}
	}
	cases := []struct {
		name         string
		distribution string
		release      string
		image        string
		want         string
	}{
		{"ibm default stream", v1alpha1.StorageCephDistributionIBM, "", "", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9"},
		{"ibm explicit stream", v1alpha1.StorageCephDistributionIBM, "9", "", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9"},
		{"ibm full product version", v1alpha1.StorageCephDistributionIBM, "9.9.1", "", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9"},
		{"redhat default stream", v1alpha1.StorageCephDistributionRedHat, "", "", "registry.redhat.io/rhceph/rhceph-9-rhel9"},
		{"oss release name", v1alpha1.StorageCephDistributionOSS, "squid", "", "quay.io/ceph/ceph"},
		{"oss version", v1alpha1.StorageCephDistributionOSS, "19.2.1", "", "quay.io/ceph/ceph"},
		{"ibm pinned digest", v1alpha1.StorageCephDistributionIBM, "", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:abc", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9"},
		{"redhat pinned tag", v1alpha1.StorageCephDistributionRedHat, "9.1", "registry.redhat.io/rhceph/rhceph-9-rhel9:9", "registry.redhat.io/rhceph/rhceph-9-rhel9"},
		{"oss pinned tag", v1alpha1.StorageCephDistributionOSS, "19.2.1", "quay.io/ceph/ceph:v19.2.1", "quay.io/ceph/ceph"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := Select(cluster(tc.distribution, tc.release, tc.image), nil, secret.Index{}, "/context/secrets")
			if provider.ImageBase != tc.want {
				t.Fatalf("ImageBase = %q, want %q", provider.ImageBase, tc.want)
			}
			if got := Vars(provider)["imageBase"]; got != tc.want {
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

func TestExpectedImageRepositoryUsesDistributionStreamAndMirrorRoot(t *testing.T) {
	cases := []struct {
		name         string
		distribution string
		release      string
		registry     string
		want         string
		wantOK       bool
	}{
		{
			name:         "redhat current default",
			distribution: v1alpha1.StorageCephDistributionRedHat,
			want:         "registry.redhat.io/rhceph/rhceph-9-rhel9",
			wantOK:       true,
		},
		{
			name:         "redhat exact release mirror",
			distribution: v1alpha1.StorageCephDistributionRedHat,
			release:      "9.0",
			registry:     "mirror.example.test/vendor",
			want:         "mirror.example.test/vendor/rhceph/rhceph-9-rhel9",
			wantOK:       true,
		},
		{
			name:         "ibm current alias",
			distribution: v1alpha1.StorageCephDistributionIBM,
			release:      "9",
			want:         "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9",
			wantOK:       true,
		},
		{
			name:         "ibm mirror",
			distribution: v1alpha1.StorageCephDistributionIBM,
			release:      "9.9.1",
			registry:     "mirror.example.test/ibm",
			want:         "mirror.example.test/ibm/ibm-ceph/ceph-9-rhel9",
			wantOK:       true,
		},
		{
			name:         "unsupported release",
			distribution: v1alpha1.StorageCephDistributionIBM,
			release:      "9.9.0",
			wantOK:       false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExpectedImageRepository(tc.distribution, tc.release, tc.registry)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("ExpectedImageRepository(%q, %q, %q) = %q, %t; want %q, %t", tc.distribution, tc.release, tc.registry, got, ok, tc.want, tc.wantOK)
			}
		})
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
