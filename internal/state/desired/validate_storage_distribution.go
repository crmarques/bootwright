package desiredstate

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
	"github.com/crmarques/bootwright/internal/infra/media"
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
	entitlement, ok := entitlements.Find(state.Entitlements, ref)
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
	case v1alpha1.StorageCephDistributionRedHat, v1alpha1.StorageCephDistributionIBM:
		if !cephSubscriptionVersionPattern.MatchString(release) {
			return []string{fmt.Sprintf("%s.release %q must be a product version such as 9, 9.1, or 9.9.1; its leading major digit selects the product stream", prefix, release)}
		}
	}
	return nil
}

func validateStorageCephImage(prefix, image string) []string {
	if image == "" {
		return nil
	}
	if err := validatePinnedImageReference(image); err != "" {
		return []string{fmt.Sprintf("%s.image %q %s", prefix, image, err)}
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
	if mirror := community.Mirror; mirror != "" && !isHTTPURL(mirror) {
		errs = append(errs, fmt.Sprintf("%s.mirror %q must be an http or https URL", prefix, mirror))
	}
	if _, err := media.NormalizeSHA256(community.Checksum); err != nil {
		errs = append(errs, fmt.Sprintf("%s.checksum %s", prefix, err))
	}
	return errs
}

func isHTTPURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func validateStorageCephManagedOS(cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile) []string {
	distribution := storageCephDistribution(cluster)
	if distribution == v1alpha1.StorageCephDistributionOSS {
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
		if !storageCephDistributionSupportsRHELVersion(distribution, profile.Spec.OS.Version) {
			errs = append(errs, fmt.Sprintf("%s.version %q is incompatible with Ceph distribution %q; supported RHEL versions are %s", owner, profile.Spec.OS.Version, distribution, strings.Join(v1alpha1.StorageCephRHELVersions(), ", ")))
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

func storageCephDistributionSupportsRHELVersion(distribution, version string) bool {
	if distribution != v1alpha1.StorageCephDistributionRedHat && distribution != v1alpha1.StorageCephDistributionIBM {
		return true
	}
	return v1alpha1.StorageCephSupportsRHELVersion(version)
}
