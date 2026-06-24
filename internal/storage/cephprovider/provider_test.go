package cephprovider

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestSelectDefaultsToOSSProvider(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{},
		},
	}
	provider := Select(cluster, nil, "/context/secrets")
	if provider.Distribution != v1alpha1.StorageCephDistributionOSS {
		t.Fatalf("distribution = %q, want oss", provider.Distribution)
	}
	if provider.RequiresRHSM || provider.RequiresRegistry || provider.RequiresLicense {
		t.Fatalf("OSS provider requires vendor material: %#v", provider)
	}
	if provider.Community.Release != v1alpha1.StorageCephCommunityDefaultRelease {
		t.Fatalf("community release = %q, want default %q", provider.Community.Release, v1alpha1.StorageCephCommunityDefaultRelease)
	}
	community, ok := Vars(provider)["community"].(map[string]any)
	if !ok {
		t.Fatalf("oss provider vars missing community map: %#v", Vars(provider))
	}
	if community["release"] != v1alpha1.StorageCephCommunityDefaultRelease {
		t.Fatalf("community vars release = %v, want default", community["release"])
	}
	if _, hasMirror := community["mirror"]; hasMirror {
		t.Fatalf("community vars must omit mirror when unset: %#v", community)
	}
}

func TestSelectOSSProviderHonorsCommunityOverride(t *testing.T) {
	cluster := v1alpha1.StorageCluster{
		Spec: v1alpha1.StorageClusterSpec{
			Ceph: &v1alpha1.StorageClusterCephSpec{
				Distribution: v1alpha1.StorageCephDistributionOSS,
				Release:      "reef",
				Community: &v1alpha1.StorageCephCommunitySpec{
					Mirror: "https://mirror.example.test/ceph",
				},
			},
		},
	}
	provider := Select(cluster, nil, "/context/secrets")
	if provider.Community.Release != "reef" || provider.Community.Mirror != "https://mirror.example.test/ceph" {
		t.Fatalf("community override not projected: %#v", provider.Community)
	}
	community := Vars(provider)["community"].(map[string]any)
	if community["release"] != "reef" || community["mirror"] != "https://mirror.example.test/ceph" {
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

	// A full x.y.z release pins the repository as a version and derives the
	// matching container image.
	provider := Select(oss("19.2.1", ""), nil, "/context/secrets")
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

	// A release name leaves the image unpinned (floats).
	nameProvider := Select(oss("squid", ""), nil, "/context/secrets")
	if nameProvider.Community.Release != "squid" || nameProvider.Image != "" {
		t.Fatalf("name release derived an image: %#v image=%q", nameProvider.Community, nameProvider.Image)
	}
	if _, ok := Vars(nameProvider)["image"]; ok {
		t.Fatalf("name release must omit image var")
	}

	// An explicit image overrides the derived one.
	pinned := Select(oss("19.2.1", "quay.io/ceph/ceph@sha256:abc"), nil, "/context/secrets")
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

	// Default stream (release unset) keeps rhceph-9-tools.
	def := Select(redhat("", ""), nil, "/context/secrets").Repository.RedHatRepos
	if got := def[len(def)-1]; got != "rhceph-9-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms" {
		t.Fatalf("default tools repo = %q", got)
	}

	// An explicit stream selects rhceph-<N>-tools and is honored from a major.minor.
	repos := Select(redhat("10.1", "registry.redhat.io/rhceph/rhceph-10-rhel9:10"), nil, "/context/secrets").Repository.RedHatRepos
	if got := repos[len(repos)-1]; got != "rhceph-10-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms" {
		t.Fatalf("stream tools repo = %q, want rhceph-10-tools", got)
	}
	provider := Select(redhat("10.1", "registry.redhat.io/rhceph/rhceph-10-rhel9:10"), nil, "/context/secrets")
	if provider.Image != "registry.redhat.io/rhceph/rhceph-10-rhel9:10" {
		t.Fatalf("explicit image not honored: %q", provider.Image)
	}
	if Vars(provider)["image"] != "registry.redhat.io/rhceph/rhceph-10-rhel9:10" {
		t.Fatalf("image var missing for redhat: %#v", Vars(provider))
	}

	// IBM stream selects the matching vendor .repo URL.
	ibm := func(release string) v1alpha1.StorageCluster {
		return v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
			Distribution: v1alpha1.StorageCephDistributionIBM,
			Release:      release,
		}}}
	}
	if url := Select(ibm(""), nil, "/context/secrets").Repository.IBMRepoURL; url != "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-9-rhel-9.repo" {
		t.Fatalf("default ibm repo url = %q", url)
	}
	if url := Select(ibm("10"), nil, "/context/secrets").Repository.IBMRepoURL; url != "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-10-rhel-9.repo" {
		t.Fatalf("stream ibm repo url = %q, want stream 10", url)
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
		// Unset image derives the vendor/upstream repository from the stream.
		{"ibm default stream", v1alpha1.StorageCephDistributionIBM, "", "", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9"},
		{"ibm explicit stream", v1alpha1.StorageCephDistributionIBM, "10", "", "cp.icr.io/cp/ibm-ceph/ceph-10-rhel9"},
		{"redhat default stream", v1alpha1.StorageCephDistributionRedHat, "", "", "registry.redhat.io/rhceph/rhceph-9-rhel9"},
		{"oss release name", v1alpha1.StorageCephDistributionOSS, "squid", "", "quay.io/ceph/ceph"},
		{"oss version", v1alpha1.StorageCephDistributionOSS, "19.2.1", "", "quay.io/ceph/ceph"},
		// An explicit image pins the base to its repository, tag or digest stripped.
		{"ibm pinned digest", v1alpha1.StorageCephDistributionIBM, "", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:abc", "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9"},
		{"redhat pinned tag", v1alpha1.StorageCephDistributionRedHat, "10.1", "registry.redhat.io/rhceph/rhceph-10-rhel9:10", "registry.redhat.io/rhceph/rhceph-10-rhel9"},
		{"oss pinned tag", v1alpha1.StorageCephDistributionOSS, "19.2.1", "quay.io/ceph/ceph:v19.2.1", "quay.io/ceph/ceph"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := Select(cluster(tc.distribution, tc.release, tc.image), nil, "/context/secrets")
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
		// A registry host:port is preserved; only the final segment's tag is cut.
		"registry.example.test:5000/ceph/ceph:v1": "registry.example.test:5000/ceph/ceph",
	}
	for in, want := range cases {
		if got := imageRepository(in); got != want {
			t.Fatalf("imageRepository(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelectRedHatProviderProjectsEntitlement(t *testing.T) {
	env := &v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{Entitlements: []v1alpha1.EnvironmentEntitlement{{
		Name:     "rhcs",
		Provider: v1alpha1.EntitlementProviderRedHat,
		Product:  v1alpha1.EntitlementProductCeph,
		RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
			OrganizationRef:   v1alpha1.SecretRef{Name: "redhat-org"},
			ActivationKeyRef:  v1alpha1.SecretRef{Name: "redhat-key"},
			ConnectToInsights: true,
		},
		Registry: &v1alpha1.EnvironmentEntitlementRegistry{
			CredentialsRef: v1alpha1.SecretRef{Name: "redhat-registry"},
		},
	}}}}
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionRedHat,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "rhcs"},
	}}}
	provider := Select(cluster, env, "/context/secrets")
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
}

