package cephprovider

import (
	"fmt"
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

	ibmImagePathTemplate    = "ibm-ceph/ceph-%s-rhel%s"
	rhcephImagePathTemplate = "rhceph/rhceph-%s-rhel%s"

	ibmCephadmBootstrapLicenseOption = "--automatically-accept-license"
	ibmCephOrchCallHomeConsentToken  = "call-home-enabled"
)

const (
	rhelBaseOSRepo          = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-baseos-rpms"
	rhelAppStreamRepo       = "rhel-{{ ansible_distribution_major_version }}-for-x86_64-appstream-rpms"
	rhcephToolsRepoTemplate = "rhceph-%s-tools-for-rhel-{{ ansible_distribution_major_version }}-x86_64-rpms"
)

type distributionDef struct {
	requiresRHSM                     bool
	requiresRegistry                 bool
	requiresLicense                  bool
	nativeCapabilityCandidates       NativeCapabilityCandidates
	registryURL                      string
	baseRepos                        []string
	usesRHCephToolsRepo              bool
	ibmRepoTemplate                  string
	imagePathTemplate                string
	nativePreparationRuntimePackages []string
	artifactPolicy                   ArtifactPolicy
}

func (d distributionDef) requiresEntitlement() bool {
	return d.requiresRHSM || d.requiresRegistry || d.requiresLicense
}

var distributions = map[string]distributionDef{
	v1alpha1.StorageCephDistributionOSS: {
		artifactPolicy: ArtifactPolicy{
			PackagePin:               ArtifactPinForbidden,
			CephadmAnsiblePackagePin: ArtifactPinForbidden,
		},
	},
	v1alpha1.StorageCephDistributionRedHat: {
		requiresRHSM:                     true,
		requiresRegistry:                 true,
		registryURL:                      RedHatRegistryURL,
		baseRepos:                        []string{rhelBaseOSRepo, rhelAppStreamRepo},
		usesRHCephToolsRepo:              true,
		imagePathTemplate:                rhcephImagePathTemplate,
		nativePreparationRuntimePackages: []string{"ceph-common"},
		artifactPolicy: ArtifactPolicy{
			PackagePin:               ArtifactPinOptional,
			CephadmAnsiblePackagePin: ArtifactPinOptional,
			ImageBaseRequired:        true,
			NativePreparationMode:    NativePreparationCephadmAnsibleLocal,
		},
	},
	v1alpha1.StorageCephDistributionIBM: {
		requiresRHSM:     true,
		requiresRegistry: true,
		requiresLicense:  true,
		nativeCapabilityCandidates: NativeCapabilityCandidates{
			CephadmBootstrapLicenseOption: ibmCephadmBootstrapLicenseOption,
			CephOrchCallHomeConsentToken:  ibmCephOrchCallHomeConsentToken,
		},
		registryURL:                      IBMRegistryURL,
		baseRepos:                        []string{rhelBaseOSRepo, rhelAppStreamRepo},
		ibmRepoTemplate:                  ibmStorageCephRepoTemplate,
		imagePathTemplate:                ibmImagePathTemplate,
		nativePreparationRuntimePackages: []string{"ceph-common"},
		artifactPolicy: ArtifactPolicy{
			PackagePin:                       ArtifactPinRequired,
			RPMReleaseRequired:               true,
			CephadmAnsiblePackagePin:         ArtifactPinRequired,
			CephadmAnsibleRPMReleaseRequired: true,
			ImageBaseRequired:                true,
			ImagePinRequired:                 true,
			NativeParityMode:                 NativeParityCephVersion,
			NativePreparationMode:            NativePreparationCephadmAnsibleLocal,
		},
	},
}

type ArtifactPinPolicy string

const (
	ArtifactPinOptional  ArtifactPinPolicy = "optional"
	ArtifactPinRequired  ArtifactPinPolicy = "required"
	ArtifactPinForbidden ArtifactPinPolicy = "forbidden"

	NativeParityCephVersion = "ceph-version"

	NativePreparationCephadmAnsibleLocal = "cephadm-ansible-local"
)

type ArtifactPolicy struct {
	PackagePin                       ArtifactPinPolicy
	RPMReleaseRequired               bool
	CephadmAnsiblePackagePin         ArtifactPinPolicy
	CephadmAnsibleRPMReleaseRequired bool
	ImageBaseRequired                bool
	ImagePinRequired                 bool
	NativeParityMode                 string
	NativePreparationMode            string
}

type NativeCapabilityCandidates struct {
	CephadmBootstrapLicenseOption string
	CephOrchCallHomeConsentToken  string
}

func ArtifactPolicyFor(distribution string) (ArtifactPolicy, bool) {
	def, ok := distributions[distribution]
	if !ok {
		return ArtifactPolicy{}, false
	}
	return def.artifactPolicy, true
}

