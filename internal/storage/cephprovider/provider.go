package cephprovider

import (
	"fmt"
	"regexp"
	"sort"
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

	ibmImagePathTemplate    = "ibm-ceph/ceph-%s-rhel9"
	rhcephImagePathTemplate = "rhceph/rhceph-%s-rhel9"
)

const (
	rhelBaseOSRepo          = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-baseos-rpms"
	rhelAppStreamRepo       = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-appstream-rpms"
	rhcephToolsRepoTemplate = "rhceph-%s-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms"
)

var (
	ossUpstreamVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	ossSupportedReleaseNames  = map[string]bool{"squid": true, "tentacle": true}
	ossSupportedSeries        = []string{"19.2.", "20.2."}
)

type distributionDef struct {
	defaultRelease      string
	releases            map[string]releaseDef
	requiresRHSM        bool
	requiresRegistry    bool
	requiresLicense     bool
	registryURL         string
	baseRepos           []string
	usesRHCephToolsRepo bool
	ibmRepoTemplate     string
	imagePathTemplate   string
	runtimeOS           RuntimeOS
}

type releaseDef struct {
	value     string
	stream    string
	runtimeOS RuntimeOS
}

func (d distributionDef) requiresEntitlement() bool {
	return d.requiresRHSM || d.requiresRegistry || d.requiresLicense
}

func rhelCephRuntimeOS(versions []string, message string) RuntimeOS {
	return RuntimeOS{
		Family:         "rhel",
		ExactVersions:  append([]string(nil), versions...),
		ManagedMessage: message,
	}
}

var (
	rhcs90RuntimeOS = rhelCephRuntimeOS(
		[]string{"9.6", "9.7", "10", "10.0", "10.1"},
		"Red Hat Ceph Storage 9.0 requires RHEL 9.6, 9.7, 10, or 10.1 on storage nodes",
	)
	rhcs91RuntimeOS = rhelCephRuntimeOS(
		[]string{"9.8", "10.2"},
		"Red Hat Ceph Storage 9.1 requires RHEL 9.8 or 10.2 on storage nodes",
	)
	ibm991RuntimeOS = rhelCephRuntimeOS(
		[]string{"9.8", "10.2"},
		"IBM Storage Ceph 9.9.1 requires RHEL 9.8 or 10.2 on storage nodes",
	)
)

var distributions = map[string]distributionDef{
	v1alpha1.StorageCephDistributionOSS: {
		defaultRelease: v1alpha1.StorageCephCommunityDefaultRelease,
		runtimeOS: RuntimeOS{
			Family:         "rhel",
			ManagedMessage: "Community Ceph is supported on RHEL-family storage nodes",
		},
	},
	v1alpha1.StorageCephDistributionRedHat: {
		defaultRelease: "9.1",
		releases: map[string]releaseDef{
			"9":   {value: "9.1", stream: "9", runtimeOS: rhcs91RuntimeOS},
			"9.0": {value: "9.0", stream: "9", runtimeOS: rhcs90RuntimeOS},
			"9.1": {value: "9.1", stream: "9", runtimeOS: rhcs91RuntimeOS},
		},
		requiresRHSM:        true,
		requiresRegistry:    true,
		registryURL:         RedHatRegistryURL,
		baseRepos:           []string{rhelBaseOSRepo, rhelAppStreamRepo},
		usesRHCephToolsRepo: true,
		imagePathTemplate:   rhcephImagePathTemplate,
	},
	v1alpha1.StorageCephDistributionIBM: {
		defaultRelease: "9.9.1",
		releases: map[string]releaseDef{
			"9":     {value: "9.9.1", stream: "9", runtimeOS: ibm991RuntimeOS},
			"9.9.1": {value: "9.9.1", stream: "9", runtimeOS: ibm991RuntimeOS},
		},
		requiresRHSM:      true,
		requiresRegistry:  true,
		requiresLicense:   true,
		registryURL:       IBMRegistryURL,
		baseRepos:         []string{rhelBaseOSRepo, rhelAppStreamRepo},
		ibmRepoTemplate:   ibmStorageCephRepoTemplate,
		imagePathTemplate: ibmImagePathTemplate,
	},
}

