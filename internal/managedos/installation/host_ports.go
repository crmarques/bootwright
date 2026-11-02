package installation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type HostPort struct {
	Number    int
	Transport string
}

type HostPortRequirement struct {
	Machine   string
	Publisher string
	Networks  []string
	Protocols []string
	Ports     []HostPort
}

type HostPortRequirements func(api.Catalog) ([]HostPortRequirement, error)

func RealizesHostPorts(catalog api.Catalog, machine string) bool {
	for _, object := range InstalledMachines(catalog) {
		if object.Name() != machine {
			continue
		}
		profile, found := catalog.Find(api.MachineInstallProfile, object.Spec().Get("os", "installProfileRef").Text())
		return found && firewallChoice(profile.Spec().Get("customizations")) == "enabled"
	}
	return false
}
