package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
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
	return append(unresolvedEndpointBinds(state), unresolvedStorageNodeAddresses(state)...)
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
					ocp.Metadata.Name, name, source.ComponentRef.Name, source.BindAddressRef))
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
