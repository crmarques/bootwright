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
	LiveMon        string   `json:"liveMon,omitempty"`
	LiveNode       string   `json:"liveNode,omitempty"`
	LiveMachine    string   `json:"liveMachine,omitempty"`
	LiveSite       string   `json:"liveSite,omitempty"`
	MonHostsDuring []string `json:"monHostsDuring"`
	MonHostsAfter  []string `json:"monHostsAfter"`
	Settled        bool     `json:"settled"`
	SameSiteMons   []string `json:"sameSiteMons,omitempty"`
	Degraded       string   `json:"degraded,omitempty"`
}

func (p Plan) SameSite() bool { return len(p.SameSiteMons) > 0 }

type Refusal struct {
	Reason string
	Token  string
}

func (r Refusal) Error() string { return r.Reason }

func DesiredArbiter(cluster v1alpha1.StorageCluster) (v1alpha1.StorageCephNode, error) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.StorageCephNode{}, fmt.Errorf("StorageCluster/%s declares no spec.ceph, so it has no arbiter to replace", cluster.Metadata.Name)
	}
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil {
		return v1alpha1.StorageCephNode{}, fmt.Errorf("StorageCluster/%s declares no spec.ceph.topology.stretch, so it has no arbiter; a stretch tiebreaker exists only in a stretch topology", cluster.Metadata.Name)
	}
	if strings.TrimSpace(stretch.Tiebreaker.Node) == "" {
		return v1alpha1.StorageCephNode{}, fmt.Errorf("StorageCluster/%s declares spec.ceph.topology.stretch without a tiebreaker, so there is no arbiter to replace; author spec.ceph.topology.stretch.tiebreaker.node and run `bootwright apply` to add one", cluster.Metadata.Name)
	}
	name := topology.CanonicalHostname(cluster, stretch.Tiebreaker.Node)
	node, ok := topology.HostByName(cluster, name)
	if !ok {
		return v1alpha1.StorageCephNode{}, fmt.Errorf("StorageCluster/%s spec.ceph.topology.stretch.tiebreaker.node %q matches no topology node", cluster.Metadata.Name, stretch.Tiebreaker.Node)
	}
	return node, nil
}

func Compute(state v1alpha1.State, cluster v1alpha1.StorageCluster, live cephstate.Discovery) (Plan, error) {
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
		return Plan{}, fmt.Errorf("storage cluster %s is not in stretch mode on the cluster itself (`ceph mon dump` reports stretch_mode false), so it carries no tiebreaker to replace; run `bootwright apply --clusters %s` to converge the authored stretch topology first", cluster.Metadata.Name, cluster.Metadata.Name)
	}
	plan.LiveMon = dump.TiebreakerMon
	if plan.LiveMon != "" {
		plan.LiveSite = dump.SiteOf(plan.LiveMon, domain)
		if node, ok := topology.HostByName(cluster, plan.LiveMon); ok {
			plan.LiveNode = node.Name
			plan.LiveMachine = node.MachineRef.Name
		} else {
			plan.LiveNode = plan.LiveMon
		}
	}
	plan.Settled = plan.LiveMon != "" && plan.LiveMon == plan.DesiredMon
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
