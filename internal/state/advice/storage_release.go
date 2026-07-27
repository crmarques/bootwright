package advice

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/cephprovider"
)

const cephReleaseCatalogGroup = "Ceph release catalog"

func storageReleaseAdvisories(object string, state v1alpha1.State, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	distribution := cephprovider.Distribution(cluster)
	release, ok := cephprovider.ResolveRelease(distribution, cluster.Spec.Ceph.Release)
	if !ok {
		return nil
	}
	var out []StorageAdvisory
	if !release.Cataloged {
		out = append(out, uncatalogedReleaseAdvisory(object, distribution, release))
	}
	if advisory, ok := runtimeOSAdvisory(object, state, cluster, distribution, release); ok {
		out = append(out, advisory)
	}
	return out
}

func uncatalogedReleaseAdvisory(object, distribution string, release cephprovider.ResolvedRelease) StorageAdvisory {
	impact := "Bootwright derives the repository, image, and runtime-OS coordinates from the release string instead of recorded vendor facts, so a wrong release name surfaces only when the install pulls"
	remediation := "confirm the release against the vendor lifecycle documentation"
	if v1alpha1.StorageCephDistributionSubscriptionBacked(distribution) {
		remediation += fmt.Sprintf(" and pin spec.ceph.image, since the daemon image build base is assumed to be rhel%s", release.ImageOSMajor)
	}
	return StorageAdvisory{
		Severity:    SeverityWarn,
		Group:       cephReleaseCatalogGroup,
		Object:      object,
		Finding:     fmt.Sprintf("release %q is outside the %s releases Bootwright carries facts for (%s)", release.Value, distribution, strings.Join(cephprovider.CatalogedReleases(distribution), ", ")),
		Impact:      impact,
		Remediation: remediation,
	}
}

func runtimeOSAdvisory(object string, state v1alpha1.State, cluster v1alpha1.StorageCluster, distribution string, release cephprovider.ResolvedRelease) (StorageAdvisory, bool) {
	if len(release.RuntimeOS.ExactVersions) == 0 {
		return StorageAdvisory{}, false
	}
	versions := map[string][]string{}
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		version, ok := cephNodeInstallOSVersion(state, node)
		if !ok || cephprovider.RuntimeOSVersionCataloged(release.RuntimeOS, version) {
			continue
		}
		versions[version] = append(versions[version], node.Name)
	}
	if len(versions) == 0 {
		return StorageAdvisory{}, false
	}
	return StorageAdvisory{
		Severity:    SeverityWarn,
		Group:       cephReleaseCatalogGroup,
		Object:      object,
		Finding:     fmt.Sprintf("storage node RHEL %s is outside the versions recorded for %s release %s (%s)", formatRuntimeOSFindings(versions), distribution, release.Value, strings.Join(release.RuntimeOS.ExactVersions, ", ")),
		Impact:      "the combination is untested here and may hit vendor package or ABI mismatches at install time; apply proceeds because the recorded matrix is a snapshot, not a contract",
		Remediation: "check the vendor compatibility guide, then either move the nodes' MachineInstallProfile spec.os.version onto a recorded version or accept the combination knowingly",
	}, true
}

func cephNodeInstallOSVersion(state v1alpha1.State, node v1alpha1.StorageCephNode) (string, bool) {
	machine, ok := stateview.Machine(state, node.MachineRef.Name)
	if !ok || machine.Spec.OS.InstallProfileRef.Name == "" {
		return "", false
	}
	profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
	if !ok || strings.ToLower(profile.Spec.OS.Family) != v1alpha1.MachineInstallOSFamilyRHEL {
		return "", false
	}
	version := strings.TrimSpace(profile.Spec.OS.Version)
	if version == "" {
		return "", false
	}
	return version, true
}

func formatRuntimeOSFindings(versions map[string][]string) string {
	keys := make([]string, 0, len(versions))
	for version := range versions {
		keys = append(keys, version)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, version := range keys {
		nodes := versions[version]
		sort.Strings(nodes)
		parts = append(parts, fmt.Sprintf("%s on %s", version, strings.Join(nodes, ", ")))
	}
	return strings.Join(parts, "; ")
}
