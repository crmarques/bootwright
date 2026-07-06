package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func validateStorageCephStretch(cluster v1alpha1.StorageCluster) []string {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	nodes := cluster.Spec.Ceph.Topology.Hosts
	prefix := fmt.Sprintf("StorageCluster/%s spec.ceph.topology.stretch", cluster.Metadata.Name)
	var errs []string
	if stretch.FailureDomain == "" {
		errs = append(errs, prefix+".failureDomain is required")
	}
	if len(stretch.DataSites) != 2 {
		// dataSites is optional: when omitted, normalize derives it from every
		// non-tiebreaker host site, so an OSD-only extra site lands here and trips
		// this check. Show the current (possibly derived) value and point at the
		// remedy rather than implying the operator authored the wrong count.
		errs = append(errs, fmt.Sprintf("%s.dataSites must name exactly two data sites, got %d %v; if this was derived from spec.ceph.topology.hosts, author spec.ceph.topology.stretch.dataSites explicitly to name the two data sites and exclude OSD-only or tiebreaker sites", prefix, len(stretch.DataSites), stretch.DataSites))
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
	// Resolve the tiebreaker host through the same machine-name-or-hostname
	// resolver every other storage reference uses (storageCephNodeByName).
	// normalize FQDN-qualifies node.Hostname while tiebreaker.host keeps the
	// authored token, so a raw `node.Hostname == tiebreaker.host` compare
	// silently skipped the mon-only/no-OSD/site safety checks for the common
	// machine-name authoring form.
	if stretch.Tiebreaker.Host == "" {
		errs = append(errs, prefix+".tiebreaker.host is required")
	} else if node, ok := storageCephNodeByName(cluster, stretch.Tiebreaker.Host); ok {
		if stretch.Tiebreaker.Site != "" && node.Site != stretch.Tiebreaker.Site {
			errs = append(errs, fmt.Sprintf("%s.tiebreaker.host %q is in site %q, want %q", prefix, stretch.Tiebreaker.Host, node.Site, stretch.Tiebreaker.Site))
		}
		if !storageCephNodeRolesOnly(node, v1alpha1.StorageCephRoleMON) {
			errs = append(errs, fmt.Sprintf("%s.tiebreaker.host %q must be mon-only", prefix, stretch.Tiebreaker.Host))
		}
		if len(node.Devices) > 0 {
			errs = append(errs, fmt.Sprintf("%s.tiebreaker.host %q must not declare OSD devices", prefix, stretch.Tiebreaker.Host))
		}
	} else {
		errs = append(errs, fmt.Sprintf("%s.tiebreaker.host %q must name a spec.ceph.topology.hosts[] entry", prefix, stretch.Tiebreaker.Host))
	}
	if stretch.RuleName == "" {
		errs = append(errs, prefix+".ruleName is required")
	}
	monBySite := map[string]int{}
	for _, node := range nodes {
		if topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			monBySite[node.Site]++
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
