package advice

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/cephprovider"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

type AdvisorySeverity string

const (
	SeverityWarn AdvisorySeverity = "WARN"
	SeverityInfo AdvisorySeverity = "INFO"
)

const (
	cephBestPracticeGroup  = "Ceph best practice"
	stretchPoolGroup       = "Stretch pools"
	stretchTiebreakerGroup = "Stretch tiebreaker"
	estateSiteGroup        = "Estate sites"
)

type StorageAdvisory struct {
	Severity    AdvisorySeverity `json:"severity"`
	Group       string           `json:"group"`
	Object      string           `json:"object"`
	Finding     string           `json:"finding"`
	Impact      string           `json:"impact,omitempty"`
	Remediation string           `json:"remediation,omitempty"`
}

func StorageAdvisories(state v1alpha1.State) []StorageAdvisory {
	disconnected := environmentIsDisconnected(state)
	out := unreferencedSiteAdvisories(state)
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil || v1alpha1.StorageClusterExternal(cluster) {
			continue
		}
		object := fmt.Sprintf("%s/%s", v1alpha1.KindStorageCluster, cluster.Metadata.Name)
		out = append(out, storageMonitorAdvisories(object, cluster)...)
		out = append(out, storageManagerAdvisories(object, cluster)...)
		out = append(out, storageImageAdvisories(object, cluster)...)
		out = append(out, storageSidecarImageAdvisories(object, cluster, disconnected)...)
		out = append(out, storageStretchPoolAdvisories(object, state, cluster)...)
		out = append(out, storageStretchTiebreakerAdvisories(object, cluster)...)
		out = append(out, storageStretchDataSiteTiebreakerAdvisories(object, cluster)...)
	}
	return out
}

func unreferencedSiteAdvisories(state v1alpha1.State) []StorageAdvisory {
	if len(state.Environments) == 0 {
		return nil
	}
	env := state.Environments[0]
	used := map[string]bool{}
	for _, machine := range state.Machines {
		if site := v1alpha1.MachineSite(machine); site != "" {
			used[site] = true
		}
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.Site != "" {
				used[node.Site] = true
			}
		}
	}
	var out []StorageAdvisory
	for _, site := range v1alpha1.EnvironmentSiteNames(env) {
		if used[site] {
			continue
		}
		out = append(out, StorageAdvisory{
			Severity:    SeverityInfo,
			Group:       estateSiteGroup,
			Object:      fmt.Sprintf("%s/%s", v1alpha1.KindEnvironment, env.Metadata.Name),
			Finding:     fmt.Sprintf("site %q is declared but no Machine stands in it", site),
			Impact:      "a site no machine references places nothing; if it was meant to hold a standby arbiter, the candidate machine is missing, and replace-arbiter will have nowhere to move the tiebreaker",
			Remediation: fmt.Sprintf("declare a Machine with spec.placement.site %q, or drop the site from spec.sites", site),
		})
	}
	return out
}

func storageStretchDataSiteTiebreakerAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil || stretch.Tiebreaker.Node == "" || stretch.Tiebreaker.Site == "" {
		return nil
	}
	for _, site := range stretch.DataSites {
		if site != stretch.Tiebreaker.Site {
			continue
		}
		return []StorageAdvisory{{
			Severity:    SeverityWarn,
			Group:       stretchTiebreakerGroup,
			Object:      object,
			Finding:     fmt.Sprintf("the tiebreaker mon stands in data site %q rather than a third site", site),
			Impact:      "losing that data site takes its two replicas and the tiebreaker vote at once, so the surviving site cannot win the monitor election and the cluster stops serving; a cluster not already in stretch mode also cannot enter it in this shape, because Ceph refuses enable_stretch_mode when the tiebreaker shares a data site",
			Remediation: fmt.Sprintf("this is the acknowledged emergency shape after `--authorize same-site-arbiter`; move the tiebreaker back with `bootwright storage-cluster replace-arbiter %s --new-arbiter-machine <machine outside %s>` once a third site is available", strings.TrimPrefix(object, v1alpha1.KindStorageCluster+"/"), strings.Join(stretch.DataSites, " and ")),
		}}
	}
	return nil
}

func storageStretchTiebreakerAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil || stretch.Tiebreaker.Node != "" {
		return nil
	}
	return []StorageAdvisory{{
		Severity:    SeverityWarn,
		Group:       stretchTiebreakerGroup,
		Object:      object,
		Finding:     "stretch mode is declared with no tiebreaker/arbiter mon",
		Impact:      "a two-site stretch cluster without a tiebreaker mon in a third site loses monitor quorum if either data site fails, and apply cannot enable stretch mode, so every pool places two replicas per site (apply reconciles the Ceph-internal pools onto the same rule in place of the stretch-mode re-homing) but without automatic degraded-mode failover",
		Remediation: "add a mon-only arbiter node in a third site and set spec.ceph.topology.stretch.tiebreaker.node before relying on this cluster",
	}}
}

func environmentIsDisconnected(state v1alpha1.State) bool {
	if len(state.Environments) == 0 {
		return false
	}
	registries := state.Environments[0].Spec.Registries
	if registries == nil {
		return false
	}
	return registries.Mirror != nil || len(registries.ImageDigestSources) > 0
}

