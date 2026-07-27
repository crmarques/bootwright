package inventory

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/roles"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

const versionLookupDate = "2026-05-21"
const currentVersionLookupDate = "2026-07-14"
const ansibleCoreLookupDate = "2026-07-24"
const pyvmomiLookupDate = "2026-06-11"

const (
	defaultSushyToolsVersion = "2.2.0"
	defaultPyvmomiVersion    = "9.1.0.0"
)

const OpenShiftClientsMirrorBase = "https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp"

const HelmMirrorBase = "https://mirror.openshift.com/pub/openshift-v4/clients/helm"

const helmMirrorChannel = "latest"

type ComponentPin struct {
	Name       string `yaml:"name" json:"name"`
	Version    string `yaml:"version" json:"version"`
	Source     string `yaml:"source" json:"source"`
	LookupDate string `yaml:"lookupDate" json:"lookupDate"`
}

func ComponentPins(state v1alpha1.State) []ComponentPin {
	pins := []ComponentPin{
		{Name: "ansible-core", Version: "2.20.7", Source: "https://pypi.org/project/ansible-core/", LookupDate: ansibleCoreLookupDate},
		{Name: "pip", Version: "26.1.2", Source: "https://pypi.org/project/pip/", LookupDate: currentVersionLookupDate},
		{Name: "go.yaml.in/yaml/v3", Version: "v3.0.4", Source: "https://go.yaml.in/yaml/v3", LookupDate: versionLookupDate},
	}
	if usesSushyTools(state) {
		pins = append(pins, ComponentPin{Name: "sushy-tools", Version: defaultSushyToolsVersion, Source: "https://pypi.org/project/sushy-tools/", LookupDate: versionLookupDate})
	}
	if usesVSphere(state) {
		pins = append(pins, ComponentPin{Name: "pyvmomi", Version: defaultPyvmomiVersion, Source: "https://pypi.org/project/pyvmomi/", LookupDate: pyvmomiLookupDate})
	}
	for _, gate := range servicePinGates {
		if gate.uses(state) {
			pins = appendServicePin(pins, state, gate.key.Kind, gate.key.Realisation)
		}
	}
	for _, version := range openshiftInstallVersions(state) {
		pins = append(pins, ComponentPin{
			Name:       "openshift-install",
			Version:    version,
			Source:     OpenShiftClientsReleaseURL(state, version) + "/",
			LookupDate: versionLookupDate,
		})
	}
	return pins
}

func OpenShiftClientsReleaseURL(state v1alpha1.State, version string) string {
	base := OpenShiftClientsMirrorBase
	if env := stateview.Environment(state); env != nil {
		if m := strings.TrimSpace(env.Spec.Defaults.ClientsMirror); m != "" {
			base = strings.TrimRight(m, "/")
		}
	}
	return base + "/" + version
}

func HelmReleaseURL(state v1alpha1.State) string {
	base := HelmMirrorBase
	if env := stateview.Environment(state); env != nil {
		if m := strings.TrimSpace(env.Spec.Defaults.HelmMirror); m != "" {
			base = strings.TrimRight(m, "/")
		}
	}
	return base + "/" + helmMirrorChannel
}

func VirtctlMirrorOverride(state v1alpha1.State) string {
	if env := stateview.Environment(state); env != nil {
		if m := strings.TrimSpace(env.Spec.Defaults.VirtctlMirror); m != "" {
			return strings.TrimRight(m, "/")
		}
	}
	return ""
}

var servicePinGates = []struct {
	key  roles.ServiceKey
	uses func(v1alpha1.State) bool
}{
	{roles.ServiceKey{Kind: v1alpha1.ComponentSlotLoadBalancer, Realisation: v1alpha1.InfraComponentTypeHAProxy}, usesManagedHAProxy},
	{roles.ServiceKey{Kind: v1alpha1.ComponentSlotRegistry, Realisation: v1alpha1.InfraComponentTypeMirrorRegistry}, usesManagedMirrorRegistry},
	{roles.ServiceKey{Kind: v1alpha1.ComponentSlotProxy, Realisation: v1alpha1.InfraComponentTypeSquid}, usesManagedProxy},
	{roles.ServiceKey{Kind: v1alpha1.ComponentSlotNameResolution, Realisation: v1alpha1.InfraComponentTypeDnsmasq}, usesManagedDNS},
	{roles.ServiceKey{Kind: v1alpha1.ComponentSlotArtifactServer, Realisation: v1alpha1.ArtifactServerProtocolHTTP}, usesManagedArtifacts},
}

