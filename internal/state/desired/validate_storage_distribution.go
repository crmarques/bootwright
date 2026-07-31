package desiredstate

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/storage/cephprovider"
)

var (
	cephPackageVersionPattern = regexp.MustCompile(`^(?:[0-9]+:)?[0-9][0-9A-Za-z._+~^-]*$`)
	cephImageBasePattern      = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?(/[a-z0-9]+([._-][a-z0-9]+)*)+$`)
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

func validateStorageCephMgmtGatewayRelease(prefix string, cluster v1alpha1.StorageCluster) []string {
	distribution := storageCephDistribution(cluster)
	authored := cluster.Spec.Ceph.Release
	major, ok := cephprovider.UpstreamCephMajor(distribution, authored)
	if !ok || major >= cephprovider.MgmtGatewayMinimumCephMajor {
		return nil
	}
	resolved := authored
	if resolved == "" {
		resolved = cephprovider.DefaultRelease(distribution) + " (the distribution default)"
	}
	return []string{fmt.Sprintf("%s requires Ceph %d or later; spec.ceph.release %s is Ceph %d, which defines no mgmt-gateway or oauth2-proxy service and refuses the spec document at apply time",
		prefix, cephprovider.MgmtGatewayMinimumCephMajor, strconv.Quote(resolved), major)}
}

func validateStorageCephDistributionFamily(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
	var errs []string
	errs = append(errs, validateStorageCephDistribution(prefix, cluster, state)...)
	errs = append(errs, validateStorageCephRelease(prefix, storageCephDistribution(cluster), cluster.Spec.Ceph.Release)...)
	errs = append(errs, validateStorageCephPackageVersion(prefix, cluster)...)
	errs = append(errs, validateStorageCephImage(prefix+".image", cluster, state)...)
	errs = append(errs, validateStorageCephCommunity(prefix+".community", cluster)...)
	errs = append(errs, validateStorageCephIBM(prefix+".ibm", cluster)...)
	return errs
}

func validateStorageCephPackageVersion(prefix string, cluster v1alpha1.StorageCluster) []string {
	version := cluster.Spec.Ceph.PackageVersion
	if version == "" {
		return nil
	}
	if storageCephDistribution(cluster) == v1alpha1.StorageCephDistributionOSS {
		return []string{prefix + ".packageVersion must be empty when distribution=oss; the exact community build is named by .release as an x.y.z version, which selects the package repository"}
	}
	if !cephPackageVersionPattern.MatchString(version) {
		return []string{fmt.Sprintf("%s.packageVersion %q must be an RPM version-release such as 19.2.1-245.el9cp, optionally epoch-prefixed; it names the cephadm build to install and must not carry a package name, glob, or separator", prefix, version)}
	}
	return nil
}

func validateStorageCephImage(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
	image := cluster.Spec.Ceph.Image
	if image == nil {
		image = &v1alpha1.StorageCephImageSpec{}
	}
	errs := validateStorageCephImageParts(prefix, *image)
	distribution := storageCephDistribution(cluster)
	if !v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		return errs
	}
	vendorRegistry := cephprovider.DefaultRegistryURL(distribution)
	registry := vendorRegistry
	if entitlement, ok := v1alpha1.EntitlementByName(state.Entitlements, cluster.Spec.Ceph.EntitlementRef.Name); ok && entitlement.Spec.Registry != nil && entitlement.Spec.Registry.URL != "" {
		registry = entitlement.Spec.Registry.URL
		if registry != vendorRegistry && image.Base == "" {
			return append(errs, fmt.Sprintf("%s.base is required when Entitlement/%s spec.registry.url overrides the vendor registry; the derived base names the vendor registry, which a mirrored estate cannot pull", prefix, entitlement.Metadata.Name))
		}
	}
	if image.Base != "" {
		errs = append(errs, validateStorageCephImageBase(prefix, distribution, cluster.Spec.Ceph.Release, registry, image.Base)...)
	}
	return errs
}

func validateStorageCephImageParts(prefix string, image v1alpha1.StorageCephImageSpec) []string {
	var errs []string
	if image.Base != "" && !cephImageBasePattern.MatchString(image.Base) {
		errs = append(errs, fmt.Sprintf("%s.base %q must be a bare %s reference carrying no tag, digest, or scheme; the build belongs in .version", prefix, image.Base, "<registry>/<path>"))
	}
	if image.Version != "" && !imageVersionTag.MatchString(image.Version) && !imageSHA256Digest.MatchString(image.Version) {
		errs = append(errs, fmt.Sprintf("%s.version %q must be an image tag or a sha256: digest; a mutable tag such as latest is not a pin", prefix, image.Version))
	}
	if image.Base != "" && image.Version == "" {
		errs = append(errs, prefix+".base names no image until .version completes it")
	}
	return errs
}

func validateStorageCephImageBase(prefix, distribution, authored, registry, base string) []string {
	release, ok := cephprovider.ResolveRelease(distribution, authored)
	if !ok {
		return nil
	}
	vendorPrefix, ok := cephprovider.ImageRepositoryPrefix(distribution, authored, registry)
	if ok && !strings.HasPrefix(base, vendorPrefix) {
		return []string{fmt.Sprintf("%s.base must start with %q for %s release %s; the vendor namespace and stream must match the cluster, the trailing build base is yours to declare", prefix, vendorPrefix, distribution, release.Value)}
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
