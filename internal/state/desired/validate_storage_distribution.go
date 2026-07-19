package desiredstate

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/storage/cephprovider"
)

func validateStorageCephDistribution(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
	distribution := storageCephDistribution(cluster)
	if cluster.Spec.Ceph.Distribution != "" && distribution == "" {
		return []string{fmt.Sprintf("%s.distribution %q must be one of {%s, %s, %s}",
			prefix, cluster.Spec.Ceph.Distribution, v1alpha1.StorageCephDistributionOSS, v1alpha1.StorageCephDistributionRedHat, v1alpha1.StorageCephDistributionIBM)}
	}
	ref := cluster.Spec.Ceph.EntitlementRef.Name
	switch distribution {
	case v1alpha1.StorageCephDistributionOSS:
		if ref != "" {
			return []string{prefix + ".entitlementRef must be empty when distribution=oss"}
		}
		return nil
	case v1alpha1.StorageCephDistributionRedHat:
		return validateStorageCephDistributionEntitlement(prefix, state, ref, v1alpha1.EntitlementTypeRedHatCeph)
	case v1alpha1.StorageCephDistributionIBM:
		return validateStorageCephDistributionEntitlement(prefix, state, ref, v1alpha1.EntitlementTypeIBMStorageCeph)
	default:
		return nil
	}
}

func validateStorageCephDistributionEntitlement(prefix string, state v1alpha1.State, ref, wantType string) []string {
	if ref == "" {
		return []string{prefix + ".entitlementRef is required when distribution requires subscription or license handling"}
	}
	entitlement, ok := v1alpha1.EntitlementByName(state.Entitlements, ref)
	if !ok {
		return []string{fmt.Sprintf("%s.entitlementRef %q does not match any Entitlement", prefix, ref)}
	}
	if entitlement.Spec.Type != wantType {
		return []string{fmt.Sprintf("%s.entitlementRef %q resolves to type %q, want %q", prefix, ref, entitlement.Spec.Type, wantType)}
	}
	return nil
}

func validateStorageCephRelease(prefix, distribution, release string) []string {
	if release == "" {
		return nil
	}
	switch distribution {
	case v1alpha1.StorageCephDistributionOSS:
		if !cephOSSReleaseNamePattern.MatchString(release) && !cephOSSReleaseVersionPattern.MatchString(release) {
			return []string{fmt.Sprintf("%s.release %q must be an upstream Ceph release name (e.g. squid) or an x.y.z version (e.g. 19.2.1)", prefix, release)}
		}
		if _, ok := cephprovider.ResolveRelease(distribution, release); !ok {
			return []string{fmt.Sprintf("%s.release %q is not in a supported Ceph release series; supported values are {%s}", prefix, release, strings.Join(cephprovider.SupportedReleases(distribution), ", "))}
		}
	case v1alpha1.StorageCephDistributionRedHat, v1alpha1.StorageCephDistributionIBM:
		if !cephSubscriptionVersionPattern.MatchString(release) {
			return []string{fmt.Sprintf("%s.release %q must be a product version such as 9, 9.1, or 9.9.1; its leading major digit selects the product stream", prefix, release)}
		}
		if _, ok := cephprovider.ResolveRelease(distribution, release); !ok {
			return []string{fmt.Sprintf("%s.release %q is not a supported %s product release; supported values are {%s}", prefix, release, distribution, strings.Join(cephprovider.SupportedReleases(distribution), ", "))}
		}
	}
	return nil
}

func validateStorageCephImage(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
	image := cluster.Spec.Ceph.Image
	distribution := storageCephDistribution(cluster)
	if image != "" {
		if err := validatePinnedImageReference(image); err != "" {
			return []string{fmt.Sprintf("%s.image %q %s", prefix, image, err)}
		}
	}
	if v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		vendorRegistry := cephprovider.DefaultRegistryURL(distribution)
		registry := vendorRegistry
		if entitlement, ok := v1alpha1.EntitlementByName(state.Entitlements, cluster.Spec.Ceph.EntitlementRef.Name); ok && entitlement.Spec.Registry != nil && entitlement.Spec.Registry.URL != "" {
			registry = entitlement.Spec.Registry.URL
			if registry != vendorRegistry && image == "" {
				return []string{fmt.Sprintf("%s.image is required when Entitlement/%s spec.registry.url overrides the vendor registry; pin the mirrored daemon image explicitly", prefix, entitlement.Metadata.Name)}
			}
		}
		expected, ok := cephprovider.ExpectedImageRepository(distribution, cluster.Spec.Ceph.Release, registry)
		if image != "" && ok && cephprovider.ImageRepository(image) != expected {
			release, _ := cephprovider.ResolveRelease(distribution, cluster.Spec.Ceph.Release)
			return []string{fmt.Sprintf("%s.image repository must be %q for %s release %s; preserve the vendor repository suffix when mirroring", prefix, expected, distribution, release.Value)}
		}
	}
	return nil
}

