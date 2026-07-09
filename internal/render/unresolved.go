package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/media"
	"github.com/crmarques/bootwright/internal/render/inventory"
	"github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func checkResolvedNames(state v1alpha1.State) error {
	events := unresolvedNames(state)
	if len(events) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to render output that would silently drop unresolved names: %s", strings.Join(events, "; "))
}

func unresolvedNames(state v1alpha1.State) []string {
	events := append(unresolvedEndpointBinds(state), unresolvedStorageNodeAddresses(state)...)
	return append(events, unresolvedMachineOSInstallImages(state)...)
}

func unresolvedEndpointBinds(state v1alpha1.State) []string {
	var events []string
	for _, ocp := range state.ContainerClusters {
		ci, ok := stateview.ClusterInstallForContainerCluster(state, ocp)
		if !ok {
			continue
		}
		names := make([]string, 0, len(ci.Endpoints))
		for name := range ci.Endpoints {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			source := ci.Endpoints[name].Source
			if source.Type != v1alpha1.EndpointSourceInfraComponent {
				continue
			}
			if _, ok := stateview.LoadBalancerBindAddress(state, source); !ok {
				events = append(events, fmt.Sprintf(
					"ContainerCluster/%s spec.install.endpoints.%s.source (componentRef %q, bindAddressRef %q) does not resolve to a loadBalancer bind address",
					ocp.Metadata.Name, name, source.ComponentRef.Name, source.BindAddressRef.Name))
			}
		}
	}
	return events
}

func unresolvedStorageNodeAddresses(state v1alpha1.State) []string {
	var events []string
	for _, cluster := range inventory.ManagedStorageClusters(state) {
		ceph := cluster.Spec.Ceph
		for i, host := range ceph.Topology.Hosts {
			if topology.NodeAddress(state, cluster, host.Hostname) == "" {
				events = append(events, fmt.Sprintf(
					"StorageCluster/%s spec.ceph.topology.hosts[%d].machineRef %q does not resolve to a machine address for host %q",
					cluster.Metadata.Name, i, host.MachineRef.Name, host.Hostname))
			}
		}
		bootstrap := ceph.Cephadm.Bootstrap
		if bootstrap.Host != "" && topology.NodeAddressByRef(state, cluster, bootstrap.Host, bootstrap.AddressRef.Name) == "" {
			events = append(events, fmt.Sprintf(
				"StorageCluster/%s spec.ceph.cephadm.bootstrap.host %q does not resolve to a machine address",
				cluster.Metadata.Name, bootstrap.Host))
		}
	}
	return events
}

func unresolvedMachineOSInstallImages(state v1alpha1.State) []string {
	var events []string
	for _, cluster := range inventory.ManagedStorageClusters(state) {
		seen := map[string]bool{}
		for i, host := range cluster.Spec.Ceph.Topology.Hosts {
			name := host.MachineRef.Name
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			machine, ok := stateview.Machine(state, name)
			if !ok || !v1alpha1.MachineInstallsOS(machine) {
				continue
			}
			if machine.Spec.Access.SSH == nil {
				continue
			}
			profile, ok := stateview.MachineInstallProfile(state, machine.Spec.OS.InstallProfileRef.Name)
			if !ok || profile.Spec.Installer.Anaconda == nil {
				continue
			}
			image, ok := stateview.MachineImage(state, profile.Spec.Installer.Anaconda.ImageRef.Name)
			if !ok {
				continue
			}
			if _, err := media.Resolve(image.Spec.BootMedia); err != nil {
				events = append(events, fmt.Sprintf(
					"StorageCluster/%s spec.ceph.topology.hosts[%d].machineRef %q MachineImage %q spec.bootMedia %q does not resolve to installable media: %v",
					cluster.Metadata.Name, i, name, image.Metadata.Name, image.Spec.BootMedia, err))
			}
		}
	}
	return events
}
