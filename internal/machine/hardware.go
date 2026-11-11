package machine

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const HardwareFamily = "machine-hardware"

func ProvesHardware(catalog api.Catalog, placement Placement) bool {
	if placement.Connection != ConnectionSSH {
		return false
	}
	host, found := catalog.Find(api.Machine, placement.Machine)
	return found && !installed(host)
}

func HardwareHosts(catalog api.Catalog, controllerMachine string) ([]Placement, error) {
	var placements []Placement
	for _, host := range catalog.OfKind(api.Machine) {
		if host.Name() == controllerMachine || installed(host) {
			continue
		}
		if placementHost(host, catalog) {
			placement, err := PlacementFor(host, controllerMachine)
			if err != nil {
				return nil, err
			}
			placements = append(placements, placement)
			continue
		}
		if placement, dialed := clusterDialed(catalog, host); dialed {
			placements = append(placements, placement)
		}
	}
	return placements, nil
}

func clusterNode(catalog api.Catalog, machine string) bool {
	for _, kind := range api.ClusterKinds() {
		for _, cluster := range catalog.OfKind(kind) {
			if slices.Contains(api.ClusterNodeMachines(cluster), machine) {
				return true
			}
		}
	}
	return false
}

func clusterDialed(catalog api.Catalog, host api.Object) (Placement, bool) {
	fleet, declared := FleetKey(catalog)
	ssh, reached := SSH(host)
	if !declared || !reached || !host.Spec().Get("os", "provided").Bool() || ssh.User == "" || ssh.KnownHostsRef == "" || !clusterNode(catalog, host.Name()) {
		return Placement{}, false
	}
	return Placement{Address: ssh.Address, Connection: ConnectionSSH, KnownHostsRef: ssh.KnownHostsRef, Machine: host.Name(), Port: ssh.Port, PrivateKeyRef: fleet, User: ssh.User}, true
}