type Provider struct {
	Distribution               string
	Entitlement                entitlements.Resolved
	OSRegistration             entitlements.Resolved
	RequiresRHSM               bool
	RequiresRegistry           bool
	RequiresLicense            bool
	PrerequisitePackages       []string
	CephadmPackage             string
	CephadmPackageSpec         string
	CephadmAnsiblePackage      string
	CephadmAnsiblePackageSpec  string
	Image                      string
	ImageBase                  string
	Community                  Community
	Repository                 Repository
	RuntimeOS                  RuntimeOS
	ArtifactPolicy             ArtifactPolicy
	NativeCapabilityCandidates NativeCapabilityCandidates
	NativePreparation          NativePreparation
	PackageArtifacts           []PackageArtifact
	IBMCallHome                string
}

type NativePreparation struct {
	RuntimePackages     []string
	RuntimePackageSpecs []string
}

type PackageArtifact struct {
	Name             string
	Spec             string
	DesiredStatePath string
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

func cephImageBase(cluster v1alpha1.StorageCluster) string {
	return v1alpha1.StorageCephImageBase(cluster.Spec.Ceph)
}

func cephImageVersion(cluster v1alpha1.StorageCluster) string {
	return v1alpha1.StorageCephImageVersion(cluster.Spec.Ceph)
}

func cephPackageVersion(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil {
		return ""
	}
	return cluster.Spec.Ceph.PackageVersion
}

func cephadmAnsiblePackageVersion(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.Cephadm.Ansible == nil {
		return ""
	}
	return cluster.Spec.Ceph.Cephadm.Ansible.PackageVersion
}

func communitySource(cluster v1alpha1.StorageCluster) Community {
	release := cephRelease(cluster)
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

func imageBase(cluster v1alpha1.StorageCluster, distribution string) string {
	if base := cephImageBase(cluster); base != "" {
		return base
	}
	if distribution == v1alpha1.StorageCephDistributionOSS {
		return ossImageRepository
	}
	return ""
}

func resolvedImage(cluster v1alpha1.StorageCluster, base string, community Community) string {
	version := cephImageVersion(cluster)
	if version == "" {
		version = DerivedOSSImageVersion(Distribution(cluster), community.Version)
	}
	return JoinImageReference(base, version)
}

func subscriptionStream(cluster v1alpha1.StorageCluster) string {
	if release, ok := ResolveRelease(Distribution(cluster), cephRelease(cluster)); ok {
		return release.Stream
	}
	return ""
}

func subscriptionRepository(def distributionDef, stream string, packages *v1alpha1.StorageCephIBMPackagesSpec) Repository {
	repos := append([]string(nil), def.baseRepos...)
	if def.usesRHCephToolsRepo {
		repos = append(repos, fmt.Sprintf(rhcephToolsRepoTemplate, stream))
	}
	repo := Repository{RedHatRepos: repos}
	if def.ibmRepoTemplate == "" {
		return repo
	}
	if packages != nil && packages.Source == v1alpha1.StorageCephIBMPackageSourceSubscription {
		repo.RedHatRepos = append(repo.RedHatRepos, packages.SubscriptionRepos...)
		return repo
	}
	repo.IBMRepoURL = fmt.Sprintf(def.ibmRepoTemplate, stream)
	return repo
}

func Select(cluster v1alpha1.StorageCluster, ents []v1alpha1.Entitlement, idx secret.Index, secretsDir string) Provider {
	distribution := Distribution(cluster)
	def := distributions[distribution]
	stream := subscriptionStream(cluster)
	provider := Provider{
		Distribution:               distribution,
		PrerequisitePackages:       []string{"firewalld", "lvm2", "podman", "chrony", "sos"},
		CephadmPackage:             "cephadm",
		CephadmAnsiblePackage:      "cephadm-ansible",
		RequiresRHSM:               def.requiresRHSM,
		RequiresRegistry:           def.requiresRegistry,
		RequiresLicense:            def.requiresLicense,
		RuntimeOS:                  RuntimeOS{Family: "rhel"},
		ArtifactPolicy:             def.artifactPolicy,
		NativeCapabilityCandidates: def.nativeCapabilityCandidates,
		NativePreparation: NativePreparation{
			RuntimePackages: append([]string(nil), def.nativePreparationRuntimePackages...),
		},
	}
	if distribution == v1alpha1.StorageCephDistributionOSS {
		provider.Community = communitySource(cluster)
	} else {
		provider.Repository = subscriptionRepository(def, stream, v1alpha1.StorageCephIBMPackages(cluster.Spec.Ceph))
	}
	if version := cephPackageVersion(cluster); version != "" {
		provider.CephadmPackageSpec = provider.CephadmPackage + "-" + version
		provider.PackageArtifacts = append(provider.PackageArtifacts, PackageArtifact{
			Name: provider.CephadmPackage, Spec: provider.CephadmPackageSpec,
			DesiredStatePath: "spec.ceph.packageVersion",
		})
		if provider.ArtifactPolicy.NativePreparationMode != "" {
			for _, name := range def.nativePreparationRuntimePackages {
				spec := name + "-" + version
				provider.NativePreparation.RuntimePackageSpecs = append(provider.NativePreparation.RuntimePackageSpecs, spec)
				provider.PackageArtifacts = append(provider.PackageArtifacts, PackageArtifact{
					Name: name, Spec: spec,
					DesiredStatePath: "spec.ceph.packageVersion",
				})
			}
		}
	}
	if version := cephadmAnsiblePackageVersion(cluster); version != "" {
		provider.CephadmAnsiblePackageSpec = provider.CephadmAnsiblePackage + "-" + version
		provider.PackageArtifacts = append(provider.PackageArtifacts, PackageArtifact{
			Name: provider.CephadmAnsiblePackage, Spec: provider.CephadmAnsiblePackageSpec,
			DesiredStatePath: "spec.ceph.cephadm.ansible.packageVersion",
		})
	}
	if def.requiresEntitlement() && cluster.Spec.Ceph != nil {
		provider.Entitlement, _ = entitlements.Resolve(ents, idx, cluster.Spec.Ceph.EntitlementRef.Name, def.registryURL, secretsDir)
	}
	provider.ImageBase = imageBase(cluster, distribution)
	provider.Image = resolvedImage(cluster, provider.ImageBase, provider.Community)
	if distribution == v1alpha1.StorageCephDistributionIBM && cluster.Spec.Ceph != nil && cluster.Spec.Ceph.IBM != nil {
		provider.IBMCallHome = cluster.Spec.Ceph.IBM.CallHome
	}
	return provider
}

func Vars(provider Provider) map[string]any {
	out := map[string]any{
		"name":                  provider.Distribution,
		"distribution":          provider.Distribution,
		"requiresRHSM":          provider.RequiresRHSM,
		"requiresRegistry":      provider.RequiresRegistry,
		"requiresLicense":       provider.RequiresLicense,
		"prerequisitePackages":  append([]string(nil), provider.PrerequisitePackages...),
		"cephadmPackage":        provider.CephadmPackage,
		"cephadmAnsiblePackage": provider.CephadmAnsiblePackage,
		"artifactPolicy": map[string]any{
			"packagePin":                       string(provider.ArtifactPolicy.PackagePin),
			"rpmReleaseRequired":               provider.ArtifactPolicy.RPMReleaseRequired,
			"cephadmAnsiblePackagePin":         string(provider.ArtifactPolicy.CephadmAnsiblePackagePin),
			"cephadmAnsibleRPMReleaseRequired": provider.ArtifactPolicy.CephadmAnsibleRPMReleaseRequired,
			"imageBaseRequired":                provider.ArtifactPolicy.ImageBaseRequired,
			"imagePinRequired":                 provider.ArtifactPolicy.ImagePinRequired,
			"nativeParityMode":                 provider.ArtifactPolicy.NativeParityMode,
			"nativePreparationMode":            provider.ArtifactPolicy.NativePreparationMode,
		},
	}
	if provider.NativeCapabilityCandidates.CephadmBootstrapLicenseOption != "" || provider.NativeCapabilityCandidates.CephOrchCallHomeConsentToken != "" {
		out["nativeCapabilityCandidates"] = map[string]any{
			"cephadmBootstrapLicenseOption": provider.NativeCapabilityCandidates.CephadmBootstrapLicenseOption,
			"cephOrchCallHomeConsentToken":  provider.NativeCapabilityCandidates.CephOrchCallHomeConsentToken,
		}
	}
	if provider.ArtifactPolicy.NativePreparationMode != "" {
		out["nativePreparation"] = map[string]any{
			"runtimePackages":     append([]string(nil), provider.NativePreparation.RuntimePackages...),
			"runtimePackageSpecs": append([]string(nil), provider.NativePreparation.RuntimePackageSpecs...),
		}
	}
	if len(provider.PackageArtifacts) > 0 {
		artifacts := make([]map[string]any, 0, len(provider.PackageArtifacts))
		for _, artifact := range provider.PackageArtifacts {
			artifacts = append(artifacts, map[string]any{
				"name": artifact.Name, "spec": artifact.Spec,
				"desiredStatePath": artifact.DesiredStatePath,
			})
		}
		out["packageArtifacts"] = artifacts
	}
	if provider.CephadmPackageSpec != "" {
		out["cephadmPackageSpec"] = provider.CephadmPackageSpec
	}
	if provider.CephadmAnsiblePackageSpec != "" {
		out["cephadmAnsiblePackageSpec"] = provider.CephadmAnsiblePackageSpec
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
		out["runtimeOS"] = map[string]any{"family": provider.RuntimeOS.Family}
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