func storageSidecarImageAdvisories(object string, cluster v1alpha1.StorageCluster, disconnected bool) []StorageAdvisory {
	ceph := cluster.Spec.Ceph
	monitoring := ceph.Monitoring
	monitoringEnabled := monitoring == nil || monitoring.Enabled == nil || *monitoring.Enabled
	if !monitoringEnabled {
		return nil
	}
	vendorMismatch := ceph.Distribution == v1alpha1.StorageCephDistributionIBM
	if !disconnected && !vendorMismatch {
		return nil
	}
	if len(v1alpha1.StorageCephSidecarImagePins(ceph)) > 0 {
		return nil
	}
	return []StorageAdvisory{{
		Severity:    SeverityWarn,
		Group:       cephBestPracticeGroup,
		Object:      object,
		Finding:     "monitoring/ingress sidecar images are not pinned to the entitled registry or mirror",
		Impact:      "cephadm pulls prometheus/grafana/alertmanager/node-exporter/haproxy/keepalived from upstream defaults this cluster cannot reach; the monitoring stack and every HA VIP fail to deploy",
		Remediation: "pin mgr/cephadm/container_image_{prometheus,grafana,alertmanager,node_exporter,haproxy,keepalived} under spec.ceph.config[mgr] to the mirror/entitled registry references",
	}}
}

func storageMonitorAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	if cluster.Spec.Ceph.Topology.Stretch != nil {
		return nil
	}
	mons := len(topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMON))
	switch {
	case mons < 3:
		return []StorageAdvisory{{
			Severity:    SeverityWarn,
			Group:       cephBestPracticeGroup,
			Object:      object,
			Finding:     fmt.Sprintf("%d host(s) carry the mon role", mons),
			Impact:      "Ceph recommends at least 3 monitors for quorum; fewer cannot survive a single monitor failure without losing the cluster",
			Remediation: "give the mon role to at least 3 hosts (an odd count); single-monitor clusters are for lab or single-node use only",
		}}
	case mons%2 == 0:
		return []StorageAdvisory{{
			Severity:    SeverityWarn,
			Group:       cephBestPracticeGroup,
			Object:      object,
			Finding:     fmt.Sprintf("%d hosts carry the mon role (an even count)", mons),
			Impact:      "an even monitor count tolerates no more failures than the next-lower odd count, so the extra monitor adds cost without availability",
			Remediation: "use an odd number of mon hosts (3, 5, 7)",
		}}
	}
	return nil
}

func storageManagerAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	mgrs := len(topology.CephHostsWithRole(cluster, v1alpha1.StorageCephRoleMGR))
	if mgrs < 2 {
		return []StorageAdvisory{{
			Severity:    SeverityWarn,
			Group:       cephBestPracticeGroup,
			Object:      object,
			Finding:     fmt.Sprintf("%d host(s) carry the mgr role", mgrs),
			Impact:      "Ceph recommends at least 2 managers; a single manager is a single point of failure for orchestration, the dashboard, and metrics",
			Remediation: "give the mgr role to at least 2 hosts for an active/standby pair",
		}}
	}
	return nil
}

func storageImageAdvisories(object string, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	distribution := cluster.Spec.Ceph.Distribution
	if distribution != v1alpha1.StorageCephDistributionIBM && distribution != v1alpha1.StorageCephDistributionRedHat {
		return nil
	}
	if v1alpha1.StorageCephImagePinned(cluster.Spec.Ceph) {
		return nil
	}
	example, ok := cephprovider.DerivedImageRepository(distribution, cluster.Spec.Ceph.Release, "")
	if !ok {
		return nil
	}
	return []StorageAdvisory{{
		Severity:    SeverityWarn,
		Group:       cephBestPracticeGroup,
		Object:      object,
		Finding:     fmt.Sprintf("distribution %q pins no spec.ceph.image.version", distribution),
		Impact:      "the install uses the distribution-packaged cephadm's default image tag, which floats; the running Ceph version is not reproducible across re-installs",
		Remediation: "set spec.ceph.image.version to the vendor build you run, as a tag or a sha256: digest; spec.ceph.image.base defaults to " + example + " and is only authored for a mirror",
	}}
}

func storageStretchPoolAdvisories(object string, state v1alpha1.State, cluster v1alpha1.StorageCluster) []StorageAdvisory {
	if cluster.Spec.Ceph.Topology.Stretch == nil {
		return nil
	}
	var pools []string
	for _, pool := range state.StoragePools {
		if pool.Spec.StorageClusterRef.Name == cluster.Metadata.Name && pool.Spec.PlacementPolicyRef.Name == "" {
			pools = append(pools, pool.Metadata.Name)
		}
	}
	if len(pools) == 0 {
		return nil
	}
	return []StorageAdvisory{{
		Severity: SeverityInfo,
		Group:    stretchPoolGroup,
		Object:   object,
		Finding: fmt.Sprintf(
			"policy-less pools inherit the stretch rule and size %d/minSize %d: %s",
			topology.StretchReplicatedPoolSize, topology.StretchReplicatedPoolMinSize, strings.Join(pools, ", "),
		),
	}}
}
