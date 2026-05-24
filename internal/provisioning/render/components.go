package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/support"
)

// versionLookupDate is the freshness stamp on the pinned versions
// below. Bump whenever a pin is updated.
const versionLookupDate = "2026-05-21"
const currentVersionLookupDate = "2026-05-21"

const (
	defaultSushyToolsVersion = "2.2.0"
)

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
		{Name: "ansible-core", Version: "2.21.0", Source: "https://pypi.org/project/ansible-core/", LookupDate: versionLookupDate},
		{Name: "pip", Version: "26.1.1", Source: "https://pypi.org/project/pip/", LookupDate: currentVersionLookupDate},
		{Name: "go.yaml.in/yaml/v3", Version: "v3.0.4", Source: "https://go.yaml.in/yaml/v3", LookupDate: versionLookupDate},
	}
	if usesSushyTools(state) {
		pins = append(pins, ComponentPin{Name: "sushy-tools", Version: defaultSushyToolsVersion, Source: "https://pypi.org/project/sushy-tools/", LookupDate: versionLookupDate})
	}
	if usesManagedHAProxy(state) {
		pins = appendServicePin(pins, v1alpha1.ComponentSlotLoadBalancer, "haProxy")
	}
	if usesManagedMirrorRegistry(state) {
		pins = appendServicePin(pins, v1alpha1.ComponentSlotRegistry, "mirrorRegistry")
	}
	if usesManagedProxy(state) {
		pins = appendServicePin(pins, v1alpha1.ComponentSlotProxy, "squid")
	}
	if usesManagedDNS(state) {
		pins = appendServicePin(pins, v1alpha1.ComponentSlotNameResolution, "dnsmasq")
	}
	for _, version := range openshiftInstallVersions(state) {
		pins = append(pins, ComponentPin{
			Name:       "openshift-install",
			Version:    version,
			Source:     "https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/" + version + "/",
			LookupDate: versionLookupDate,
		})
	}
	return pins
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
		for _, mp := range p.Spec.MachineProfiles {
			l := mp.Libvirt
			if l == nil || l.BMCEmulationDefaults == nil {
				continue
			}
			d := l.BMCEmulationDefaults
			enabled := d.Enabled == nil || *d.Enabled
			if enabled && (d.Emulator == "" || d.Emulator == v1alpha1.DefaultBMCEmulator) {
				return true
			}
		}
	}
	return false
}

func usesManagedHAProxy(state v1alpha1.State) bool {
	for _, ci := range state.ClusterInfras {
		if len(ci.Spec.Components.LoadBalancers) > 0 {
			return true
		}
	}
	return false
}

func usesManagedMirrorRegistry(state v1alpha1.State) bool {
	for _, ci := range state.ClusterInfras {
		if ci.Spec.Components.Registry != nil {
			return true
		}
	}
	return false
}

func usesManagedProxy(state v1alpha1.State) bool {
	for _, ci := range state.ClusterInfras {
		if ci.Spec.Components.Proxy != nil {
			return true
		}
	}
	return false
}

func usesManagedDNS(state v1alpha1.State) bool {
	for _, ci := range state.ClusterInfras {
		if ci.Spec.Components.NameResolution != nil {
			return true
		}
	}
	return false
}
