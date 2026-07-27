package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render/ceph"
	"github.com/crmarques/bootwright/internal/render/installer"
	"github.com/crmarques/bootwright/internal/render/inventory"
	"github.com/crmarques/bootwright/internal/roles"
)

type (
	InstallerAsset           = installer.InstallerAsset
	InstallerManifest        = installer.InstallerManifest
	InstallerSecretInputStat = installer.InstallerSecretInputStat
	PathOptions              = inventory.PathOptions
	StorageAsset             = ceph.StorageAsset
)

const (
	InstallerRelativeDir = installer.InstallerRelativeDir
	RuntimeRelativeDir   = installer.RuntimeRelativeDir

	GroupProviderHosts       = inventory.GroupProviderHosts
	GroupInfraComponentHosts = inventory.GroupInfraComponentHosts
	GroupInfraHosts          = inventory.GroupInfraHosts
	GroupControllerHosts     = inventory.GroupControllerHosts
	GroupOCPHosts            = inventory.GroupOCPHosts
	GroupBootHosts           = inventory.GroupBootHosts
	GroupMachineTaskHosts    = inventory.GroupMachineTaskHosts
	GroupStorageHosts        = inventory.GroupStorageHosts
)

func InstallerAssets(clustersDir string, state v1alpha1.State) []InstallerAsset {
	return installer.InstallerAssets(clustersDir, state)
}

func InstallerConfig(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (map[string]any, error) {
	return installer.InstallerConfig(state, ocp)
}

func AgentConfig(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (map[string]any, error) {
	return installer.AgentConfig(state, ocp)
}

func InstallerManifests(ocp v1alpha1.ContainerCluster, secrets installer.InstallerSecrets) []InstallerManifest {
	return installer.InstallerManifests(ocp, secrets)
}

func PlaceholderInstallerSecrets(state v1alpha1.State, ocp v1alpha1.ContainerCluster) installer.InstallerSecrets {
	return installer.PlaceholderInstallerSecrets(state, ocp)
}

func InstallerSecretInputStatsForContext(contextName string, state v1alpha1.State, ocp v1alpha1.ContainerCluster, secretsDir string) ([]InstallerSecretInputStat, error) {
	return installer.InstallerSecretInputStatsForContext(contextName, state, ocp, secretsDir)
}

func ComponentPins(state v1alpha1.State) []inventory.ComponentPin {
	return inventory.ComponentPins(state)
}

func SSHCommonArgWords(knownHostsPath string) []string {
	return inventory.SSHCommonArgWords(knownHostsPath)
}

func OpenShiftClientsReleaseURL(state v1alpha1.State, version string) string {
	return inventory.OpenShiftClientsReleaseURL(state, version)
}

func HelmReleaseURL(state v1alpha1.State) string {
	return inventory.HelmReleaseURL(state)
}

func VirtctlMirrorOverride(state v1alpha1.State) string {
	return inventory.VirtctlMirrorOverride(state)
}

func HostGroupCountsWithOwnershipRecords(state v1alpha1.State, records []ownership.ResourceRecord) map[string]int {
	return inventory.HostGroupCountsWithOwnershipRecords(state, records)
}

func HostGroupMembers(state v1alpha1.State) map[string][]string {
	return inventory.HostGroupMembers(state)
}

func HostGroupMembersWithOwnershipRecords(state v1alpha1.State, records []ownership.ResourceRecord) map[string][]string {
	return inventory.HostGroupMembersWithOwnershipRecords(state, records)
}

func AgentNodeGroupName(clusterName string) string {
	return inventory.AgentNodeGroupName(clusterName)
}

func ManagedOSGroupName(clusterName string) string {
	return inventory.ManagedOSGroupName(clusterName)
}

func ManagedOSHostName(clusterName, machineName string) string {
	return inventory.ManagedOSHostName(clusterName, machineName)
}

func MachineInfraHostName(clusterName, machineName string) string {
	return inventory.MachineInfraHostName(clusterName, machineName)
}

func MachineInventoryHosts(state v1alpha1.State, machineName string) []string {
	return inventory.MachineInventoryHosts(state, machineName)
}

func StorageSeedHostName(cluster v1alpha1.StorageCluster) string {
	return inventory.StorageSeedHostName(cluster)
}

func StorageNodeHostName(clusterName, nodeName string) string {
	return inventory.StorageNodeHostName(clusterName, nodeName)
}

func StorageClusterGroupName(clusterName string) string {
	return inventory.StorageClusterGroupName(clusterName)
}

func FabricHostDesiredVars(state v1alpha1.State, host string) []any {
	return inventory.FabricHostDesiredVars(state, host)
}

func ProviderDriver(state v1alpha1.State, m v1alpha1.InstallMachine) roles.DispatchSupport {
	return inventory.ProviderDriver(state, m)
}
