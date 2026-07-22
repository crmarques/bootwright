package topology

import "github.com/crmarques/bootwright/api/v1alpha1"

const SingleHostMinimumOSDs = 2

type osdReadinessInspection struct {
	managed            bool
	managedExact       bool
	count              int
	dynamicHosts       []string
	declaredCountKnown bool
}

func OSDReadinessExpectation(cluster v1alpha1.StorageCluster) (mode string, count int, dynamicHosts []string) {
	inspection := inspectOSDReadiness(cluster)
	if cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Cephadm.Bootstrap.SingleHostDefaults {
		switch {
		case !inspection.managed:
			return "atLeastOne", SingleHostMinimumOSDs, inspection.dynamicHosts
		case inspection.managedExact:
			return "exact", max(inspection.count, SingleHostMinimumOSDs), inspection.dynamicHosts
		default:
			return "atLeastOne", max(inspection.count+len(inspection.dynamicHosts), SingleHostMinimumOSDs), inspection.dynamicHosts
		}
	}
	switch {
	case !inspection.managed:
		return "skip", 0, inspection.dynamicHosts
	case inspection.managedExact:
		return "exact", inspection.count, inspection.dynamicHosts
	default:
		return "atLeastOne", inspection.count + len(inspection.dynamicHosts), inspection.dynamicHosts
	}
}

func DeclaredOSDCount(cluster v1alpha1.StorageCluster) (int, bool) {
	inspection := inspectOSDReadiness(cluster)
	return inspection.count, inspection.declaredCountKnown
}

func osdSelectionUsesAllDevices(selection *v1alpha1.StorageCephDeviceSelection) bool {
	return selection != nil && selection.All
}

func OSDHostUsesAllDevices(cluster v1alpha1.StorageCluster, host v1alpha1.StorageCephNode) bool {
	if cluster.Spec.Ceph == nil || !NodeHasRole(host, v1alpha1.StorageCephRoleOSD) {
		return false
	}
	if host.OSD != nil && osdSelectionUsesAllDevices(host.OSD.DataDevices) {
		return true
	}
	name := host.Name
	if name == "" {
		name = CanonicalHostname(cluster, host.MachineRef.Name)
	}
	for i := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		dg := cluster.Spec.Ceph.Topology.OSDDrivegroups[i]
		if !osdSelectionUsesAllDevices(dg.OSD.DataDevices) {
			continue
		}
		for _, placed := range ResolvePlacement(cluster, dg.Placement, v1alpha1.StorageCephRoleOSD) {
			if placed == name {
				return true
			}
		}
	}
	return false
}

func ClusterHasAllDevicesOSDHost(cluster v1alpha1.StorageCluster) bool {
	if cluster.Spec.Ceph == nil {
		return false
	}
	for _, host := range cluster.Spec.Ceph.Topology.Nodes {
		if OSDHostUsesAllDevices(cluster, host) {
			return true
		}
	}
	return false
}

func inspectOSDReadiness(cluster v1alpha1.StorageCluster) osdReadinessInspection {
	inspection := osdReadinessInspection{
		managedExact:       true,
		dynamicHosts:       []string{},
		declaredCountKnown: true,
	}
	if cluster.Spec.Ceph == nil {
		return inspection
	}
	dynamicHostSet := map[string]bool{}
	addDynamicHosts := func(hosts []string) {
		for _, host := range hosts {
			if host == "" || dynamicHostSet[host] {
				continue
			}
			dynamicHostSet[host] = true
			inspection.dynamicHosts = append(inspection.dynamicHosts, host)
		}
	}
	consider := func(osd *v1alpha1.StorageCephNodeOSD, hosts []string) {
		if osd == nil {
			return
		}
		if osd.Unmanaged {
			inspection.declaredCountKnown = false
			return
		}
		inspection.managed = true
		if !osdDataSelectionIsStatic(osd.DataDevices) {
			inspection.managedExact = false
			inspection.declaredCountKnown = false
			addDynamicHosts(hosts)
			return
		}
		perDevice := osd.OSDsPerDevice
		if perDevice < 1 {
			perDevice = 1
		}
		inspection.count += len(hosts) * len(dataDeviceStaticPaths(osd)) * perDevice
	}
	for _, host := range cluster.Spec.Ceph.Topology.Nodes {
		if !NodeHasRole(host, v1alpha1.StorageCephRoleOSD) {
			continue
		}
		if len(host.Devices) > 0 {
			inspection.managed = true
			inspection.count += len(host.Devices)
			continue
		}
		consider(host.OSD, []string{host.Name})
	}
	for i := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
		drivegroup := cluster.Spec.Ceph.Topology.OSDDrivegroups[i]
		hosts := ResolvePlacement(cluster, drivegroup.Placement, v1alpha1.StorageCephRoleOSD)
		consider(&cluster.Spec.Ceph.Topology.OSDDrivegroups[i].OSD, hosts)
	}
	return inspection
}

func osdDataSelectionIsStatic(selection *v1alpha1.StorageCephDeviceSelection) bool {
	if selection == nil {
		return false
	}
	if selection.All || selection.Model != "" || selection.Vendor != "" || selection.Rotational != nil ||
		selection.Size != "" || selection.Limit != 0 {
		return false
	}
	return len(selection.Paths) > 0 || len(selection.PathSpecs) > 0
}
