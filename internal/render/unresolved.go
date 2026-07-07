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

// checkResolvedNames is render's second enforcement line behind Validate. The
// state/view and storage/topology resolution helpers degrade to empty strings
// on unresolvable names and their render consumers guard with
// `if address != ""`, so a validator gap (or a render call on state that never
// passed Validate) would otherwise ship install-configs with empty VIPs, DNS
// records without addresses, or a silently shrunken Ceph monitor list. The
// render entry points call this before writing anything so the failure is a
// hard error naming each unresolved name instead of degraded output.
func checkResolvedNames(state v1alpha1.State) error {
	events := unresolvedNames(state)
	if len(events) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to render output that would silently drop unresolved names: %s", strings.Join(events, "; "))
}

// unresolvedNames re-runs every name resolution the renderer consumes through
// an empty-string guard and reports each one that does not resolve. Keep this
// in step with the guarded call sites: when a new consumer reads a resolver
// that returns "" on failure, add the matching check here.
// stateview.MachineRouteAddress is deliberately not checked: its only render
// consumer is the managed-proxy URL, whose resolution failure already returns
// a hard error from proxy.ManagedProxyURL.
func unresolvedNames(state v1alpha1.State) []string {
	events := append(unresolvedEndpointBinds(state), unresolvedStorageNodeAddresses(state)...)
	return append(events, unresolvedMachineOSInstallImages(state)...)
}

// unresolvedEndpointBinds mirrors the resolution behind
// stateview.EndpointAddress: an install endpoint sourced from an
// infraComponent load balancer must resolve to exactly one bindAddresses[]
// entry, or the installer platform VIPs (installer_platform.go) and the
// cluster DNS records (dns_records.go) render empty.
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

// unresolvedStorageNodeAddresses mirrors the resolution behind
// topology.NodeAddress / NodeAddressByRef: every managed Ceph topology host
// and the cephadm bootstrap host must resolve to a machine address, or the
// storage inventory ansible_host, the bootstrap monIP, and
// topology.MonitorEndpoints silently drop entries.
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

// unresolvedMachineOSInstallImages mirrors the resolution behind
// machineOSInstallVars (inventory/vars_machine_os_install.go): every machine a
// managed Ceph topology slates for a managed-OS install resolves its Anaconda
// MachineImage ISO reference through media.Resolve, and a resolution failure
// makes that consumer return nil — silently dropping the machine from the
// managed-OS install group instead of installing its OS. The SSH-access,
// install-profile, and image-reference guards short-circuit exactly as the
// consumer does, so a machine that legitimately carries no Anaconda OS install
// is skipped and only a genuinely-unresolvable ISO reference is reported.
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
