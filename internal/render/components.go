package render

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/support"
	"github.com/crmarques/bootwright/internal/state/view"
)

// Lookup date constants are freshness stamps on the pinned versions below.
// Bump the matching constant whenever a pin is updated.
const versionLookupDate = "2026-05-21"
const currentVersionLookupDate = "2026-05-21"
const ansibleCoreLookupDate = "2026-05-31"

const (
	defaultSushyToolsVersion = "2.2.0"
)

// OpenShiftClientsMirrorBase is the canonical upstream base URL for downloading
// oc, kubectl, and openshift-install. The openshift-install ComponentPin source
// and the controller CLI install both derive from it; an Environment
// defaults.clientsMirror overrides it for disconnected labs.
const OpenShiftClientsMirrorBase = "https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp"

type ComponentPin struct {
	Name       string `yaml:"name" json:"name"`
	Version    string `yaml:"version" json:"version"`
	Source     string `yaml:"source" json:"source"`
	LookupDate string `yaml:"lookupDate" json:"lookupDate"`
}

// ComponentPins enumerates the runtime tools and container images
// Bootwright pins. Includes only tools whose use is implied by the
// loaded state (sushy-tools when libvirt BMC emulation is on, haproxy
// when an LB capability is referenced, etc.).
func ComponentPins(state v1alpha1.State) []ComponentPin {
	pins := []ComponentPin{
		{Name: "ansible-core", Version: "2.21.0", Source: "https://pypi.org/project/ansible-core/", LookupDate: ansibleCoreLookupDate},
		{Name: "pip", Version: "26.1.1", Source: "https://pypi.org/project/pip/", LookupDate: currentVersionLookupDate},
		{Name: "go.yaml.in/yaml/v3", Version: "v3.0.4", Source: "https://go.yaml.in/yaml/v3", LookupDate: versionLookupDate},
	}
	if usesSushyTools(state) {
		pins = append(pins, ComponentPin{Name: "sushy-tools", Version: defaultSushyToolsVersion, Source: "https://pypi.org/project/sushy-tools/", LookupDate: versionLookupDate})
	}
	for _, gate := range servicePinGates {
		if gate.uses(state) {
			pins = appendServicePin(pins, gate.key.Kind, gate.key.Realisation)
		}
	}
	for _, version := range openshiftInstallVersions(state) {
		pins = append(pins, ComponentPin{
			Name:       "openshift-install",
			Version:    version,
			Source:     OpenShiftClientsMirrorBase + "/" + version + "/",
			LookupDate: versionLookupDate,
		})
	}
	return pins
}

// OpenShiftClientsReleaseURL is the release-scoped base URL the controller CLI
// install fetches oc/openshift-install and their checksums from. It honors an
// Environment defaults.clientsMirror override and otherwise uses the pinned
// upstream mirror, keeping the install source renderer-owned instead of
// hardcoded in the Ansible role.
func OpenShiftClientsReleaseURL(state v1alpha1.State, version string) string {
	base := OpenShiftClientsMirrorBase
	if env := stateview.Environment(state); env != nil {
		if m := strings.TrimSpace(env.Spec.Defaults.ClientsMirror); m != "" {
			base = strings.TrimRight(m, "/")
		}
	}
	return base + "/" + version
}

// servicePinGates maps each pinnable managed service to the predicate that
// decides whether the loaded state actually uses it. The set of keys here must
// match support.PinnableServiceKeys(); TestServicePinGatesCoverPinnableServices
// enforces that so a new image-bearing registry entry cannot ship without a pin.
var servicePinGates = []struct {
	key  support.ServiceKey
	uses func(v1alpha1.State) bool
}{
	{support.ServiceKey{Kind: v1alpha1.ComponentSlotLoadBalancer, Realisation: v1alpha1.InfraComponentTypeHAProxy}, usesManagedHAProxy},
	{support.ServiceKey{Kind: v1alpha1.ComponentSlotRegistry, Realisation: v1alpha1.InfraComponentTypeMirrorRegistry}, usesManagedMirrorRegistry},
	{support.ServiceKey{Kind: v1alpha1.ComponentSlotProxy, Realisation: v1alpha1.InfraComponentTypeSquid}, usesManagedProxy},
	{support.ServiceKey{Kind: v1alpha1.ComponentSlotNameResolution, Realisation: v1alpha1.InfraComponentTypeDnsmasq}, usesManagedDNS},
	{support.ServiceKey{Kind: v1alpha1.ComponentSlotArtifacts, Realisation: v1alpha1.ArtifactServerProtocolHTTP}, usesManagedArtifacts},
}

func appendServicePin(pins []ComponentPin, kind, realisation string) []ComponentPin {
	image, ok := support.ServiceImagePin(kind, realisation)
	if !ok {
		return pins
	}
	return append(pins, ComponentPin{
		Name:       image.Type,
		Version:    image.Version,
		Source:     image.Source,
		LookupDate: image.LookupDate,
	})
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
		server, ok := artifacts.Select(state, ci)
		if ok && server.Config != nil && artifacts.ClusterNeedsPublication(state, ci, ocp) {
			return true
		}
	}
	return false
}
