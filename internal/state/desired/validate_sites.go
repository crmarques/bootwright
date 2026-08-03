package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type siteReference struct {
	owner string
	site  string
}

func validateSites(state v1alpha1.State) []string {
	env := primaryEnvironment(&state)
	if env == nil {
		return nil
	}
	errs := validateEnvironmentSites(*env)
	declared := map[string]bool{}
	for _, name := range v1alpha1.EnvironmentSiteNames(*env) {
		declared[name] = true
	}
	refs := collectSiteReferences(state)
	if len(declared) == 0 {
		if len(refs) > 0 {
			errs = append(errs, fmt.Sprintf("%s names site %q but Environment/%s declares no spec.sites; declare every site the estate spans so a mistyped site fails here instead of silently becoming an extra CRUSH bucket", refs[0].owner, refs[0].site, env.Metadata.Name))
		}
		return errs
	}
	known := strings.Join(v1alpha1.EnvironmentSiteNames(*env), ", ")
	for _, ref := range refs {
		if !declared[ref.site] {
			errs = append(errs, fmt.Sprintf("%s %q is not declared in Environment/%s spec.sites (declared: %s)", ref.owner, ref.site, env.Metadata.Name, known))
		}
	}
	errs = append(errs, validateMachineSitesAreDeclared(state)...)
	errs = append(errs, validateStorageNodeSiteAgreement(state)...)
	return errs
}

func validateEnvironmentSites(env v1alpha1.Environment) []string {
	var errs []string
	seen := map[string]bool{}
	for i, site := range env.Spec.Sites {
		owner := fmt.Sprintf("Environment/%s spec.sites[%d]", env.Metadata.Name, i)
		if site.Name == "" {
			errs = append(errs, owner+".name is required")
			continue
		}
		if len(site.Name) > 63 || !IsDNSLabel(site.Name) {
			errs = append(errs, fmt.Sprintf("%s.name %q is not a DNS label; a site name is rendered as a CRUSH bucket name", owner, site.Name))
		}
		if seen[site.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, site.Name))
		}
		seen[site.Name] = true
	}
	return errs
}

func collectSiteReferences(state v1alpha1.State) []siteReference {
	var refs []siteReference
	add := func(owner, site string) {
		if site != "" {
			refs = append(refs, siteReference{owner: owner, site: site})
		}
	}
	addPlacement := func(owner string, placement v1alpha1.StoragePlacement) {
		for i, site := range placement.Sites {
			add(fmt.Sprintf("%s.sites[%d]", owner, i), site)
		}
	}
	for _, machine := range state.Machines {
		add(fmt.Sprintf("Machine/%s spec.placement.site", machine.Metadata.Name), v1alpha1.MachineSite(machine))
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		prefix := fmt.Sprintf("StorageCluster/%s spec.ceph", cluster.Metadata.Name)
		for i, node := range cluster.Spec.Ceph.Topology.Nodes {
			add(fmt.Sprintf("%s.topology.nodes[%d].site", prefix, i), node.Site)
		}
		if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil {
			for i, site := range stretch.DataSites {
				add(fmt.Sprintf("%s.topology.stretch.dataSites[%d]", prefix, i), site)
			}
			add(prefix+".topology.stretch.tiebreaker.site", stretch.Tiebreaker.Site)
		}
		for i, drivegroup := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
			addPlacement(fmt.Sprintf("%s.topology.osdDrivegroups[%d].placement", prefix, i), drivegroup.Placement)
		}
		if monitoring := cluster.Spec.Ceph.Monitoring; monitoring != nil {
			for _, item := range []struct {
				field   string
				service *v1alpha1.StorageCephMonitoringService
			}{
				{"prometheus", monitoring.Prometheus},
				{"grafana", monitoring.Grafana},
				{"alertmanager", monitoring.Alertmanager},
				{"nodeExporter", monitoring.NodeExporter},
				{"loki", monitoring.Loki},
				{"promtail", monitoring.Promtail},
			} {
				if item.service != nil {
					addPlacement(fmt.Sprintf("%s.monitoring.%s.placement", prefix, item.field), item.service.Placement)
				}
			}
		}
		for i, service := range cluster.Spec.Ceph.Services {
			addPlacement(fmt.Sprintf("%s.services[%d].placement", prefix, i), service.Placement)
		}
		if mgmt := cluster.Spec.Ceph.MgmtGateway; mgmt != nil {
			addPlacement(prefix+".mgmtGateway.ingress.placement", mgmt.Ingress.Placement)
		}
	}
	for _, fs := range state.StorageFilesystems {
		addPlacement(fmt.Sprintf("StorageFilesystem/%s spec.cephfs.mds.placement", fs.Metadata.Name), fs.Spec.CephFS.MDS.Placement)
	}
	for _, gateway := range state.StorageObjectGateways {
		prefix := fmt.Sprintf("StorageObjectGateway/%s spec.ceph", gateway.Metadata.Name)
		addPlacement(prefix+".placement", gateway.Spec.Ceph.Placement)
		for i, ingress := range gateway.Spec.Ceph.Ingresses {
			addPlacement(fmt.Sprintf("%s.ingresses[%d].placement", prefix, i), ingress.Placement)
		}
	}
	for _, nfs := range state.StorageNFSExports {
		addPlacement(fmt.Sprintf("StorageNFSExport/%s spec.ceph.placement", nfs.Metadata.Name), nfs.Spec.Ceph.Placement)
	}
	return refs
}