func validateStorageCephCommunity(prefix string, cluster v1alpha1.StorageCluster) []string {
	community := cluster.Spec.Ceph.Community
	if community == nil {
		return nil
	}
	if storageCephDistribution(cluster) != v1alpha1.StorageCephDistributionOSS {
		return []string{prefix + " must be empty unless distribution=oss"}
	}
	var errs []string
	if mirror := community.Mirror; mirror != "" && !isHTTPSURL(mirror) {
		errs = append(errs, fmt.Sprintf("%s.mirror %q must be an https URL", prefix, mirror))
	}
	if _, err := media.NormalizeSHA256(community.Checksum); err != nil {
		errs = append(errs, fmt.Sprintf("%s.checksum %s", prefix, err))
	}
	return errs
}

func validateStorageCephIBM(prefix string, cluster v1alpha1.StorageCluster) []string {
	ibm := cluster.Spec.Ceph.IBM
	if storageCephDistribution(cluster) != v1alpha1.StorageCephDistributionIBM {
		if ibm != nil {
			return []string{prefix + " must be empty unless distribution=ibm"}
		}
		return nil
	}
	if ibm == nil {
		return []string{prefix + " is required when distribution=ibm so Call Home outbound-communication intent is explicit"}
	}
	switch ibm.CallHome {
	case v1alpha1.StorageCephIBMCallHomeEnabled, v1alpha1.StorageCephIBMCallHomeDisabled:
		return nil
	default:
		return []string{fmt.Sprintf("%s.callHome %q must be one of {%s, %s}", prefix, ibm.CallHome, v1alpha1.StorageCephIBMCallHomeEnabled, v1alpha1.StorageCephIBMCallHomeDisabled)}
	}
}

func isHTTPURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func isHTTPSURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func validateStorageCephManagedOS(cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile) []string {
	distribution := storageCephDistribution(cluster)
	if distribution == v1alpha1.StorageCephDistributionOSS {
		return nil
	}
	release, ok := cephprovider.ResolveRelease(distribution, cluster.Spec.Ceph.Release)
	if !ok {
		return nil
	}
	var errs []string
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		machine, ok := machines[node.MachineRef.Name]
		if !ok || machine.Spec.OS.InstallProfileRef.Name == "" {
			continue
		}
		profile, ok := installProfiles[machine.Spec.OS.InstallProfileRef.Name]
		if !ok {
			continue
		}
		owner := fmt.Sprintf("StorageCluster/%s spec.ceph.topology.hosts[%s] MachineInstallProfile/%s spec.os", cluster.Metadata.Name, node.Hostname, profile.Metadata.Name)
		if strings.ToLower(profile.Spec.OS.Family) != v1alpha1.MachineInstallOSFamilyRHEL {
			errs = append(errs, fmt.Sprintf("%s.family %q is incompatible with Ceph distribution %q; use RHEL", owner, profile.Spec.OS.Family, distribution))
			continue
		}
		if !stringInSlice(profile.Spec.OS.Version, release.RuntimeOS.ExactVersions) {
			errs = append(errs, fmt.Sprintf("%s.version %q is incompatible with Ceph distribution %q release %q; supported RHEL versions are %s", owner, profile.Spec.OS.Version, distribution, release.Value, strings.Join(release.RuntimeOS.ExactVersions, ", ")))
		}
	}
	return errs
}

func validateStorageCephFIPS(cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile) []string {
	if !cluster.Spec.Ceph.Security.FIPS.Enabled {
		return nil
	}
	prefix := fmt.Sprintf("StorageCluster/%s spec.ceph.security.fips.enabled", cluster.Metadata.Name)
	if storageCephDistribution(cluster) == v1alpha1.StorageCephDistributionOSS {
		return []string{prefix + " requires distribution redhat or ibm"}
	}
	var errs []string
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		machine, ok := machines[node.MachineRef.Name]
		if !ok || machine.Spec.OS.InstallProfileRef.Name == "" {
			continue
		}
		profile, ok := installProfiles[machine.Spec.OS.InstallProfileRef.Name]
		if !ok {
			continue
		}
		if !profile.Spec.Customizations.Security.FIPS.Enabled {
			errs = append(errs, fmt.Sprintf("%s requires MachineInstallProfile/%s (Ceph host %s) spec.customizations.security.fips.enabled: true", prefix, profile.Metadata.Name, node.MachineRef.Name))
		}
	}
	return errs
}

func stringInSlice(value string, values []string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
