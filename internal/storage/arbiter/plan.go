package arbiter

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephstate"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

type Plan struct {
	Cluster        string   `json:"cluster"`
	FailureDomain  string   `json:"failureDomain"`
	DesiredNode    string   `json:"desiredNode"`
	DesiredMon     string   `json:"desiredMon"`
	DesiredSite    string   `json:"desiredSite"`
	DesiredMachine string   `json:"desiredMachine"`
	TiebreakerMon  string   `json:"tiebreakerMon,omitempty"`
	LiveMon        string   `json:"liveMon,omitempty"`
	LiveNode       string   `json:"liveNode,omitempty"`
	LiveMachine    string   `json:"liveMachine,omitempty"`
	LiveSite       string   `json:"liveSite,omitempty"`
	MonHostsDuring []string `json:"monHostsDuring"`
	MonHostsAfter  []string `json:"monHostsAfter"`
	Settled        bool     `json:"settled"`
	Residue        []string `json:"residue,omitempty"`
	SameSiteMons   []string `json:"sameSiteMons,omitempty"`
	Degraded       string   `json:"degraded,omitempty"`
}

func (p Plan) SameSite() bool { return len(p.SameSiteMons) > 0 }

func (p Plan) TiebreakerSwitched() bool { return p.TiebreakerMon == p.DesiredMon }

func DesiredArbiter(cluster v1alpha1.StorageCluster) (v1alpha1.StorageCephNode, error) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.StorageCephNode{}, fmt.Errorf("StorageCluster/%s declares no spec.ceph, so it has no arbiter to replace", cluster.Metadata.Name)
	}
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil {
		return v1alpha1.StorageCephNode{}, fmt.Errorf("StorageCluster/%s declares no spec.ceph.topology.stretch, so it has no arbiter; a stretch tiebreaker exists only in a stretch topology", cluster.Metadata.Name)
	}
	if strings.TrimSpace(stretch.Tiebreaker.Node) == "" {
		return v1alpha1.StorageCephNode{}, fmt.Errorf("StorageCluster/%s declares spec.ceph.topology.stretch without a tiebreaker, so there is no arbiter to replace; whether a stretch cluster carries an arbiter is fixed when it is bootstrapped, so spec.ceph.topology.stretch.tiebreaker.node has to be declared before the first apply builds the cluster — on a cluster that is already running, no command adds one", cluster.Metadata.Name)
	}
	name := topology.CanonicalHostname(cluster, stretch.Tiebreaker.Node)
	node, ok := topology.HostByName(cluster, name)
	if !ok {
		return v1alpha1.StorageCephNode{}, fmt.Errorf("StorageCluster/%s spec.ceph.topology.stretch.tiebreaker.node %q matches no topology node", cluster.Metadata.Name, stretch.Tiebreaker.Node)
	}
	return node, nil
}

