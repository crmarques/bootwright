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
	var errs []string
	ref := cluster.Spec.Ceph.EntitlementRef.Name
	switch distribution {
	case v1alpha1.StorageCephDistributionOSS:
		if ref != "" {
			errs = append(errs, prefix+".entitlementRef must be empty when distribution=oss")
		}
	case v1alpha1.StorageCephDistributionRedHat:
		errs = append(errs, validateStorageCephDistributionEntitlement(prefix, state, ref, v1alpha1.EntitlementTypeRedHatCeph)...)
	case v1alpha1.StorageCephDistributionIBM:
		errs = append(errs, validateStorageCephDistributionEntitlement(prefix, state, ref, v1alpha1.EntitlementTypeIBMStorageCeph)...)
	}
	if osRef := cluster.Spec.Ceph.OSSubscriptionRef.Name; osRef != "" {
		if ent, ok := v1alpha1.EntitlementByName(state.Entitlements, osRef); !ok {
			errs = append(errs, fmt.Sprintf("%s.osSubscriptionRef %q does not match any Entitlement", prefix, osRef))
		} else if ent.Spec.Type != v1alpha1.EntitlementTypeRedHatRHEL {
			errs = append(errs, fmt.Sprintf("%s.osSubscriptionRef %q resolves to type %q, want %q", prefix, osRef, ent.Spec.Type, v1alpha1.EntitlementTypeRedHatRHEL))
		}
	}
	return errs
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
	if _, ok := cephprovider.ResolveRelease(distribution, release); ok {
		return nil
	}
	if distribution == v1alpha1.StorageCephDistributionOSS {
		return []string{fmt.Sprintf("%s.release %q must be an upstream Ceph release name (e.g. squid) or an x.y.z version (e.g. 19.2.1)", prefix, release)}
	}
	return []string{fmt.Sprintf("%s.release %q must be a dot-separated numeric product version such as 9, 9.1, or 9.9.1.0; its leading component selects the product stream", prefix, release)}
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
		if image != "" {
			return validateStorageCephImageRepository(prefix, distribution, cluster.Spec.Ceph.Release, registry, image)
		}
	}
	return nil
}

func validateStorageCephImageRepository(prefix, distribution, authored, registry, image string) []string {
	release, ok := cephprovider.ResolveRelease(distribution, authored)
	if !ok {
		return nil
	}
	vendorPrefix, ok := cephprovider.ImageRepositoryPrefix(distribution, authored, registry)
	if ok && !strings.HasPrefix(cephprovider.ImageRepository(image), vendorPrefix) {
		return []string{fmt.Sprintf("%s.image repository must start with %q for %s release %s; the vendor namespace and stream must match the cluster, the trailing build base is yours to declare", prefix, vendorPrefix, distribution, release.Value)}
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
	if storageCephDistribution(cluster) == v1alpha1.StorageCephDistributionOSS {
		return nil
	}
	var errs []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		machine, ok := machines[node.MachineRef.Name]
		if !ok || machine.Spec.OS.InstallProfileRef.Name == "" {
			continue
		}
		profile, ok := installProfiles[machine.Spec.OS.InstallProfileRef.Name]
		if !ok || strings.ToLower(profile.Spec.OS.Family) == v1alpha1.MachineInstallOSFamilyRHEL {
			continue
		}
		errs = append(errs, fmt.Sprintf("StorageCluster/%s spec.ceph.topology.nodes[%s] MachineInstallProfile/%s spec.os.family %q is not RHEL; the subscription-backed Ceph provider only implements RHEL-family package sources", cluster.Metadata.Name, node.Name, profile.Metadata.Name, profile.Spec.OS.Family))
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
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
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
