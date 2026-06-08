package cephprovider

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
)

const (
	RedHatRegistryURL = "registry.redhat.io"
	IBMRegistryURL    = "cp.icr.io/cp"
)

const (
	rhelBaseOSRepo    = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-baseos-rpms"
	rhelAppStreamRepo = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-appstream-rpms"
)

type Provider struct {
	Distribution         string
	Entitlement          entitlements.Resolved
	RequiresRHSM         bool
	RequiresRegistry     bool
	RequiresLicense      bool
	PrerequisitePackages []string
	CephadmPackage       string
	CephCommonPackage    string
	Community            Community
	Repository           Repository
	RuntimeOS            RuntimeOS
}

// Community describes the upstream package source for the oss distribution.
// It is unset for the redhat and ibm distributions.
type Community struct {
	Release string
	Mirror  string
}

type Repository struct {
	RedHatRepos []string
	IBMRepoURL  string
}

type RuntimeOS struct {
	Family         string
	MajorVersions  []string
	ExactVersions  []string
	ManagedMessage string
}

func Distribution(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.Distribution == "" {
		return v1alpha1.StorageCephDistributionOSS
	}
	return cluster.Spec.Ceph.Distribution
}

// communitySource resolves the upstream package source for the oss
// distribution, defaulting the release when the operator left it unset.
func communitySource(cluster v1alpha1.StorageCluster) Community {
	out := Community{Release: v1alpha1.StorageCephCommunityDefaultRelease}
	if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.Community == nil {
		return out
	}
	if release := cluster.Spec.Ceph.Community.Release; release != "" {
		out.Release = release
	}
	out.Mirror = cluster.Spec.Ceph.Community.Mirror
	return out
}

func Select(cluster v1alpha1.StorageCluster, env *v1alpha1.Environment, secretsDir string) Provider {
	distribution := Distribution(cluster)
	provider := Provider{
		Distribution:         distribution,
		PrerequisitePackages: []string{"firewalld", "lvm2", "podman", "chrony"},
		CephadmPackage:       "cephadm",
		CephCommonPackage:    "ceph-common",
		RuntimeOS: RuntimeOS{
			Family: "linux",
		},
	}
	switch distribution {
	case v1alpha1.StorageCephDistributionOSS:
		provider.Community = communitySource(cluster)
	case v1alpha1.StorageCephDistributionRedHat:
		provider.RequiresRHSM = true
		provider.RequiresRegistry = true
		provider.Repository.RedHatRepos = []string{
			rhelBaseOSRepo,
			rhelAppStreamRepo,
			"rhceph-9-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms",
		}
		provider.RuntimeOS = RuntimeOS{
			Family:         "rhel",
			ExactVersions:  []string{"9.6", "9.7", "10", "10.0", "10.1"},
			ManagedMessage: "Red Hat Ceph Storage 9 requires RHEL 9.6, 9.7, 10, or 10.1 on storage nodes",
		}
		if cluster.Spec.Ceph != nil {
			provider.Entitlement, _ = entitlements.Resolve(env, cluster.Spec.Ceph.EntitlementRef.Name, RedHatRegistryURL, secretsDir)
		}
	case v1alpha1.StorageCephDistributionIBM:
		provider.RequiresRHSM = true
		provider.RequiresRegistry = true
		provider.RequiresLicense = true
		provider.Repository.RedHatRepos = []string{rhelBaseOSRepo, rhelAppStreamRepo}
		provider.Repository.IBMRepoURL = "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-9-rhel-9.repo"
		provider.RuntimeOS = RuntimeOS{
			Family:         "rhel",
			ExactVersions:  []string{"9.6", "9.7", "10", "10.0", "10.1"},
			ManagedMessage: "IBM Storage Ceph 9 requires RHEL 9.6, 9.7, 10, or 10.1 on storage nodes",
		}
		if cluster.Spec.Ceph != nil {
			provider.Entitlement, _ = entitlements.Resolve(env, cluster.Spec.Ceph.EntitlementRef.Name, IBMRegistryURL, secretsDir)
		}
	}
	return provider
}

func Vars(provider Provider) map[string]any {
	out := map[string]any{
		"name":                 provider.Distribution,
		"distribution":         provider.Distribution,
		"requiresRHSM":         provider.RequiresRHSM,
		"requiresRegistry":     provider.RequiresRegistry,
		"requiresLicense":      provider.RequiresLicense,
		"prerequisitePackages": append([]string(nil), provider.PrerequisitePackages...),
		"cephadmPackage":       provider.CephadmPackage,
		"cephCommonPackage":    provider.CephCommonPackage,
	}
	if provider.Community.Release != "" {
		community := map[string]any{"release": provider.Community.Release}
		if provider.Community.Mirror != "" {
			community["mirror"] = provider.Community.Mirror
		}
		out["community"] = community
	}
	if provider.Entitlement.Name != "" {
		out["entitlement"] = map[string]any{
			"name":     provider.Entitlement.Name,
			"provider": provider.Entitlement.Provider,
			"product":  provider.Entitlement.Product,
		}
	}
	if len(provider.Repository.RedHatRepos) > 0 || provider.Repository.IBMRepoURL != "" {
		repo := map[string]any{}
		if len(provider.Repository.RedHatRepos) > 0 {
			repo["redhatRepos"] = append([]string(nil), provider.Repository.RedHatRepos...)
		}
		if provider.Repository.IBMRepoURL != "" {
			repo["ibmRepoURL"] = provider.Repository.IBMRepoURL
		}
		out["repository"] = repo
	}
	if provider.RuntimeOS.Family != "" {
		os := map[string]any{"family": provider.RuntimeOS.Family}
		if len(provider.RuntimeOS.MajorVersions) > 0 {
			os["majorVersions"] = append([]string(nil), provider.RuntimeOS.MajorVersions...)
		}
		if len(provider.RuntimeOS.ExactVersions) > 0 {
			os["exactVersions"] = append([]string(nil), provider.RuntimeOS.ExactVersions...)
		}
		if provider.RuntimeOS.ManagedMessage != "" {
			os["message"] = provider.RuntimeOS.ManagedMessage
		}
		out["runtimeOS"] = os
	}
	if provider.Entitlement.RHSM.OrganizationPath != "" || provider.Entitlement.RHSM.ActivationKeyPath != "" {
		out["rhsm"] = map[string]any{
			"organizationPath":  provider.Entitlement.RHSM.OrganizationPath,
			"activationKeyPath": provider.Entitlement.RHSM.ActivationKeyPath,
			"connectToInsights": provider.Entitlement.RHSM.ConnectToInsights,
		}
	}
	if provider.Entitlement.Registry.URL != "" || provider.Entitlement.Registry.CredentialsPath != "" || provider.Entitlement.Registry.TrustBundlePath != "" {
		registry := map[string]any{}
		if provider.Entitlement.Registry.URL != "" {
			registry["url"] = provider.Entitlement.Registry.URL
		}
		if provider.Entitlement.Registry.CredentialsPath != "" {
			registry["credentialsPath"] = provider.Entitlement.Registry.CredentialsPath
		}
		if provider.Entitlement.Registry.TrustBundlePath != "" {
			registry["trustBundlePath"] = provider.Entitlement.Registry.TrustBundlePath
		}
		out["registry"] = registry
	}
	if provider.RequiresLicense || provider.Entitlement.License.Accepted {
		out["license"] = map[string]any{
			"accepted": provider.Entitlement.License.Accepted,
		}
	}
	return out
}
