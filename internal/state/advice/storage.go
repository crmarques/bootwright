package advice

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

type AdvisorySeverity string

const (
	SeverityWarn AdvisorySeverity = "WARN"
	SeverityInfo AdvisorySeverity = "INFO"
)

const (
	cephBestPracticeGroup = "Ceph best practice"
	stretchPoolGroup      = "Stretch pools"
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
	var out []StorageAdvisory
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
	}
	return out
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
	for key := range ceph.Config["mgr"] {
		if strings.HasPrefix(key, "mgr/cephadm/container_image_") && key != "mgr/cephadm/container_image_base" {
			return nil
		}
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
	if cluster.Spec.Ceph.Image != "" {
		return nil
	}
	example := "registry.redhat.io/rhceph/rhceph-9-rhel9@sha256:..."
	if distribution == v1alpha1.StorageCephDistributionIBM {
		example = "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9@sha256:..."
	}
	return []StorageAdvisory{{
		Severity:    SeverityWarn,
		Group:       cephBestPracticeGroup,
		Object:      object,
		Finding:     fmt.Sprintf("distribution %q pins no spec.ceph.image", distribution),
		Impact:      "the install uses the distribution-packaged cephadm's default image tag, which floats; the running Ceph version is not reproducible across re-installs",
		Remediation: "set spec.ceph.image to a digest-pinned reference (for example " + example + ")",
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