func TestSelectIBMProviderProjectsLicenseAndRegistry(t *testing.T) {
	// The ibm item carries registry + license only; its RHSM is sourced from the
	// referenced redhat/rhel item, yet the projected vars are identical.
	env := &v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{Entitlements: []v1alpha1.EnvironmentEntitlement{
		{
			Name:     "rhel",
			Provider: v1alpha1.EntitlementProviderRedHat,
			Product:  v1alpha1.EntitlementProductRHEL,
			RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
				OrganizationRef:  v1alpha1.SecretRef{Name: "ibm-org"},
				ActivationKeyRef: v1alpha1.SecretRef{Name: "ibm-key"},
			},
		},
		{
			Name:               "ibm-ceph",
			Provider:           v1alpha1.EntitlementProviderIBM,
			Product:            v1alpha1.EntitlementProductIBMStorageCeph,
			RHELEntitlementRef: v1alpha1.LocalObjectReference{Name: "rhel"},
			Registry: &v1alpha1.EnvironmentEntitlementRegistry{
				CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
			},
			License: &v1alpha1.EnvironmentEntitlementLicense{Accept: true},
		},
	}}}
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionIBM,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "ibm-ceph"},
	}}}
	vars := Vars(Select(cluster, env, "/context/secrets"))
	rhsm := vars["rhsm"].(map[string]any)
	if rhsm["organizationPath"] != "/context/secrets/ibm-org" || rhsm["activationKeyPath"] != "/context/secrets/ibm-key" {
		t.Fatalf("rhsm vars (via rhelEntitlementRef) = %#v", rhsm)
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
		repos := Select(cluster, nil, "/context/secrets").Repository.RedHatRepos
		if len(repos) != len(tc.want) {
			t.Fatalf("%s redhatRepos = %#v, want %#v", tc.distribution, repos, tc.want)
		}
		for i := range tc.want {
			if repos[i] != tc.want[i] {
				t.Fatalf("%s redhatRepos[%d] = %q, want %q", tc.distribution, i, repos[i], tc.want[i])
			}
		}
		repo := Vars(Select(cluster, nil, "/context/secrets"))["repository"].(map[string]any)
		if _, ok := repo["redhatRepos"].([]string); !ok {
			t.Fatalf("%s Vars repository.redhatRepos missing or wrong type: %#v", tc.distribution, repo)
		}
	}
	oss := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{}}}
	if repos := Select(oss, nil, "/context/secrets").Repository.RedHatRepos; len(repos) != 0 {
		t.Fatalf("oss redhatRepos = %#v, want none", repos)
	}
}
