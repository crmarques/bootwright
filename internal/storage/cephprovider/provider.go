package cephprovider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
)

const (
	RedHatRegistryURL = "registry.redhat.io"
	IBMRegistryURL    = "cp.icr.io/cp"

	// ossImageRepository is the canonical upstream container image repository.
	// An oss x.y.z release derives quay.io/ceph/ceph:vX.Y.Z when spec.ceph.image
	// is unset, pinning the running daemons reproducibly.
	ossImageRepository = "quay.io/ceph/ceph"

	// ibmStorageCephRepoTemplate is the vendor .repo definition cephadm and ceph
	// packages are installed from on IBM Storage Ceph nodes. The "%s" segment is
	// IBM's release stream (cephadm-ansible's ceph_ibm_version), selected by
	// spec.ceph.release; the "-rhel-9" suffix carries the RHEL track separately.
	ibmStorageCephRepoTemplate = "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/ibm-storage-ceph-%s-rhel-9.repo"

	// ibmImageBaseTemplate and rhcephImageBaseTemplate are the vendor container
	// image repositories cephadm lists upgrade targets from: the dashboard
	// "Upgrade" page and `ceph orch upgrade ls` resolve against
	// mgr/cephadm/container_image_base, NOT the running daemon image. The "%s"
	// segment is the product stream major selected by spec.ceph.release; the
	// "-rhel9" suffix matches the RHEL track the images are built for. cephadm's
	// compiled-in default base is the Red Hat image even on IBM Storage Ceph
	// (which is built from Red Hat Ceph Storage), so for IBM the base must be
	// pinned to cp.icr.io — where the cluster is entitled — or upgrade discovery
	// 401s against registry.redhat.io.
	ibmImageBaseTemplate    = "cp.icr.io/cp/ibm-ceph/ceph-%s-rhel9"
	rhcephImageBaseTemplate = "registry.redhat.io/rhceph/rhceph-%s-rhel9"
)

const (
	rhelBaseOSRepo    = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-baseos-rpms"
	rhelAppStreamRepo = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-appstream-rpms"
	// rhcephToolsRepoTemplate is the Red Hat Ceph Storage tools repository. "%s"
	// is the product stream major selected by spec.ceph.release; the RHEL major
	// is expanded by subscription-manager at apply time.
	rhcephToolsRepoTemplate = "rhceph-%s-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms"
)

// ossUpstreamVersionPattern matches a full upstream Ceph x.y.z version. Such a
// release pins the package repository to rpm-x.y.z and derives the matching
// container image; anything else (squid, reef) is treated as a release name.
var ossUpstreamVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// rhelCephVersions are the RHEL releases the subscription-backed (redhat, ibm)
// Ceph distributions support on storage nodes.
var rhelCephVersions = []string{"9.6", "9.7", "10", "10.0", "10.1"}

// distributionDef is the declarative record of everything that varies between
// the supported Ceph distributions. Distribution facts live in this one table
// rather than in imperative selection logic, so adding a distribution is a table
// entry plus its api/v1alpha1 constant and validation — never a new code branch
// in Select, Vars, or the Ansible role.
type distributionDef struct {
	requiresRHSM        bool
	requiresRegistry    bool
	requiresLicense     bool
	registryURL         string   // default cephadm registry; an entitlement may override it
	baseRepos           []string // stream-independent subscription-manager repositories
	usesRHCephToolsRepo bool     // append rhceph-<stream>-tools (Red Hat Ceph Storage)
	ibmRepoTemplate     string   // vendor .repo template taking the stream major, when set
	imageBaseTemplate   string   // container image repository template (stream major), when set
	runtimeOS           RuntimeOS
}

func (d distributionDef) requiresEntitlement() bool {
	return d.requiresRHSM || d.requiresRegistry || d.requiresLicense
}

func rhelCephRuntimeOS(message string) RuntimeOS {
	return RuntimeOS{
		Family:         "rhel",
		ExactVersions:  append([]string(nil), rhelCephVersions...),
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
	// Image, when set, is the fully-qualified cephadm bootstrap container image
	// that pins every Ceph daemon. It is explicit for redhat and ibm and either
	// explicit or derived from an oss x.y.z release.
	Image string
	// ImageBase is the bare container image repository (no tag or digest) that
	// cephadm lists upgrade targets from via mgr/cephadm/container_image_base. It
	// is the repository of an explicit Image when one is pinned, otherwise the
	// distribution's vendor repository derived from the release stream. Pinned on
	// the cluster at apply time so upgrade discovery queries the registry the
	// cluster is entitled to rather than cephadm's compiled-in default.
	ImageBase  string
	Community  Community
	Repository Repository
	RuntimeOS  RuntimeOS
}

// Community describes the upstream package source for the oss distribution.
// It is unset for the redhat and ibm distributions. Exactly one of Release (a
// release name such as squid) or Version (a full x.y.z) is set.
type Community struct {
	Release string
	Version string
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

// cephRelease returns the operator's spec.ceph.release, empty when unset.
func cephRelease(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil {
		return ""
	}
	return cluster.Spec.Ceph.Release
}

// cephImage returns the operator's explicit spec.ceph.image, empty when unset.
func cephImage(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil {
		return ""
	}
	return cluster.Spec.Ceph.Image
}

// communitySource resolves the upstream package source for the oss
// distribution, defaulting the release when the operator left it unset and
// classifying it as a release name or a full x.y.z version.
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
	}
	return out
}

// ossImage pins the oss container image: the operator's explicit image when
// set, otherwise quay.io/ceph/ceph:vX.Y.Z derived from an x.y.z release. A
// release name leaves it empty (the image floats; not reproducible).
func ossImage(cluster v1alpha1.StorageCluster, community Community) string {
	if image := cephImage(cluster); image != "" {
		return image
	}
	if community.Version != "" {
		return ossImageRepository + ":v" + community.Version
	}
	return ""
}

// imageRepository strips the tag or digest from a fully-qualified container
// image reference, yielding the bare repository cephadm lists upgrade tags from
// (mgr/cephadm/container_image_base). A registry host:port is preserved; only a
// trailing :tag or @digest on the final path segment is removed.
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

// imageBase resolves the container image repository cephadm pins into
// mgr/cephadm/container_image_base. An explicit image wins (its repository); for
// an unset image it is the upstream repository (oss) or the vendor repository
// derived from the release stream (redhat, ibm). Empty only for a distribution
// that declares no image repository, which leaves the base to cephadm's default.
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

// subscriptionStream resolves the product stream major (for example 9) for the
// redhat and ibm distributions from spec.ceph.release, defaulting when unset.
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

// subscriptionRepository assembles the stream-dependent subscription repos.
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

func Select(cluster v1alpha1.StorageCluster, env *v1alpha1.Environment, secretsDir string) Provider {
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
		provider.Entitlement, _ = entitlements.Resolve(env, cluster.Spec.Ceph.EntitlementRef.Name, def.registryURL, secretsDir)
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
