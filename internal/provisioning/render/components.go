package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// versionLookupDate is the freshness stamp on the pinned versions
// below. Bump whenever a pin is updated.
const versionLookupDate = "2026-05-21"
const currentVersionLookupDate = "2026-05-21"

const (
	defaultSushyToolsVersion     = "2.2.0"
	defaultHAProxyVersion        = "3.3.10"
	defaultMirrorRegistryVersion = "3.1.1"
	defaultSquidVersion          = "7.5-oe2403sp3"
	defaultDnsmasqVersion        = "2.92_p2"
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
		pins = append(pins, ComponentPin{Name: v1alpha1.ComponentImageTypeHAProxy, Version: defaultHAProxyVersion, Source: "https://hub.docker.com/_/haproxy", LookupDate: versionLookupDate})
	}
	if usesManagedMirrorRegistry(state) {
		pins = append(pins, ComponentPin{Name: v1alpha1.ComponentImageTypeMirrorRegistry, Version: defaultMirrorRegistryVersion, Source: "https://hub.docker.com/_/registry", LookupDate: versionLookupDate})
	}
	if usesManagedProxy(state) {
		pins = append(pins, ComponentPin{Name: v1alpha1.ComponentImageTypeSquid, Version: defaultSquidVersion, Source: "https://hub.docker.com/r/openeuler/squid", LookupDate: versionLookupDate})
	}
	if usesManagedDNS(state) {
		pins = append(pins, ComponentPin{Name: v1alpha1.ComponentImageTypeDnsmasq, Version: defaultDnsmasqVersion, Source: "https://hub.docker.com/r/dockurr/dnsmasq", LookupDate: currentVersionLookupDate})
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
