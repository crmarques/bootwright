package desiredstate

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/storage/cephprovider"
)

var (
	cephPackageVersionPattern    = regexp.MustCompile(`^(?:[0-9]+:)?[0-9][0-9A-Za-z._+~^-]*$`)
	cephRPMVersionReleasePattern = regexp.MustCompile(`^(?:[0-9]+:)?[0-9][0-9A-Za-z._+~^]*-[0-9A-Za-z][0-9A-Za-z._+~^-]*$`)
	cephImageBasePattern         = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?(/[a-z0-9]+([._-][a-z0-9]+)*)+$`)
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
		return []string{prefix + ".release is required; managed Ceph versions are authored desired state and never selected from a compiled default"}
	}
	if _, ok := cephprovider.ResolveRelease(distribution, release); ok {
		return nil
	}
	if distribution == v1alpha1.StorageCephDistributionOSS {
		return []string{fmt.Sprintf("%s.release %q must be an upstream Ceph release name or an x.y.z version", prefix, release)}
	}
	return []string{fmt.Sprintf("%s.release %q must be a dot-separated numeric product version; its leading component selects the product stream", prefix, release)}
}

func validateStorageCephMgmtGatewayRelease(prefix string, cluster v1alpha1.StorageCluster) []string {
	distribution := storageCephDistribution(cluster)
	authored := cluster.Spec.Ceph.Release
	major, ok := cephprovider.UpstreamCephMajor(distribution, authored)
	if !ok || major >= cephprovider.MgmtGatewayMinimumCephMajor {
		return nil
	}
	return []string{fmt.Sprintf("%s requires Ceph %d or later; spec.ceph.release %q is Ceph %d, which defines no mgmt-gateway or oauth2-proxy service and refuses the spec document at apply time",
		prefix, cephprovider.MgmtGatewayMinimumCephMajor, authored, major)}
}

func validateStorageCephMgmtGatewayPortScheme(prefix string, mgmt *v1alpha1.StorageCephMgmtGateway) []string {
	if mgmt.Port == 0 {
		return nil
	}
	exposure := v1alpha1.StorageCephMgmtGatewayExposureEffective(mgmt)
	conventions := map[string]struct {
		ports   []int
		reputed string
	}{
		v1alpha1.StorageCephMgmtGatewayExposureHTTP:  {ports: []int{443, 8443}, reputed: "TLS"},
		v1alpha1.StorageCephMgmtGatewayExposureHTTPS: {ports: []int{80, 8080}, reputed: "cleartext"},
	}
	convention, ok := conventions[exposure]
	if !ok {
		return nil
	}
	for _, port := range convention.ports {
		if mgmt.Port != port {
			continue
		}
		return []string{fmt.Sprintf("%s.port %d contradicts exposure: %s — %d is a conventional %s port, and every operator, scanner and firewall rule that reads it will assume the scheme this gateway does not serve; author a port whose convention matches, or drop the field for the %s default %d",
			prefix, mgmt.Port, exposure, mgmt.Port, convention.reputed, exposure,
			v1alpha1.StorageCephMgmtGatewayDefaultPort(exposure))}
	}
	return nil
}

func validateStorageCephMgmtGatewayExposure(prefix string, cluster v1alpha1.StorageCluster) []string {
	mgmt := cluster.Spec.Ceph.MgmtGateway
	if mgmt == nil {
		return nil
	}
	switch mgmt.Exposure {
	case "", v1alpha1.StorageCephMgmtGatewayExposureHTTPS, v1alpha1.StorageCephMgmtGatewayExposureHTTP:
	default:
		return []string{fmt.Sprintf("%s.exposure %q must be %q or %q", prefix, mgmt.Exposure, v1alpha1.StorageCephMgmtGatewayExposureHTTPS, v1alpha1.StorageCephMgmtGatewayExposureHTTP)}
	}
	var errs []string
	errs = append(errs, validateStorageCephMgmtGatewayPortScheme(prefix, mgmt)...)
	if mgmt.Exposure == v1alpha1.StorageCephMgmtGatewayExposureHTTP {
		if mgmt.TLS != nil {
			errs = append(errs, fmt.Sprintf("%s.tls contradicts exposure: http — a plain-HTTP gateway serves no certificate; drop the tls block or set exposure: https", prefix))
		}
		if mgmt.OAuth2Proxy != nil {
			errs = append(errs, fmt.Sprintf("%s.oauth2Proxy requires exposure: https — SSO cookies and access tokens must not cross the network in cleartext", prefix))
		}
	}
	return errs
}

func validateStorageCephDistributionFamily(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
	var errs []string
	errs = append(errs, validateStorageCephDistribution(prefix, cluster, state)...)
	if v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph.Release != "" {
		errs = append(errs, validateStorageCephRelease(prefix, storageCephDistribution(cluster), cluster.Spec.Ceph.Release)...)
	}
	errs = append(errs, validateStorageCephPackageVersion(prefix, cluster)...)
	errs = append(errs, validateStorageCephadmAnsiblePackageVersion(prefix+".cephadm.ansible", cluster)...)
	errs = append(errs, validateStorageCephImage(prefix+".image", cluster, state)...)
	errs = append(errs, validateStorageCephCommunity(prefix+".community", cluster)...)
	errs = append(errs, validateStorageCephIBM(prefix+".ibm", cluster, state)...)
	return errs
}

func validateStorageCephPackageVersion(prefix string, cluster v1alpha1.StorageCluster) []string {
	policy, ok := cephprovider.ArtifactPolicyFor(storageCephDistribution(cluster))
	if !ok {
		return nil
	}
	return validateStorageCephRPMVersion(prefix+".packageVersion", cluster.Spec.Ceph.PackageVersion, policy.PackagePin, policy.RPMReleaseRequired, v1alpha1.StorageClusterManaged(cluster))
}

func validateStorageCephadmAnsiblePackageVersion(prefix string, cluster v1alpha1.StorageCluster) []string {
	policy, ok := cephprovider.ArtifactPolicyFor(storageCephDistribution(cluster))
	if !ok {
		return nil
	}
	version := ""
	if cluster.Spec.Ceph.Cephadm.Ansible != nil {
		version = cluster.Spec.Ceph.Cephadm.Ansible.PackageVersion
	}
	return validateStorageCephRPMVersion(prefix+".packageVersion", version, policy.CephadmAnsiblePackagePin, policy.CephadmAnsibleRPMReleaseRequired, v1alpha1.StorageClusterManaged(cluster))
}

func validateStorageCephRPMVersion(prefix, version string, pinPolicy cephprovider.ArtifactPinPolicy, releaseRequired, managed bool) []string {
	switch {
	case version == "" && pinPolicy == cephprovider.ArtifactPinRequired && managed:
		return []string{prefix + " is required by the selected distribution artifact policy"}
	case version == "":
		return nil
	case pinPolicy == cephprovider.ArtifactPinForbidden:
		return []string{prefix + " must be empty because the selected distribution artifact policy forbids a native package pin"}
	}
	if !cephPackageVersionPattern.MatchString(version) {
		return []string{fmt.Sprintf("%s %q must be an RPM version or version-release, optionally epoch-prefixed; it must not carry a package name, glob, or separator", prefix, version)}
	}
	if releaseRequired && !cephRPMVersionReleasePattern.MatchString(version) {
		return []string{prefix + " must include the RPM release component because the selected distribution artifact policy requires the full native build coordinate"}
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
	policy, ok := cephprovider.ArtifactPolicyFor(distribution)
	if !ok {
		return errs
	}
	if policy.ImageBaseRequired && image.Base == "" && v1alpha1.StorageClusterManaged(cluster) {
		errs = append(errs, prefix+".base is required by the selected distribution artifact policy")
	}
	if policy.ImagePinRequired && image.Version == "" && v1alpha1.StorageClusterManaged(cluster) {
		errs = append(errs, prefix+".version is required by the selected distribution artifact policy")
	}
	if !v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		return errs
	}
	vendorRegistry := cephprovider.DefaultRegistryURL(distribution)
	registry := vendorRegistry
	if entitlement, ok := v1alpha1.EntitlementByName(state.Entitlements, cluster.Spec.Ceph.EntitlementRef.Name); ok && entitlement.Spec.Registry != nil && entitlement.Spec.Registry.URL != "" {
		registry = entitlement.Spec.Registry.URL
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

func validateStorageCephIBM(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
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
	var errs []string
	switch ibm.CallHome {
	case v1alpha1.StorageCephIBMCallHomeEnabled, v1alpha1.StorageCephIBMCallHomeDisabled:
	default:
		errs = append(errs, fmt.Sprintf("%s.callHome %q must be one of {%s, %s}", prefix, ibm.CallHome, v1alpha1.StorageCephIBMCallHomeEnabled, v1alpha1.StorageCephIBMCallHomeDisabled))
	}
	errs = append(errs, validateStorageCephIBMPackages(prefix+".packages", cluster, state)...)
	return errs
}

func validateStorageCephIBMPackages(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
	packages := v1alpha1.StorageCephIBMPackages(cluster.Spec.Ceph)
	if packages == nil {
		return nil
	}
	var errs []string
	switch packages.Source {
	case v1alpha1.StorageCephIBMPackageSourceVendor:
		if len(packages.SubscriptionRepos) > 0 {
			errs = append(errs, prefix+".subscriptionRepos must be empty when source=vendor; the vendor source fetches the IBM repository definition from the public IBM download site instead")
		}
	case v1alpha1.StorageCephIBMPackageSourceSubscription:
		if len(packages.SubscriptionRepos) == 0 {
			errs = append(errs, prefix+".subscriptionRepos must name at least one RHSM repository serving the IBM Storage Ceph packages when source=subscription; the storage phase enables exactly these repositories and installs the pinned cephadm build and the license package from them")
		}
	default:
		errs = append(errs, fmt.Sprintf("%s.source %q must be one of {%s, %s}", prefix, packages.Source, v1alpha1.StorageCephIBMPackageSourceVendor, v1alpha1.StorageCephIBMPackageSourceSubscription))
	}
	seen := map[string]bool{}
	for i, id := range packages.SubscriptionRepos {
		owner := fmt.Sprintf("%s.subscriptionRepos[%d]", prefix, i)
		switch {
		case id == "":
			errs = append(errs, owner+" must not be empty")
		case strings.ContainsAny(id, " \t\"'"):
			errs = append(errs, fmt.Sprintf("%s %q must not contain whitespace or quotes", owner, id))
		case id == v1alpha1.MachineInstallSubscriptionRepoAllID:
			errs = append(errs, fmt.Sprintf("%s %q is not allowed; name concrete repository ids", owner, id))
		case seen[id]:
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, id))
		}
		seen[id] = true
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

func validateStorageCephOSDTPM2Stack(cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile) []string {
	fleetTPM2 := false
	for _, drivegroup := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		if drivegroup.OSD.TPM2 {
			fleetTPM2 = true
		}
	}
	var errs []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if !fleetTPM2 && (node.OSD == nil || !node.OSD.TPM2) {
			continue
		}
		machine, ok := machines[node.MachineRef.Name]
		if !ok || machine.Spec.OS.InstallProfileRef.Name == "" {
			continue
		}
		profile, ok := installProfiles[machine.Spec.OS.InstallProfileRef.Name]
		if !ok {
			continue
		}
		if storageCephNodeInstallsTPM2Stack(profile) {
			continue
		}
		errs = append(errs, fmt.Sprintf(
			"StorageCluster/%s osd.tpm2 covers Ceph host %s, and sealing the OSD key runs systemd-cryptenroll on that host, which dlopens the %s libraries. MachineInstallProfile/%s installs neither %s nor customizations.security.diskEncryption, and %s is only a weak dependency of systemd-udev, so a minimal install does not carry it and enrollment fails after the OSD is already created. Add %s to customizations.packages.install",
			cluster.Metadata.Name, node.Name, v1alpha1.TPM2StackPackage, profile.Metadata.Name,
			v1alpha1.TPM2StackPackage, v1alpha1.TPM2StackPackage, v1alpha1.TPM2StackPackage))
	}
	return errs
}

func storageCephNodeInstallsTPM2Stack(profile v1alpha1.MachineInstallProfile) bool {
	if profile.Spec.Customizations.Security.DiskEncryption != nil {
		return true
	}
	return machineInstallStringListContains(profile.Spec.Customizations.Packages.Install, v1alpha1.TPM2StackPackage)
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
