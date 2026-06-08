package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func validateStorageCephStretch(cluster v1alpha1.StorageCluster) []string {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	nodes := cluster.Spec.Ceph.Topology.Nodes
	prefix := fmt.Sprintf("StorageCluster/%s spec.ceph.topology.stretch", cluster.Metadata.Name)
	var errs []string
	if stretch.FailureDomain == "" {
		errs = append(errs, prefix+".failureDomain is required")
	}
	if len(stretch.DataSites) != 2 {
		errs = append(errs, fmt.Sprintf("%s.dataSites must contain exactly two sites", prefix))
	}
	dataSites := map[string]bool{}
	for i, site := range stretch.DataSites {
		owner := fmt.Sprintf("%s.dataSites[%d]", prefix, i)
		if site == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if dataSites[site] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, site))
		}
		dataSites[site] = true
	}
	if stretch.Tiebreaker.Site == "" {
		errs = append(errs, prefix+".tiebreaker.site is required")
	} else if dataSites[stretch.Tiebreaker.Site] {
		errs = append(errs, fmt.Sprintf("%s.tiebreaker.site %q must be distinct from dataSites", prefix, stretch.Tiebreaker.Site))
	}
	if stretch.Tiebreaker.Node == "" {
		errs = append(errs, prefix+".tiebreaker.node is required")
	}
	if stretch.RuleName == "" {
		errs = append(errs, prefix+".ruleName is required")
	}
	if stretch.ReplicatedPoolDefaults.Size != 4 || stretch.ReplicatedPoolDefaults.MinSize != 2 {
		errs = append(errs, fmt.Sprintf("%s.replicatedPoolDefaults must set size: 4 and minSize: 2 for stretch mode", prefix))
	}
	monBySite := map[string]int{}
	for _, node := range nodes {
		if topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			monBySite[node.Site]++
		}
		if node.Name == stretch.Tiebreaker.Node {
			if node.Site != stretch.Tiebreaker.Site {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q is in site %q, want %q", prefix, node.Name, node.Site, stretch.Tiebreaker.Site))
			}
			if !storageCephNodeRolesOnly(node, v1alpha1.StorageCephRoleMON) {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q must be mon-only", prefix, node.Name))
			}
			if len(node.Devices) > 0 {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q must not declare OSD devices", prefix, node.Name))
			}
		}
	}
	for _, site := range stretch.DataSites {
		if monBySite[site] != 2 {
			errs = append(errs, fmt.Sprintf("%s requires exactly two mon nodes in data site %q (got %d)", prefix, site, monBySite[site]))
		}
	}
	if monBySite[stretch.Tiebreaker.Site] != 1 {
		errs = append(errs, fmt.Sprintf("%s requires exactly one tiebreaker mon in site %q (got %d)", prefix, stretch.Tiebreaker.Site, monBySite[stretch.Tiebreaker.Site]))
	}
	return errs
}