func Compute(cluster v1alpha1.StorageCluster, live cephstate.Discovery) (Plan, error) {
	desired, err := DesiredArbiter(cluster)
	if err != nil {
		return Plan{}, err
	}
	domain := topology.FailureDomain(cluster)
	plan := Plan{
		Cluster:       cluster.Metadata.Name,
		FailureDomain: domain,
		DesiredNode:   desired.Name,
		DesiredMon:    topology.CephDaemonName(desired.Name),
		DesiredSite:   desired.Site,
		MonHostsAfter: topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMON),
	}
	plan.DesiredMachine = desired.MachineRef.Name

	dump, err := live.MonDump()
	if err != nil {
		return Plan{}, fmt.Errorf("read the live monmap of storage cluster %s: %w; `bootwright storage-cluster replace-arbiter` reconciles the live tiebreaker, so it cannot plan without one", cluster.Metadata.Name, err)
	}
	if !dump.StretchMode {
		return Plan{}, fmt.Errorf("storage cluster %s is not in stretch mode on the cluster itself (`ceph mon dump` reports stretch_mode false), so it carries no tiebreaker to replace; if this cluster was authored as a stretch cluster before it was bootstrapped, converge it with `bootwright apply --clusters %s`, but if the stretch topology was added to an already-built cluster, apply refuses it as a shape change Bootwright cannot make in place", cluster.Metadata.Name, cluster.Metadata.Name)
	}
	plan.TiebreakerMon = dump.TiebreakerMon
	if plan.TiebreakerMon == "" {
		return Plan{}, fmt.Errorf("storage cluster %s reports stretch mode but names no tiebreaker_mon in its live monmap, so Bootwright will not guess which mon to retire; repair the monmap with `ceph mon set_new_tiebreaker <mon>` on the cluster, or converge the authored stretch topology with `bootwright apply --clusters %s`", cluster.Metadata.Name, cluster.Metadata.Name)
	}
	strays := undeclaredMons(cluster, dump, plan.DesiredMon)
	switch {
	case !plan.TiebreakerSwitched():
		plan.LiveMon = plan.TiebreakerMon
	case len(strays) == 1:
		plan.LiveMon = strays[0]
	case len(strays) > 1:
		return Plan{}, fmt.Errorf("storage cluster %s already answers with mon.%s as its stretch tiebreaker, but its monmap still carries mon(s) %s that no node of the authored topology declares, so Bootwright cannot tell which one an interrupted replacement left behind; remove the leftover mon(s) with `ceph mon rm <mon>` and `ceph orch host rm <host>` on the cluster, then re-run", cluster.Metadata.Name, plan.DesiredMon, strings.Join(strays, ", "))
	}
	if plan.LiveMon != "" {
		plan.LiveSite = dump.SiteOf(plan.LiveMon, domain)
		if node, ok := topology.HostByName(cluster, plan.LiveMon); ok {
			plan.LiveNode = node.Name
			plan.LiveMachine = node.MachineRef.Name
		}
	}
	plan.LiveNode = retirementHost(cluster, live, plan)
	plan.Residue = retirementResidue(cluster, live, plan)
	plan.Settled = plan.TiebreakerSwitched() && len(plan.Residue) == 0
	plan.MonHostsDuring = unionHosts(plan.MonHostsAfter, plan.LiveNode)
	for _, mon := range dump.MonsAtSite(domain, desired.Site) {
		if mon == plan.DesiredMon || mon == plan.LiveMon {
			continue
		}
		plan.SameSiteMons = append(plan.SameSiteMons, mon)
	}
	plan.Degraded = degradedReason(dump, plan)
	return plan, nil
}

func degradedReason(dump cephstate.MonDump, plan Plan) string {
	var absent []string
	for _, name := range dump.MonNames() {
		if name == plan.LiveMon {
			continue
		}
		if !dump.InQuorum(name) {
			absent = append(absent, name)
		}
	}
	if len(absent) == 0 {
		return ""
	}
	sort.Strings(absent)
	return "mon(s) " + strings.Join(absent, ", ") + " are outside quorum"
}

