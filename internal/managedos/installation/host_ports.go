package installation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	machineref "github.com/crmarques/bootwright/internal/machine"
)

type HostPort struct {
	Number    int
	Transport string
}

type HostPortRequirement struct {
	Addresses []string
	Machine   string
	Publisher string
	Networks  []string
	Protocols []string
	Ports     []HostPort
}

type HostPortRequirements func(api.Catalog) ([]HostPortRequirement, error)

func RealizesHostPorts(catalog api.Catalog, machine string) bool {
	return machineref.ManagesFirewall(catalog, machine)
}