func validateMachineSitesAreDeclared(state v1alpha1.State) []string {
	machines := indexMachines(state.Machines)
	var errs []string
	stretched := storageStretchClusterNames(state)
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		because := storageSiteRequirement(state, cluster)
		if because == "" {
			continue
		}
		for i, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.Site != "" || node.MachineRef.Name == "" {
				continue
			}
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				continue
			}
			errs = append(errs, fmt.Sprintf("Machine/%s spec.placement.site is required: StorageCluster/%s spec.ceph.topology.nodes[%d] binds it and %s, so the site the machine stands in is the site the cluster places it in", machine.Metadata.Name, cluster.Metadata.Name, i, because))
		}
	}
	if len(stretched) == 0 {
		return errs
	}
	for _, machine := range state.Machines {
		if !machineHasCapability(machine, v1alpha1.MachineCapabilityCephArbiter) || v1alpha1.MachineSite(machine) != "" {
			continue
		}
		errs = append(errs, fmt.Sprintf("Machine/%s spec.placement.site is required: it carries capability %q and StorageCluster/%s declares stretch mode, so replace-arbiter reads this machine's site to place the tiebreaker mon truthfully", machine.Metadata.Name, v1alpha1.MachineCapabilityCephArbiter, strings.Join(stretched, ", ")))
	}
	return errs
}

func storageStretchClusterNames(state v1alpha1.State) []string {
	var out []string
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Topology.Stretch != nil {
			out = append(out, cluster.Metadata.Name)
		}
	}
	return out
}

func validateStorageNodeSiteAgreement(state v1alpha1.State) []string {
	machines := indexMachines(state.Machines)
	var errs []string
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for i, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.Site == "" || node.MachineRef.Name == "" {
				continue
			}
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				continue
			}
			site := v1alpha1.MachineSite(machine)
			if site == "" || site == node.Site {
				continue
			}
			errs = append(errs, fmt.Sprintf("StorageCluster/%s spec.ceph.topology.nodes[%d].site %q does not match Machine/%s spec.placement.site %q; a machine stands in one site, so the cluster cannot place it in another — drop the node site to take the machine's, or correct whichever is wrong", cluster.Metadata.Name, i, node.Site, machine.Metadata.Name, site))
		}
	}
	return errs
}