type Provider struct {
	Distribution         string
	Entitlement          entitlements.Resolved
	OSRegistration       entitlements.Resolved
	RequiresRHSM         bool
	RequiresRegistry     bool
	RequiresLicense      bool
	PrerequisitePackages []string
	CephadmPackage       string
	Image                string
	ImageBase            string
	Community            Community
	Repository           Repository
	RuntimeOS            RuntimeOS
	IBMCallHome          string
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

type ResolvedRelease struct {
	Value     string
	Stream    string
	RuntimeOS RuntimeOS
}

func ResolveRelease(distribution, authored string) (ResolvedRelease, bool) {
	def, ok := distributions[distribution]
	if !ok {
		return ResolvedRelease{}, false
	}
	value := authored
	if value == "" {
		value = def.defaultRelease
	}
	if distribution == v1alpha1.StorageCephDistributionOSS && !supportedOSSRelease(value) {
		return ResolvedRelease{}, false
	}
	if def.releases == nil {
		return ResolvedRelease{Value: value, RuntimeOS: def.runtimeOS}, true
	}
	release, ok := def.releases[value]
	if !ok {
		return ResolvedRelease{}, false
	}
	return ResolvedRelease{Value: release.value, Stream: release.stream, RuntimeOS: release.runtimeOS}, true
}

func SupportedReleases(distribution string) []string {
	def, ok := distributions[distribution]
	if !ok {
		return nil
	}
	if distribution == v1alpha1.StorageCephDistributionOSS {
		return []string{"19.2.x", "20.2.x", "squid", "tentacle"}
	}
	if def.releases == nil {
		return nil
	}
	out := make([]string, 0, len(def.releases))
	for value := range def.releases {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func supportedOSSRelease(value string) bool {
	if ossSupportedReleaseNames[value] {
		return true
	}
	if !ossUpstreamVersionPattern.MatchString(value) {
		return false
	}
	for _, series := range ossSupportedSeries {
		if strings.HasPrefix(value, series) {
			return true
		}
	}
	return false
}

func DefaultRelease(distribution string) string {
	if def, ok := distributions[distribution]; ok {
		return def.defaultRelease
	}
	return ""
}

func DefaultRegistryURL(distribution string) string {
	if def, ok := distributions[distribution]; ok {
		return def.registryURL
	}
	return ""
}

func DerivedOSSImage(release string) string {
	if ossUpstreamVersionPattern.MatchString(release) {
		return ossImageRepository + ":v" + release
	}
	return ""
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
		release = DefaultRelease(v1alpha1.StorageCephDistributionOSS)
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
		return DerivedOSSImage(community.Version)
	}
	return ""
}

func ImageRepository(image string) string {
	if at := strings.IndexByte(image, '@'); at >= 0 {
		image = image[:at]
	}
	segStart := strings.LastIndexByte(image, '/') + 1
	if colon := strings.IndexByte(image[segStart:], ':'); colon >= 0 {
		return image[:segStart+colon]
	}
	return image
}

func ExpectedImageRepository(distribution, release, registryURL string) (string, bool) {
	def, ok := distributions[distribution]
	if !ok {
		return "", false
	}
	resolved, ok := ResolveRelease(distribution, release)
	if !ok {
		return "", false
	}
	if distribution == v1alpha1.StorageCephDistributionOSS {
		return ossImageRepository, true
	}
	if registryURL == "" {
		registryURL = def.registryURL
	}
	if def.imagePathTemplate == "" || registryURL == "" || resolved.Stream == "" {
		return "", false
	}
	return strings.TrimSuffix(registryURL, "/") + "/" + fmt.Sprintf(def.imagePathTemplate, resolved.Stream), true
}

func imageBase(distribution, release, registryURL, image string) string {
	if image != "" {
		return ImageRepository(image)
	}
	repository, _ := ExpectedImageRepository(distribution, release, registryURL)
	return repository
}

func subscriptionStream(cluster v1alpha1.StorageCluster) string {
	if release, ok := ResolveRelease(Distribution(cluster), cephRelease(cluster)); ok {
		return release.Stream
	}
	return ""
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
	release, _ := ResolveRelease(distribution, cephRelease(cluster))
	provider := Provider{
		Distribution:         distribution,
		PrerequisitePackages: []string{"firewalld", "lvm2", "podman", "chrony"},
		CephadmPackage:       "cephadm",
		RequiresRHSM:         def.requiresRHSM,
		RequiresRegistry:     def.requiresRegistry,
		RequiresLicense:      def.requiresLicense,
		RuntimeOS:            release.RuntimeOS,
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
	if def.requiresEntitlement() && cluster.Spec.Ceph != nil {
		provider.Entitlement, _ = entitlements.Resolve(ents, idx, cluster.Spec.Ceph.EntitlementRef.Name, def.registryURL, secretsDir)
	}
	registryURL := def.registryURL
	if provider.Entitlement.Registry.URL != "" {
		registryURL = provider.Entitlement.Registry.URL
	}
	provider.ImageBase = imageBase(distribution, release.Value, registryURL, provider.Image)
	if distribution == v1alpha1.StorageCephDistributionIBM && cluster.Spec.Ceph != nil && cluster.Spec.Ceph.IBM != nil {
		provider.IBMCallHome = cluster.Spec.Ceph.IBM.CallHome
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
	regRHSM := provider.Entitlement.RHSM
	regManagement := provider.Entitlement.RHSM.Management
	if regManagement == "" && provider.OSRegistration.Name != "" {
		regRHSM = provider.OSRegistration.RHSM
		regManagement = provider.OSRegistration.RHSM.Management
		if regManagement == "" {
			regManagement = v1alpha1.EntitlementRHSMManagementManaged
		}
	}
	if regManagement != "" {
		out["rhsmManagement"] = regManagement
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
	if regRHSM.OrganizationPath != "" || regRHSM.ActivationKeyPath != "" {
		rhsm := map[string]any{
			"organizationPath":  regRHSM.OrganizationPath,
			"activationKeyPath": regRHSM.ActivationKeyPath,
			"connectToInsights": regRHSM.ConnectToInsights,
		}
		if satellite := regRHSM.Satellite; satellite.Hostname != "" {
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
	if provider.IBMCallHome != "" {
		out["ibm"] = map[string]any{
			"callHome": provider.IBMCallHome,
		}
	}
	return out
}
