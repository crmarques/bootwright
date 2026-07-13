package cephprovider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

const (
	RedHatRegistryURL = "registry.redhat.io"
	IBMRegistryURL    = "cp.icr.io/cp"

	ossImageRepository = "quay.io/ceph/ceph"

	ibmStorageCephRepoTemplate = "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-%s-rhel-{{ ansible_distribution_major_version }}.repo"

	ibmImageBaseTemplate    = "cp.icr.io/cp/ibm-ceph/ceph-%s-rhel9"
	rhcephImageBaseTemplate = "registry.redhat.io/rhceph/rhceph-%s-rhel9"
)

const (
	rhelBaseOSRepo          = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-baseos-rpms"
	rhelAppStreamRepo       = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-appstream-rpms"
	rhcephToolsRepoTemplate = "rhceph-%s-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms"
)

var ossUpstreamVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type distributionDef struct {
	requiresRHSM        bool
	requiresRegistry    bool
	requiresLicense     bool
	registryURL         string
	baseRepos           []string
	usesRHCephToolsRepo bool
	ibmRepoTemplate     string
	imageBaseTemplate   string
	runtimeOS           RuntimeOS
}

func (d distributionDef) requiresEntitlement() bool {
	return d.requiresRHSM || d.requiresRegistry || d.requiresLicense
}

func rhelCephRuntimeOS(message string) RuntimeOS {
	return RuntimeOS{
		Family:         "rhel",
		ExactVersions:  v1alpha1.StorageCephRHELVersions(),
		ManagedMessage: message,
	}
}

var distributions = map[string]distributionDef{
	v1alpha1.StorageCephDistributionOSS: {
		runtimeOS: RuntimeOS{Family: "linux"},
	},
	v1alpha1.StorageCephDistributionRedHat: {
		requiresRHSM:        true,
		requiresRegistry:    true,
		registryURL:         RedHatRegistryURL,
		baseRepos:           []string{rhelBaseOSRepo, rhelAppStreamRepo},
		usesRHCephToolsRepo: true,
		imageBaseTemplate:   rhcephImageBaseTemplate,
		runtimeOS:           rhelCephRuntimeOS("Red Hat Ceph Storage requires RHEL 9.6, 9.7, 10, or 10.1 on storage nodes"),
	},
	v1alpha1.StorageCephDistributionIBM: {
		requiresRHSM:      true,
		requiresRegistry:  true,
		requiresLicense:   true,
		registryURL:       IBMRegistryURL,
		baseRepos:         []string{rhelBaseOSRepo, rhelAppStreamRepo},
		ibmRepoTemplate:   ibmStorageCephRepoTemplate,
		imageBaseTemplate: ibmImageBaseTemplate,
		runtimeOS:         rhelCephRuntimeOS("IBM Storage Ceph requires RHEL 9.6, 9.7, 10, or 10.1 on storage nodes"),
	},
}

type Provider struct {
	Distribution         string
	Entitlement          entitlements.Resolved
	RequiresRHSM         bool
	RequiresRegistry     bool
	RequiresLicense      bool
	PrerequisitePackages []string
	CephadmPackage       string
	CephCommonPackage    string
	Image                string
	ImageBase            string
	Community            Community
	Repository           Repository
	RuntimeOS            RuntimeOS
}

type Community struct {
	Release  string
	Version  string
	Mirror   string
	Checksum string
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

func cephRelease(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil {
		return ""
	}
	return cluster.Spec.Ceph.Release
}

func cephImage(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil {
		return ""
	}
	return cluster.Spec.Ceph.Image
}

func communitySource(cluster v1alpha1.StorageCluster) Community {
	release := cephRelease(cluster)
	if release == "" {
		release = v1alpha1.StorageCephCommunityDefaultRelease
	}
	var out Community
	if ossUpstreamVersionPattern.MatchString(release) {
		out.Version = release
	} else {
		out.Release = release
	}
	if cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Community != nil {
		out.Mirror = cluster.Spec.Ceph.Community.Mirror
		out.Checksum = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cluster.Spec.Ceph.Community.Checksum), "sha256:"))
	}
	return out
}

func ossImage(cluster v1alpha1.StorageCluster, community Community) string {
	if image := cephImage(cluster); image != "" {
		return image
	}
	if community.Version != "" {
		return ossImageRepository + ":v" + community.Version
	}
	return ""
}

