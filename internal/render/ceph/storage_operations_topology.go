package ceph

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// cephTopologyOperations renders the cluster-wide topology-phase operations:
// the public/cluster network settings, the declared ceph config options, and
// the stretch-mode wiring. Every op reconciles in place (`ceph config set` /
// `ceph mon set` are last-write-wins); convergence is additive-only by design.
func cephTopologyOperations(cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	// public_network has no cephadm bootstrap flag (unlike --cluster-network);
	// it is seeded at bootstrap via --config and kept converged here. Both
	// network ops reconcile in place: `ceph config set` is last-write-wins.
	if publics := cluster.Spec.Ceph.Networks.PublicCIDRs; len(publics) > 0 {
		ops = append(ops, operationInPhase("topology", "set-public-network", "ceph", "config", "set", "global", "public_network", strings.Join(publics, ",")))
	}
	if clusters := cluster.Spec.Ceph.Networks.ClusterCIDRs; len(clusters) > 0 {
		ops = append(ops, operationInPhase("topology", "set-cluster-network", "ceph", "config", "set", "global", "cluster_network", strings.Join(clusters, ",")))
	}
	// Declared ceph config options reconcile in place (`ceph config set` is
	// last-write-wins); convergence is additive-only by design.
	for _, section := range sortedKeys(cluster.Spec.Ceph.Config) {
		options := cluster.Spec.Ceph.Config[section]
		for _, key := range sortedKeys(options) {
			ops = append(ops, operationInPhase("topology", "set-config-"+section+"-"+key, "ceph", "config", "set", section, key, options[key]))
		}
	}
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil {
		// Stretch mode requires the connectivity election strategy before
		// enable_stretch_mode; `ceph mon set` reconciles in place.
		ops = append(ops, operationInPhase("topology", "set-election-strategy", "ceph", "mon", "set", "election_strategy", "connectivity"))
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
				continue
			}
			ops = append(ops, operationInPhase("topology", "set-mon-location-"+node.Hostname, "ceph", "mon", "set_location", node.Hostname, stretch.FailureDomain+"="+node.Site))
		}
		// The stretch rule must place two replicas per data site
		// (choose firstn 0 type <failureDomain> + chooseleaf firstn 2 type
		// host); `crush rule create-replicated` cannot express that two-step
		// rule, so the role compiles it into the CRUSH map itself, keyed on
		// this structured operation (no argv).
		stretchRule := operationWithIdempotency("topology", "create-crush-rule-"+stretch.RuleName, "stretch-crush-rule", stretch.RuleName)
		stretchRule["structural"] = map[string]any{
			"failureDomain":            stretch.FailureDomain,
			"replicasPerFailureDomain": 2,
		}
		ops = append(ops, stretchRule)
		// The tiebreaker is authored as a machine name; enable_stretch_mode wants
		// the mon name, which is the registered (fully-qualified) hostname.
		ops = append(ops, operationWithIdempotency("topology", "enable-stretch-mode", "stretch-mode", "enabled", "ceph", "mon", "enable_stretch_mode", topology.CanonicalHostname(cluster, stretch.Tiebreaker.Host), stretch.RuleName, stretch.FailureDomain))
	}
	return ops
}

// cephMgrAndLoggingOperations renders the topology-phase mgr-module enables and
// the dashboard loki wiring. These run after the pool/filesystem storage-phase
// ops in the rendered order, so they are a distinct builder from
// cephTopologyOperations rather than folded into it.
func cephMgrAndLoggingOperations(cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	// mgr modules reconcile additively; the role probes `ceph mgr module ls`
	// and skips already-enabled (or always-on) modules.
	for _, module := range cluster.Spec.Ceph.MgrModules {
		ops = append(ops, operationWithIdempotency("topology", "enable-mgr-module-"+module, "mgr-module", module, "ceph", "mgr", "module", "enable", module))
	}
	// Authoring loki wires the dashboard to it (the easy-to-forget half of
	// centralized logging). promtail ships logs to loki, so there is no
	// set-promtail-api-host — only set-loki-api-host. Last-write-wins.
	if monitoring := cluster.Spec.Ceph.Monitoring; monitoring != nil && monitoring.Loki != nil {
		if hosts := topology.ResolvePlacement(cluster, monitoring.Loki.Placement, ""); len(hosts) > 0 {
			port := monitoring.Loki.Port
			if port == 0 {
				port = 3100
			}
			url := fmt.Sprintf("http://%s:%d", hosts[0], port)
			ops = append(ops, operationInPhase("topology", "set-dashboard-loki-api-host", "ceph", "dashboard", "set-loki-api-host", url))
		}
	}
	return ops
}