func appendServicePin(pins []ComponentPin, state v1alpha1.State, kind, realisation string) []ComponentPin {
	image, ok := roles.ServiceImagePin(kind, realisation)
	if !ok {
		return pins
	}
	pin := ComponentPin{
		Name:       image.Type,
		Version:    image.Version,
		Source:     image.Source,
		LookupDate: image.LookupDate,
	}
	fallback := image.Repository + ":" + image.Version
	if effective := managedComponentImage(state, image.Category, image.Type, fallback); effective != fallback {
		pin.Source = effective
		if tag := imageRefTag(effective); tag != "" {
			pin.Version = tag
		}
	}
	return append(pins, pin)
}

func imageRefTag(ref string) string {
	segment := ref
	if slash := strings.LastIndex(ref, "/"); slash >= 0 {
		segment = ref[slash+1:]
	}
	if colon := strings.LastIndex(segment, ":"); colon >= 0 {
		return segment[colon+1:]
	}
	return ""
}

func openshiftInstallVersions(state v1alpha1.State) []string {
	seen := map[string]bool{}
	var out []string
	for _, ocp := range state.ContainerClusters {
		if v1alpha1.DistributionType(ocp) != v1alpha1.DistributionOpenShift || ocp.Spec.Distribution.Release.Version == "" {
			continue
		}
		if seen[ocp.Spec.Distribution.Release.Version] {
			continue
		}
		seen[ocp.Spec.Distribution.Release.Version] = true
		out = append(out, ocp.Spec.Distribution.Release.Version)
	}
	return out
}

func usesSushyTools(state v1alpha1.State) bool {
	for _, p := range state.InfraProviders {
		if p.Spec.Type != v1alpha1.ProvisionerLibvirt || p.Spec.Libvirt == nil {
			continue
		}
		if len(stateview.ProviderMachineProfiles(p)) == 0 {
			continue
		}
		d := p.Spec.Libvirt.BMCEmulationDefaults
		if d == nil {
			return true
		}
		enabled := d.Enabled == nil || *d.Enabled
		if enabled && (d.Emulator == "" || d.Emulator == v1alpha1.DefaultBMCEmulator) {
			return true
		}
	}
	return false
}

func usesVSphere(state v1alpha1.State) bool {
	for _, p := range state.InfraProviders {
		if p.Spec.Type != v1alpha1.ProvisionerVSphere || p.Spec.VSphere == nil {
			continue
		}
		if len(stateview.ProviderMachineProfiles(p)) > 0 {
			return true
		}
	}
	return false
}

func usesManagedHAProxy(state v1alpha1.State) bool {
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInstallForOCP(state, ocp)
		if err != nil {
			continue
		}
		if len(loadBalancerComponentsForCluster(state, ci, ocp)) > 0 {
			return true
		}
	}
	return false
}

func usesManagedMirrorRegistry(state v1alpha1.State) bool {
	for _, ocp := range state.ContainerClusters {
		if _, ok := registryComponentForCluster(state, ocp); ok {
			return true
		}
	}
	return false
}

func usesManagedProxy(state v1alpha1.State) bool {
	return len(proxyComponentsForCluster(state)) > 0
}

func usesManagedDNS(state v1alpha1.State) bool {
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInstallForOCP(state, ocp)
		if err != nil {
			continue
		}
		if len(nameResolutionComponentsForCluster(state, ci)) > 0 {
			return true
		}
	}
	return false
}

func usesManagedArtifacts(state v1alpha1.State) bool {
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInstallForOCP(state, ocp)
		if err != nil {
			continue
		}
		if len(artifactServersForContainerCluster(state, ci, ocp)) > 0 {
			return true
		}
	}
	for _, cluster := range ManagedStorageClusters(state) {
		ci, ok := stateview.StorageClusterArtifactInstall(state, cluster)
		if !ok {
			continue
		}
		if storageUsesManagedArtifacts(state, ci) {
			return true
		}
	}
	return false
}

func storageUsesManagedArtifacts(state v1alpha1.State, ci v1alpha1.ClusterInstall) bool {
	for _, m := range ci.Machines {
		machine, ok := stateview.Machine(state, m.Source.MachineRef.Name)
		if !ok || !v1alpha1.MachineInstallsOS(machine) {
			continue
		}
		profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
		if !ok || profile.Spec.Installer.Anaconda == nil {
			continue
		}
		if server, _, ok := artifacts.ResolveEndpointRef(state, profile.Spec.Installer.Anaconda.RedfishVirtualMedia.ArtifactServerEndpoint); ok && server.Config != nil {
			return true
		}
		if hostedTree := profile.Spec.Installer.Anaconda.PackageSource.GetHostedTree(); hostedTree != nil {
			if server, _, ok := artifacts.ResolveEndpointRef(state, hostedTree.ArtifactServerEndpoint); ok && server.Config != nil {
				return true
			}
		}
	}
	return false
}
