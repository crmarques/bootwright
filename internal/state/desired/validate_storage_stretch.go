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
	tiebreakerDeferred := stretch.Tiebreaker.Node == "" && stretch.Tiebreaker.Site == ""
	var errs []string
	if stretch.FailureDomain == "" {
		errs = append(errs, prefix+".failureDomain is required")
	}
	if len(stretch.DataSites) != 2 {
		errs = append(errs, fmt.Sprintf("%s.dataSites must name exactly two data sites, got %d %v; if this was derived from spec.ceph.topology.nodes, author spec.ceph.topology.stretch.dataSites explicitly to name the two data sites and exclude OSD-only or tiebreaker sites", prefix, len(stretch.DataSites), stretch.DataSites))
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
	if !tiebreakerDeferred {
		if stretch.Tiebreaker.Site == "" {
			errs = append(errs, prefix+".tiebreaker.site is required")
		}
		if stretch.Tiebreaker.Node == "" {
			errs = append(errs, prefix+".tiebreaker.node is required")
		} else if node, ok := storageCephNodeByName(cluster, stretch.Tiebreaker.Node); ok {
			if stretch.Tiebreaker.Site != "" && node.Site != stretch.Tiebreaker.Site {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q is in site %q, want %q", prefix, stretch.Tiebreaker.Node, node.Site, stretch.Tiebreaker.Site))
			}
			if !storageCephNodeRolesOnly(node, v1alpha1.StorageCephRoleMON) {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q must be mon-only", prefix, stretch.Tiebreaker.Node))
			}
			if len(node.Devices) > 0 {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q must not declare OSD devices", prefix, stretch.Tiebreaker.Node))
			}
		} else {
			msg := fmt.Sprintf("%s.tiebreaker.node %q must name a spec.ceph.topology.nodes[] entry by node name", prefix, stretch.Tiebreaker.Node)
			if storageCephMachineRefExists(cluster, stretch.Tiebreaker.Node) {
				msg += "; it names the bound Machine, but clusters reference nodes — use the node's name"
			}
			errs = append(errs, msg)
		}
	}
	if stretch.RuleName == "" {
		errs = append(errs, prefix+".ruleName is required")
	}
	monBySite := map[string]int{}
	tiebreakerMons := 0
	for _, node := range nodes {
		if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			continue
		}
		if storageNodeIsTiebreaker(node, stretch.Tiebreaker.Node) {
			tiebreakerMons++
			continue
		}
		monBySite[node.Site]++
	}
	for _, site := range stretch.DataSites {
		if monBySite[site] != 2 {
			errs = append(errs, fmt.Sprintf("%s requires exactly two non-tiebreaker mon nodes in data site %q (got %d)", prefix, site, monBySite[site]))
		}
	}
	if !tiebreakerDeferred && stretch.Tiebreaker.Node != "" && tiebreakerMons != 1 {
		errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q must carry the %q role; stretch mode places the tiebreaker mon by name, not by site", prefix, stretch.Tiebreaker.Node, v1alpha1.StorageCephRoleMON))
	}
	return errs
}