func imageRepository(image string) string {
	if at := strings.IndexByte(image, '@'); at >= 0 {
		image = image[:at]
	}
	segStart := strings.LastIndexByte(image, '/') + 1
	if colon := strings.IndexByte(image[segStart:], ':'); colon >= 0 {
		return image[:segStart+colon]
	}
	return image
}

func imageBase(distribution string, def distributionDef, stream, image string) string {
	if image != "" {
		return imageRepository(image)
	}
	if distribution == v1alpha1.StorageCephDistributionOSS {
		return ossImageRepository
	}
	if def.imageBaseTemplate != "" {
		return fmt.Sprintf(def.imageBaseTemplate, stream)
	}
	return ""
}

func subscriptionStream(cluster v1alpha1.StorageCluster) string {
	release := cephRelease(cluster)
	if release == "" {
		return v1alpha1.StorageCephSubscriptionDefaultStream
	}
	if i := strings.IndexByte(release, '.'); i >= 0 {
		return release[:i]
	}
	return release
}

func subscriptionRepository(def distributionDef, stream string) Repository {
	repos := append([]string(nil), def.baseRepos...)
	if def.usesRHCephToolsRepo {
		repos = append(repos, fmt.Sprintf(rhcephToolsRepoTemplate, stream))
	}
	repo := Repository{RedHatRepos: repos}
	if def.ibmRepoTemplate != "" {
		repo.IBMRepoURL = fmt.Sprintf(def.ibmRepoTemplate, stream)
	}
	return repo
}

func Select(cluster v1alpha1.StorageCluster, ents []v1alpha1.Entitlement, idx secret.Index, secretsDir string) Provider {
	distribution := Distribution(cluster)
	def := distributions[distribution]
	provider := Provider{
		Distribution:         distribution,
		PrerequisitePackages: []string{"firewalld", "lvm2", "podman", "chrony"},
		CephadmPackage:       "cephadm",
		CephCommonPackage:    "ceph-common",
		RequiresRHSM:         def.requiresRHSM,
		RequiresRegistry:     def.requiresRegistry,
		RequiresLicense:      def.requiresLicense,
		RuntimeOS:            def.runtimeOS,
	}
	if provider.RuntimeOS.Family == "" {
		provider.RuntimeOS.Family = "linux"
	}
	if distribution == v1alpha1.StorageCephDistributionOSS {
		provider.Community = communitySource(cluster)
		provider.Image = ossImage(cluster, provider.Community)
	} else {
		provider.Repository = subscriptionRepository(def, subscriptionStream(cluster))
		provider.Image = cephImage(cluster)
	}
	provider.ImageBase = imageBase(distribution, def, subscriptionStream(cluster), provider.Image)
	if def.requiresEntitlement() && cluster.Spec.Ceph != nil {
		provider.Entitlement, _ = entitlements.Resolve(ents, idx, cluster.Spec.Ceph.EntitlementRef.Name, def.registryURL, secretsDir)
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
	if provider.Community.Release != "" || provider.Community.Version != "" {
		community := map[string]any{}
		if provider.Community.Version != "" {
			community["version"] = provider.Community.Version
		} else {
			community["release"] = provider.Community.Release
		}
		if provider.Community.Mirror != "" {
			community["mirror"] = provider.Community.Mirror
		}
		if provider.Community.Checksum != "" {
			community["checksum"] = provider.Community.Checksum
		}
		out["community"] = community
	}
	if provider.Image != "" {
		out["image"] = provider.Image
	}
	if provider.ImageBase != "" {
		out["imageBase"] = provider.ImageBase
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
		rhsm := map[string]any{
			"organizationPath":  provider.Entitlement.RHSM.OrganizationPath,
			"activationKeyPath": provider.Entitlement.RHSM.ActivationKeyPath,
			"connectToInsights": provider.Entitlement.RHSM.ConnectToInsights,
		}
		if satellite := provider.Entitlement.RHSM.Satellite; satellite.Hostname != "" {
			sat := map[string]any{"hostname": satellite.Hostname}
			if satellite.ContentBaseURL != "" {
				sat["contentBaseURL"] = satellite.ContentBaseURL
			}
			if satellite.TrustBundlePath != "" {
				sat["caPath"] = satellite.TrustBundlePath
			}
			rhsm["satellite"] = sat
		}
		out["rhsm"] = rhsm
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
