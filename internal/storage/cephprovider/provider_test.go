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
}

func TestSelectIBMProviderProjectsLicenseAndRegistry(t *testing.T) {
	env := &v1alpha1.Environment{Spec: v1alpha1.EnvironmentSpec{Entitlements: []v1alpha1.EnvironmentEntitlement{{
		Name:     "ibm-ceph",
		Provider: v1alpha1.EntitlementProviderIBM,
		Product:  v1alpha1.EntitlementProductIBMStorageCeph,
		RHSM: &v1alpha1.EnvironmentEntitlementRHSM{
			OrganizationRef:  v1alpha1.SecretRef{Name: "ibm-org"},
			ActivationKeyRef: v1alpha1.SecretRef{Name: "ibm-key"},
		},
		Registry: &v1alpha1.EnvironmentEntitlementRegistry{
			CredentialsRef: v1alpha1.SecretRef{Name: "ibm-registry"},
		},
		License: &v1alpha1.EnvironmentEntitlementLicense{Accept: true},
	}}}}
	cluster := v1alpha1.StorageCluster{Spec: v1alpha1.StorageClusterSpec{Ceph: &v1alpha1.StorageClusterCephSpec{
		Distribution:   v1alpha1.StorageCephDistributionIBM,
		EntitlementRef: v1alpha1.LocalObjectReference{Name: "ibm-ceph"},
	}}}
	vars := Vars(Select(cluster, env, "/context/secrets"))
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