func undeclaredMons(cluster v1alpha1.StorageCluster, dump cephstate.MonDump, desiredMon string) []string {
	var out []string
	for _, name := range dump.MonNames() {
		if name == desiredMon {
			continue
		}
		if _, ok := topology.HostByName(cluster, name); ok {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func undeclaredMonHosts(cluster v1alpha1.StorageCluster, live cephstate.Discovery) []string {
	daemons, err := live.Daemons()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, daemon := range daemons {
		if daemon.DaemonType != "mon" || daemon.Hostname == "" || seen[daemon.Hostname] {
			continue
		}
		if _, ok := topology.HostByName(cluster, daemon.Hostname); ok {
			continue
		}
		seen[daemon.Hostname] = true
		out = append(out, daemon.Hostname)
	}
	sort.Strings(out)
	return out
}

func retirementHost(cluster v1alpha1.StorageCluster, live cephstate.Discovery, plan Plan) string {
	if plan.LiveMon != "" {
		if host, ok := registeredHostFor(live, plan.LiveMon); ok {
			return host
		}
		if plan.LiveNode != "" {
			return plan.LiveNode
		}
		return plan.LiveMon
	}
	if strays := undeclaredMonHosts(cluster, live); len(strays) == 1 {
		return strays[0]
	}
	return ""
}

func registeredHostFor(live cephstate.Discovery, mon string) (string, bool) {
	hosts, err := live.Hosts()
	if err != nil {
		return "", false
	}
	for _, host := range hosts {
		if host.Hostname == mon || topology.CephDaemonName(host.Hostname) == mon {
			return host.Hostname, true
		}
	}
	return "", false
}

func retirementResidue(cluster v1alpha1.StorageCluster, live cephstate.Discovery, plan Plan) []string {
	var out []string
	if plan.LiveMon != "" {
		out = append(out, "mon."+plan.LiveMon+" is still in the monmap")
	}
	for _, host := range undeclaredMonHosts(cluster, live) {
		if plan.LiveMon != "" && host == plan.LiveNode {
			continue
		}
		out = append(out, "host "+host+" still runs a mon daemon the authored topology declares no node for")
	}
	return out
}

func unionHosts(hosts []string, extra string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(hosts)+1)
	for _, host := range hosts {
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	if extra != "" && !seen[extra] {
		out = append(out, extra)
	}
	sort.Strings(out)
	return out
}

func ValidateCandidate(state v1alpha1.State, cluster v1alpha1.StorageCluster, machineName string) error {
	machine, ok := machineByName(state, machineName)
	if !ok {
		return fmt.Errorf("--new-arbiter-machine %q matches no Machine in this context; the replacement arbiter is authored, not discovered", machineName)
	}
	if !v1alpha1.MachineHasCapability(machine, v1alpha1.MachineCapabilityCephNode) {
		return fmt.Errorf("the arbiter candidate Machine/%s lacks capability %q, so it cannot carry a Ceph mon; add %q (and %q to mark it an arbiter candidate) and run `bootwright context update`", machineName, v1alpha1.MachineCapabilityCephNode, v1alpha1.MachineCapabilityCephNode, v1alpha1.MachineCapabilityCephArbiter)
	}
	if !v1alpha1.MachineHasCapability(machine, v1alpha1.MachineCapabilityCephArbiter) {
		return fmt.Errorf("the arbiter candidate Machine/%s does not declare capability %q; an arbiter replacement only moves the tiebreaker onto a machine authored to hold it, so add that capability and run `bootwright context update`", machineName, v1alpha1.MachineCapabilityCephArbiter)
	}
	if owner, node, ok := boundElsewhere(state, cluster.Metadata.Name, machineName); ok {
		return fmt.Errorf("the arbiter candidate Machine/%s is already node %q of %s; a machine is node-bound by at most one cluster, so it cannot also become the arbiter of %s — pick an unbound arbiter candidate or release that node first", machineName, node, owner, cluster.Metadata.Name)
	}
	if v1alpha1.MachineSite(machine) == "" {
		return fmt.Errorf("the arbiter candidate Machine/%s declares no spec.placement.site; the replacement mon's CRUSH location and the same-site refusal both read the site the machine stands in, so set spec.placement.site and run `bootwright context update`", machineName)
	}
	return nil
}

func machineByName(state v1alpha1.State, name string) (v1alpha1.Machine, bool) {
	for _, machine := range state.Machines {
		if machine.Metadata.Name == name {
			return machine, true
		}
	}
	return v1alpha1.Machine{}, false
}

func boundElsewhere(state v1alpha1.State, clusterName, machineName string) (string, string, bool) {
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == machineName {
				return "ContainerCluster/" + cluster.Metadata.Name, node.Name, true
			}
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name != machineName {
				continue
			}
			if cluster.Metadata.Name == clusterName {
				continue
			}
			return "StorageCluster/" + cluster.Metadata.Name, node.Name, true
		}
	}
	return "", "", false
}

func CandidateMachines(state v1alpha1.State) []string {
	var out []string
	for _, machine := range state.Machines {
		if v1alpha1.MachineHasCapability(machine, v1alpha1.MachineCapabilityCephArbiter) {
			out = append(out, machine.Metadata.Name)
		}
	}
	sort.Strings(out)
	return out
}
